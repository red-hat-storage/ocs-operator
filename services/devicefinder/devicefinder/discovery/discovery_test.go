//nolint:lll,dupl
package discovery

import (
	"encoding/json"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder/diskutils"
	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder/types"
)

var _ = Describe("Device Discovery", func() {
	var (
		deviceList7Disk              diskutils.BlockDeviceList
		deviceList0Disk              diskutils.BlockDeviceList
		deviceList2Disk2MultiPath    diskutils.BlockDeviceList
		deviceList0Disk0MultiPath    diskutils.BlockDeviceList
		deviceListSanDisk            diskutils.BlockDeviceList
		deviceList0Disk0MultiPath0DM diskutils.BlockDeviceList
	)
	Context("When scanning for disks", func() {

		BeforeEach(func() {
			By("before tests")
			LsblkOut2Disk2MultiPath, err := os.ReadFile(
				"../../test/data/mpath-4-available-disk.json",
			)
			Expect(err).To(Not(HaveOccurred()))

			LsblkOut0Disk0MultiPath, err := os.ReadFile(
				"../../test/data/mpath-0-available-disk.json",
			)
			Expect(err).To(Not(HaveOccurred()))

			LsblkOut7Disk, err := os.ReadFile("../../test/data/7-available-disk.json")
			Expect(err).To(Not(HaveOccurred()))

			LsblkOut0Disk, err := os.ReadFile("../../test/data/0-available-disk.json")
			Expect(err).To(Not(HaveOccurred()))

			LsblkOutSanDisk, err := os.ReadFile("../../test/data/e1-fast.json")
			Expect(err).To(Not(HaveOccurred()))

			err = json.Unmarshal(LsblkOut0Disk, &deviceList0Disk)
			Expect(err).To(Not(HaveOccurred()))

			err = json.Unmarshal(LsblkOut7Disk, &deviceList7Disk)
			Expect(err).To(Not(HaveOccurred()))

			err = json.Unmarshal(LsblkOut2Disk2MultiPath, &deviceList2Disk2MultiPath)
			Expect(err).To(Not(HaveOccurred()))

			err = json.Unmarshal(LsblkOut0Disk0MultiPath, &deviceList0Disk0MultiPath)
			Expect(err).To(Not(HaveOccurred()))

			err = json.Unmarshal(LsblkOutSanDisk, &deviceListSanDisk)
			Expect(err).To(Not(HaveOccurred()))
		})

		AfterEach(func() {
			// // TODO(user): Cleanup logic after each test
		})

		It(
			"should have the correct number of discovered disks with multipath (input data 1)",
			func() {
				discoveredDisks := getDiscoverdDevices(deviceList2Disk2MultiPath.BlockDevices)
				Expect(discoveredDisks).To(HaveLen(4))
			},
		)
		It("should have the correct disks with multipath (input data 1)", func() {
			discoveredDisks := getDiscoverdDevices(deviceList2Disk2MultiPath.BlockDevices)

			Expect(discoveredDisks).To(ContainElement(
				types.DiscoveredDevice{
					DeviceID: "/dev/disk/by-id/dm-name-mpathb",
					Path:     "/dev/dm-1",
					Model:    "iscsi_disk1",
					Type:     "disk",
					Vendor:   "LIO-ORG ",
					Size:     75161927680,
					WWN:      "0x6001405c595842b2d484d0bb11e42179",
				},
			))
			Expect(discoveredDisks).To(ContainElement(
				types.DiscoveredDevice{
					DeviceID: "/dev/disk/by-id/dm-name-mpatha",
					Path:     "/dev/dm-0",
					Model:    "iscsi_disk2",
					Type:     "disk",
					Vendor:   "LIO-ORG ",
					Size:     85899345920,
					WWN:      "0x60014056ade16393c8f412da451430e4",
				},
			))
			Expect(discoveredDisks).To(ContainElement(
				types.DiscoveredDevice{
					DeviceID: "/dev/disk/by-id/scsi-35000c50015ff75aa",
					Path:     "/dev/sdb",
					Model:    "QEMU HARDDISK",
					Type:     "disk",
					Vendor:   "QEMU    ",
					Size:     10737418240,
					WWN:      "0x5000c50015ff75aa",
				},
			))
			Expect(discoveredDisks).To(ContainElement(
				types.DiscoveredDevice{
					DeviceID: "/dev/disk/by-id/scsi-35000c50015ea75bb",
					Path:     "/dev/sde",
					Model:    "QEMU HARDDISK",
					Type:     "disk",
					Vendor:   "QEMU    ",
					Size:     53687091200,
					WWN:      "0x5000c50015ea75bb",
				},
			))

		})

		It(
			"should have the correct number of discovered disks with multipath (input data 2)",
			func() {
				discoveredDisks := getDiscoverdDevices(deviceList0Disk0MultiPath.BlockDevices)
				Expect(discoveredDisks).To(BeEmpty())
			},
		)

		It(
			"should have the correct number of discovered disks without multipath (input data 3)",
			func() {
				discoveredDisks := getDiscoverdDevices(deviceList7Disk.BlockDevices)
				Expect(discoveredDisks).To(HaveLen(2))
			},
		)

		It("should have the correct disks without multipath (input data 3)", func() {
			discoveredDisks := getDiscoverdDevices(deviceList7Disk.BlockDevices)
			Expect(discoveredDisks).To(ContainElement(
				types.DiscoveredDevice{
					DeviceID: "",
					Path:     "/dev/sdk",
					Model:    "LUN C-Mode",
					Type:     "disk",
					Vendor:   "NETAPP  ",
					Size:     4294967296,
					WWN:      "0x600a098038304437415d4b6a5968624d",
				},
			))
			Expect(discoveredDisks).To(ContainElement(
				types.DiscoveredDevice{
					DeviceID: "",
					Path:     "/dev/sdj",
					Model:    "LUN C-Mode",
					Type:     "disk",
					Vendor:   "NETAPP  ",
					Size:     1288490188800,
					WWN:      "0x600a098038304437415d4b6a5968624f",
				},
			))
		})

		It("should have the correct number of discovered disks (input data 4)", func() {
			discoveredDisks := getDiscoverdDevices(deviceList0Disk.BlockDevices)
			Expect(discoveredDisks).To(BeEmpty())
		})

		It("should have the correct number of discovered disks (san disk env)", func() {
			discoveredDisks := getDiscoverdDevices(deviceListSanDisk.BlockDevices)
			Expect(discoveredDisks).To(BeEmpty())
		})

		It("should have the correct number of discovered disks (qe env data)", func() {
			LsblkOut0Disk0MultiPath0DM, err := os.ReadFile("../../test/data/mpath-0-available-disk-0dm-s.json")
			Expect(err).To(Not(HaveOccurred()))

			err = json.Unmarshal(LsblkOut0Disk0MultiPath0DM, &deviceList0Disk0MultiPath0DM)
			Expect(err).To(Not(HaveOccurred()))

			discoveredDisks := getDiscoverdDevices(deviceList0Disk0MultiPath0DM.BlockDevices)
			Expect(discoveredDisks).To(BeEmpty())

		})

	})
})
