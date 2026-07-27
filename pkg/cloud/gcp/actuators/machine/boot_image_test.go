package machine

import (
	"context"
	"fmt"
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	"github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/util"
	compute "google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	controllerfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testStreamJSON = `{
	"stream": "stable",
	"metadata": {"last-modified": "2024-01-01T00:00:00Z"},
	"architectures": {
		"x86_64": {
			"artifacts": {},
			"images": {
				"gcp": {
					"release": "418.stable",
					"project": "rhcos-cloud",
					"name": "rhcos-418-stable-x86-64"
				}
			}
		},
		"aarch64": {
			"artifacts": {},
			"images": {
				"gcp": {
					"release": "418.stable",
					"project": "rhcos-cloud",
					"name": "rhcos-418-stable-aarch64"
				}
			}
		}
	}
}`

func testBootImagesConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      coreOSBootImagesName,
			Namespace: coreOSBootImagesNamespace,
		},
		Data: map[string]string{
			"stream": testStreamJSON,
		},
	}
}

func TestResolveBootImage(t *testing.T) {
	cases := []struct {
		name               string
		machineType        string
		mockMachineType    *compute.MachineType
		mockMachineTypeErr error
		configMap          *corev1.ConfigMap
		expectedImage      string
	}{
		{
			name:        "x86_64 machine type resolves from ConfigMap",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "X86_64",
			},
			configMap:     testBootImagesConfigMap(),
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-x86-64"),
		},
		{
			name:        "ARM64 machine type resolves from ConfigMap",
			machineType: "t2a-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "ARM64",
			},
			configMap:     testBootImagesConfigMap(),
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-aarch64"),
		},
		{
			name:        "missing ConfigMap falls back to x86 default",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "X86_64",
			},
			configMap:     nil,
			expectedImage: defaultGCPBootImageX86,
		},
		{
			name:        "missing ConfigMap falls back to ARM default",
			machineType: "t2a-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "ARM64",
			},
			configMap:     nil,
			expectedImage: defaultGCPBootImageARM,
		},
		{
			name:               "MachineType API failure uses prefix-based arch and fallback image",
			machineType:        "n2-standard-4",
			mockMachineTypeErr: fmt.Errorf("API unavailable"),
			configMap:          nil,
			expectedImage:      defaultGCPBootImageX86,
		},
		{
			name:               "MachineType API failure with ARM prefix uses ARM fallback",
			machineType:        "t2a-standard-4",
			mockMachineTypeErr: fmt.Errorf("API unavailable"),
			configMap:          nil,
			expectedImage:      defaultGCPBootImageARM,
		},
		{
			name:        "empty Architecture field falls back to prefix-based detection",
			machineType: "t2a-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "",
			},
			configMap:     testBootImagesConfigMap(),
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-aarch64"),
		},
		{
			name:        "ARCHITECTURE_UNSPECIFIED falls back to prefix-based detection",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "ARCHITECTURE_UNSPECIFIED",
			},
			configMap:     testBootImagesConfigMap(),
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-x86-64"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockComputeService := &computeservice.GCPComputeServiceMock{
				MockMachineTypesGet: func(project, zone, mt string) (*compute.MachineType, error) {
					if tc.mockMachineTypeErr != nil {
						return nil, tc.mockMachineTypeErr
					}
					return tc.mockMachineType, nil
				},
			}

			clientBuilder := controllerfake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.configMap != nil {
				clientBuilder.WithObjects(tc.configMap)
			}
			fakeClient := clientBuilder.Build()

			r := &Reconciler{
				machineScope: &machineScope{
					Context:        context.Background(),
					coreClient:     fakeClient,
					computeService: mockComputeService,
					projectID:      "test-project",
					providerSpec: &machinev1.GCPMachineProviderSpec{
						MachineType: tc.machineType,
						Zone:        "us-central1-a",
					},
				},
			}

			image, err := r.resolveBootImage()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if image != tc.expectedImage {
				t.Errorf("expected image %q, got %q", tc.expectedImage, image)
			}
		})
	}
}

func TestArchToStreamArch(t *testing.T) {
	cases := []struct {
		input    util.NormalizedArch
		expected string
	}{
		{util.ArchitectureArm64, "aarch64"},
		{util.ArchitectureAmd64, "x86_64"},
		{util.NormalizedArch("unknown"), "x86_64"},
	}
	for _, tc := range cases {
		t.Run(string(tc.input), func(t *testing.T) {
			got := archToStreamArch(tc.input)
			if got != tc.expected {
				t.Errorf("archToStreamArch(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestGcpImageReference(t *testing.T) {
	got := gcpImageReference("rhcos-cloud", "rhcos-418-stable-x86-64")
	expected := "projects/rhcos-cloud/global/images/rhcos-418-stable-x86-64"
	if got != expected {
		t.Errorf("gcpImageReference() = %q, want %q", got, expected)
	}
}
