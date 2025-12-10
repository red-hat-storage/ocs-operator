package discovery

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("UdevEvent", func() {
	Context("matchUdevEvent", func() {
		DescribeTable("should match udev events correctly",
			func(text string, matches, exclusion []string, expected bool) {
				actual, err := matchUdevEvent(text, matches, exclusion)
				Expect(err).ToNot(HaveOccurred())
				Expect(actual).To(Equal(expected))
			},
			Entry("match add udev event",
				"KERNEL[1008.734088] add      /devices/pci0000:00/0000:00:07.0/virtio5/block/vdc (block)",
				[]string{"(?i)add", "(?i)remove"},
				[]string{"(?i)dm-[0-9]+"},
				true,
			),
			Entry("match remove udev event",
				"KERNEL[1008.734088] remove     /devices/pci0000:00/0000:00:07.0/virtio5/block/vdc (block)",
				[]string{"(?i)add", "(?i)remove"},
				[]string{"(?i)dm-[0-9]+"},
				true,
			),
			Entry("validate exclusion of change udev event",
				"KERNEL[1008.734088] change      /devices/pci0000:00/0000:00:07.0/virtio5/block/vdc (block)",
				[]string{"(?i)add", "(?i)remove"},
				[]string{"(?i)dm-[0-9]+"},
				false,
			),
			Entry("validate exclusion of event on dm device",
				"KERNEL[1042.464238] add      /devices/virtual/block/dm-1 (block)",
				[]string{"(?i)add", "(?i)remove"},
				[]string{"(?i)dm-[0-9]+"},
				false,
			),
		)

		Context("with invalid regex patterns", func() {
			It("should return error for invalid match regex pattern", func() {
				text := "KERNEL[1008.734088] add      /devices/pci0000:00/0000:00:07.0/virtio5/block/vdc (block)"
				matches := []string{"[invalid"}
				exclusions := []string{"(?i)dm-[0-9]+"}

				_, err := matchUdevEvent(text, matches, exclusions)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to search string"))
			})

			It("should return error for invalid exclusion regex pattern", func() {
				text := "KERNEL[1008.734088] add      /devices/pci0000:00/0000:00:07.0/virtio5/block/vdc (block)"
				matches := []string{"(?i)add"}
				exclusions := []string{"[invalid"}

				_, err := matchUdevEvent(text, matches, exclusions)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to search string"))
			})
		})
	})
})
