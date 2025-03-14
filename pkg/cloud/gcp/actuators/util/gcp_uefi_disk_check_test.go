package util_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	machinev1builder "github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder/machine/v1beta1"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	"github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/util"
)

var _ = Describe("IsUEFICompatible", func() {

	type standardImageInput struct {
		boot                 bool
		compatible           bool
		disks                []*machinev1.GCPDisk
		expectedErrSubstring string
		image                string
		projectID            string
		zone                 string
	}

	var tableFunc func(in standardImageInput) = func(in standardImageInput) {
		_, computeService := computeservice.NewComputeServiceMock()
		providerSpecBuilder := machinev1builder.GCPProviderSpec()

		if len(in.disks) != 0 {
			providerSpecBuilder = providerSpecBuilder.WithDisks(in.disks)
		} else {
			providerSpecBuilder = providerSpecBuilder.WithDisks([]*machinev1.GCPDisk{
				{
					Boot:  in.boot,
					Image: in.image,
				},
			})
		}

		if in.projectID != "" {
			providerSpecBuilder = providerSpecBuilder.WithProjectID(in.projectID)
		}

		if in.zone != "" {
			providerSpecBuilder = providerSpecBuilder.WithZone(in.zone)
		}

		providerSpec := providerSpecBuilder.Build()

		compatible, err := util.IsUEFICompatible(computeService, providerSpec)
		if in.expectedErrSubstring != "" {
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(in.expectedErrSubstring))
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(compatible).To(Equal(in.compatible))
	}

	DescribeTable("Standard image references in format projects/{project}/global/images/{image}",
		tableFunc,
		Entry("UEFI compatible image", standardImageInput{
			boot:       true,
			image:      "projects/fooproject/global/images/uefi-image",
			compatible: true,
		}),
		Entry("Non-UEFI image", standardImageInput{
			boot:       true,
			image:      "projects/fooproject/global/images/fooimage",
			compatible: false,
		}),
		Entry("Image not found", standardImageInput{
			boot:                 true,
			image:                "projects/errImageNotFound/global/images/fooimage",
			expectedErrSubstring: "unable to retrieve image",
			compatible:           false,
		}),
		Entry("Malformed image reference", standardImageInput{
			boot:                 true,
			image:                "projects/errImageNotFound//asdf/images456/fooimage",
			expectedErrSubstring: "unrecognized image path format",
			compatible:           false,
		}),
	)

	DescribeTable("Simple image names",
		tableFunc,
		Entry("Simple UEFI compatible image", standardImageInput{
			boot:       true,
			image:      "uefi-image",
			projectID:  "simple-project",
			compatible: true,
		}),
		Entry("Simple non-UEFI image", standardImageInput{
			boot:       true,
			image:      "non-uefi",
			projectID:  "simple-project",
			compatible: false,
		}),
		Entry("Simple image not found", standardImageInput{
			boot:                 true,
			image:                "nonexistent",
			projectID:            "errImageNotFound",
			expectedErrSubstring: "unable to retrieve image",
			compatible:           false,
		}),
	)

	DescribeTable("Family image references in format projects/{project}/global/images/family/{imageFamily}",
		tableFunc,
		Entry("UEFI compatible image family", standardImageInput{
			boot:       true,
			image:      "projects/fooproject/global/images/family/uefi-image-family",
			zone:       "us-central1-a",
			compatible: true,
		}),
		Entry("Non-UEFI image family", standardImageInput{
			boot:       true,
			image:      "projects/fooproject/global/images/family/fooimage",
			zone:       "us-central1-a",
			compatible: false,
		}),
		Entry("Image family not found", standardImageInput{
			boot:                 true,
			image:                "projects/errImageNotFound/global/images/family/fooimage",
			zone:                 "us-central1-a",
			expectedErrSubstring: "unable to retrieve image family",
			compatible:           false,
		}),
	)

	DescribeTable("FQDN image URLs",
		tableFunc,
		Entry("UEFI compatible FQDN image", standardImageInput{
			boot:       true,
			image:      "https://www.googleapis.com/compute/v1/projects/fooproject/global/images/uefi-image",
			compatible: true,
		}),
		Entry("Non-UEFI FQDN image", standardImageInput{
			boot:       true,
			image:      "https://www.googleapis.com/compute/v1/projects/fooproject/global/images/fooimage",
			compatible: false,
		}),
		Entry("FQDN URL missing 'projects/' segment", standardImageInput{
			boot:                 true,
			image:                "https://www.googleapis.com/compute/v1/global/images/uefi-image",
			expectedErrSubstring: "does not contain expected 'projects/'",
			compatible:           false,
		}),
		Entry("FQDN URL with incomplete segments", standardImageInput{
			boot:                 true,
			image:                "https://www.googleapis.com/compute/v1/projects/fooproject/global/images",
			expectedErrSubstring: "unexpected image path format",
			compatible:           false,
		}),
		Entry("FQDN URL not following recognized pattern", standardImageInput{
			boot:                 true,
			image:                "https://www.googleapis.com/compute/v1/projects/fooproject/global/foo/uefi-image",
			expectedErrSubstring: "unrecognized image path format",
			compatible:           false,
		}),
	)

	DescribeTable("Disks are not standard",
		tableFunc,
		Entry("When no boot disk is found", standardImageInput{
			compatible: false,
			disks: []*machinev1.GCPDisk{
				{
					Boot:  false,
					Image: "projects/fooproject/global/images/uefi-image",
				},
				{
					Boot:  false,
					Image: "projects/fooproject/global/images/fooimage",
				},
			},
			expectedErrSubstring: "no boot disk found",
		}),
		Entry("When the boot disk is not the first disk in the list", standardImageInput{
			compatible: true,
			disks: []*machinev1.GCPDisk{
				{
					Boot:  false,
					Image: "non-uefi-simple",
				},
				{
					Boot:  true,
					Image: "uefi-image",
				},
			},
			projectID: "simple-project",
		}),
	)

})
