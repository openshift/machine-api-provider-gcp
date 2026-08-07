package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	"github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/util"
	compute "google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	controllerfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testStreamRHEL9JSON = `{
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

const testStreamRHEL10JSON = `{
	"stream": "stable",
	"metadata": {"last-modified": "2025-06-01T00:00:00Z"},
	"architectures": {
		"x86_64": {
			"artifacts": {},
			"images": {
				"gcp": {
					"release": "420.stable",
					"project": "rhcos-cloud",
					"name": "rhcos-420-stable-x86-64"
				}
			}
		},
		"aarch64": {
			"artifacts": {},
			"images": {
				"gcp": {
					"release": "420.stable",
					"project": "rhcos-cloud",
					"name": "rhcos-420-stable-aarch64"
				}
			}
		}
	}
}`

func testStreamsJSON() string {
	streams := map[string]json.RawMessage{
		"rhel-9":  json.RawMessage(testStreamRHEL9JSON),
		"rhel-10": json.RawMessage(testStreamRHEL10JSON),
	}
	data, _ := json.Marshal(streams)
	return string(data)
}

func testBootImagesConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      coreOSBootImagesName,
			Namespace: coreOSBootImagesNamespace,
		},
		Data: map[string]string{
			"streams": testStreamsJSON(),
		},
	}
}

func testBootImagesConfigMapLegacy() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      coreOSBootImagesName,
			Namespace: coreOSBootImagesNamespace,
		},
		Data: map[string]string{
			"stream": testStreamRHEL9JSON,
		},
	}
}

func testOSImageStream(t *testing.T, defaultStream string) *unstructured.Unstructured {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(osImageStreamGVK)
	obj.SetName(osImageStreamName)
	if err := unstructured.SetNestedField(obj.Object, defaultStream, "spec", "defaultStream"); err != nil {
		t.Fatalf("failed to set defaultStream on OSImageStream fixture: %v", err)
	}
	return obj
}

func TestResolveBootImage(t *testing.T) {
	cases := []struct {
		name               string
		machineType        string
		mockMachineType    *compute.MachineType
		mockMachineTypeErr error
		configMap          *corev1.ConfigMap
		osImageStream      *unstructured.Unstructured
		expectedImage      string
	}{
		{
			name:        "x86_64 resolves rhel-10 image when OSImageStream defaults to rhel-10",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "X86_64",
			},
			configMap:     testBootImagesConfigMap(),
			osImageStream: testOSImageStream(t, "rhel-10"),
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-420-stable-x86-64"),
		},
		{
			name:        "ARM64 resolves rhel-10 image when OSImageStream defaults to rhel-10",
			machineType: "t2a-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "ARM64",
			},
			configMap:     testBootImagesConfigMap(),
			osImageStream: testOSImageStream(t, "rhel-10"),
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-420-stable-aarch64"),
		},
		{
			name:        "x86_64 resolves rhel-9 image when OSImageStream defaults to rhel-9",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "X86_64",
			},
			configMap:     testBootImagesConfigMap(),
			osImageStream: testOSImageStream(t, "rhel-9"),
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-x86-64"),
		},
		{
			name:        "defaults to rhel-9 when OSImageStream CR not found",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "X86_64",
			},
			configMap:     testBootImagesConfigMap(),
			osImageStream: nil,
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-x86-64"),
		},
		{
			name:        "falls back to deprecated stream key when streams key missing",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "X86_64",
			},
			configMap:     testBootImagesConfigMapLegacy(),
			osImageStream: nil,
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-x86-64"),
		},
		{
			name:        "missing ConfigMap falls back to x86 default",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "X86_64",
			},
			configMap:     nil,
			osImageStream: nil,
			expectedImage: defaultGCPBootImageX86,
		},
		{
			name:        "missing ConfigMap falls back to ARM default",
			machineType: "t2a-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "ARM64",
			},
			configMap:     nil,
			osImageStream: nil,
			expectedImage: defaultGCPBootImageARM,
		},
		{
			name:               "MachineType API failure uses prefix-based arch and fallback image",
			machineType:        "n2-standard-4",
			mockMachineTypeErr: fmt.Errorf("API unavailable"),
			configMap:          nil,
			osImageStream:      nil,
			expectedImage:      defaultGCPBootImageX86,
		},
		{
			name:               "MachineType API failure with ARM prefix uses ARM fallback",
			machineType:        "t2a-standard-4",
			mockMachineTypeErr: fmt.Errorf("API unavailable"),
			configMap:          nil,
			osImageStream:      nil,
			expectedImage:      defaultGCPBootImageARM,
		},
		{
			name:        "empty Architecture field falls back to prefix-based detection",
			machineType: "t2a-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "",
			},
			configMap:     testBootImagesConfigMap(),
			osImageStream: nil,
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-aarch64"),
		},
		{
			name:        "ARCHITECTURE_UNSPECIFIED falls back to prefix-based detection",
			machineType: "n2-standard-4",
			mockMachineType: &compute.MachineType{
				Architecture: "ARCHITECTURE_UNSPECIFIED",
			},
			configMap:     testBootImagesConfigMap(),
			osImageStream: nil,
			expectedImage: gcpImageReference("rhcos-cloud", "rhcos-418-stable-x86-64"),
		},
	}

	osImageStreamGVR := schema.GroupVersionResource{
		Group:    osImageStreamGVK.Group,
		Version:  osImageStreamGVK.Version,
		Resource: "osimagestreams",
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

			s := runtime.NewScheme()
			if err := scheme.AddToScheme(s); err != nil {
				t.Fatalf("failed to add scheme: %v", err)
			}
			s.AddKnownTypeWithName(
				osImageStreamGVK,
				&unstructured.Unstructured{},
			)

			clientBuilder := controllerfake.NewClientBuilder().WithScheme(s)
			if tc.configMap != nil {
				clientBuilder.WithObjects(tc.configMap)
			}
			if tc.osImageStream != nil {
				clientBuilder.WithRESTMapper(newFakeRESTMapper(osImageStreamGVK, osImageStreamGVR))
				clientBuilder.WithObjects(tc.osImageStream)
			}
			fakeClient := clientBuilder.Build()

			r := &Reconciler{
				machineScope: &machineScope{
					Context:        context.Background(),
					coreClient:     fakeClient,
					apiReader:      fakeClient,
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

func newFakeRESTMapper(gvk schema.GroupVersionKind, gvr schema.GroupVersionResource) meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
	m.Add(gvk, meta.RESTScopeRoot)
	return m
}
