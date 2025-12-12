package diskutils

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeCommand struct {
	output []byte
	err    error
}

func (f *fakeCommand) CombinedOutput() ([]byte, error) {
	return f.output, f.err
}

type fakeExecutor struct {
	cmd *fakeCommand
}

func (f *fakeExecutor) Execute(name string, args ...string) Command {
	return f.cmd
}

var _ = Describe("BlockDevice", func() {
	Context("BiosPartition", func() {
		It("returns true if child has 'bios' partlabel", func() {
			device := &BlockDevice{
				Children: []BlockDevice{
					{PartLabel: "BIOS Boot"},
				},
			}
			Expect(device.BiosPartition()).To(BeTrue())
		})

		It("returns true if top-level device has 'boot' partlabel", func() {
			device := &BlockDevice{PartLabel: "Boot Partition"}
			Expect(device.BiosPartition()).To(BeTrue())
		})

		It("returns false if no bios/boot found", func() {
			device := &BlockDevice{PartLabel: "data"}
			Expect(device.BiosPartition()).To(BeFalse())
		})
	})

	Context("GetDevPath", func() {
		It("returns child KName path if fstype is mpath_member", func() {
			device := &BlockDevice{
				FSType: "mpath_member",
				Children: []BlockDevice{
					{KName: "dm-0"},
				},
			}
			path, err := device.GetDevPath()
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dev/dm-0"))
		})

		It("errors out when mpath_member has no children", func() {
			device := &BlockDevice{
				FSType:   "mpath_member",
				Path:     "/dev/sdd",
				Children: []BlockDevice{},
			}
			path, err := device.GetDevPath()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no multipath members found"))
			Expect(path).To(Equal(""))
		})

		It("returns Path otherwise", func() {
			device := &BlockDevice{
				FSType: "ext4",
				Path:   "/dev/sda1",
			}
			path, err := device.GetDevPath()
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dev/sda1"))
		})
	})

	Context("GetPathByID", func() {
		It("returns path by ID if fstype is mpath_member", func() {
			device := &BlockDevice{
				FSType: "mpath_member",
				Children: []BlockDevice{
					{PathByID: "wwn-123"},
				},
			}
			path, err := device.GetPathByID()
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dev/disk/by-id/wwn-123"))
		})

		It("returns own path by ID", func() {
			device := &BlockDevice{
				PathByID: "wwn-456",
			}
			path, err := device.GetPathByID()
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/dev/disk/by-id/wwn-456"))
		})

		It("errors out when mpath_member has no children", func() {
			device := &BlockDevice{
				FSType:   "mpath_member",
				Path:     "/dev/sdd",
				Children: []BlockDevice{},
			}
			_, err := device.GetPathByID()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no multipath members found"))
		})

		It("returns error if no PathByID", func() {
			device := &BlockDevice{}
			_, err := device.GetPathByID()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("disk has no persistent ID"))
		})
	})
})

var _ = Describe("GetBlockDevices", func() {
	It("returns output when command succeeds", func() {
		expectedOutput := []byte(`{"blockdevices":[]}`)
		ExecCommand = &fakeExecutor{
			cmd: &fakeCommand{output: expectedOutput},
		}
		output, err := GetBlockDevices()
		Expect(err).ToNot(HaveOccurred())
		Expect(output).To(Equal(expectedOutput))
	})

	It("returns error when command fails", func() {
		ExecCommand = &fakeExecutor{
			cmd: &fakeCommand{err: errors.New("command failed")},
		}
		output, err := GetBlockDevices()
		Expect(err).To(HaveOccurred())
		Expect(output).To(BeEmpty())
	})
})
