/*
Copyright The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package machineset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gtypes "github.com/onsi/gomega/types"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// A mock giving some machine type options for testing
var mockMachineTypesFunc = func(_ string, _ string, machineType string) (*compute.MachineType, error) {
	switch machineType {
	// t2a-standard-2 is an arm64 instance type, but this information is not provided by the compute.MachineType struct
	case "n1-standard-2", "t2a-standard-2":
		return &compute.MachineType{
			GuestCpus: 2,
			MemoryMb:  7680,
		}, nil
	case "n2-highcpu-16":
		return &compute.MachineType{
			GuestCpus: 16,
			MemoryMb:  16384,
		}, nil
	case "a2-highgpu-2g":
		return &compute.MachineType{
			GuestCpus: 24,
			MemoryMb:  174080,
			Accelerators: []*compute.MachineTypeAccelerators{
				{
					GuestAcceleratorCount: 2,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown machineType: %s", machineType)
	}
}

var _ = Describe("Reconciler", func() {
	var c client.Client
	var stopMgr context.CancelFunc
	var fakeRecorder *record.FakeRecorder
	var namespace *corev1.Namespace

	BeforeEach(func() {
		mgr, err := manager.New(cfg, manager.Options{
			Metrics: server.Options{
				BindAddress: "0",
			},
		})
		Expect(err).ToNot(HaveOccurred())

		_, service := computeservice.NewComputeServiceMock()
		service.MockMachineTypesGet = mockMachineTypesFunc

		r := Reconciler{
			Client: mgr.GetClient(),
			Log:    log.Log,

			getGCPService: func(_ string, _ machinev1.GCPMachineProviderSpec) (computeservice.GCPComputeService, error) {
				return service, nil
			},
		}
		Expect(r.SetupWithManager(mgr, controller.Options{})).To(Succeed())

		fakeRecorder = record.NewFakeRecorder(1)
		r.recorder = fakeRecorder

		c = mgr.GetClient()
		stopMgr = StartTestManager(mgr)

		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "mhc-test-"}}
		Expect(c.Create(ctx, namespace)).To(Succeed())
	})

	AfterEach(func() {
		Expect(deleteMachineSets(c, namespace.Name)).To(Succeed())
		stopMgr()
	})

	type reconcileTestCase = struct {
		machineType         string
		guestAccelerators   []machinev1.GCPGPUConfig
		existingAnnotations map[string]string
		expectedAnnotations map[string]string
		expectedEvents      []string
	}

	DescribeTable("when reconciling MachineSets", func(rtc reconcileTestCase) {
		machineSet, err := newTestMachineSet(namespace.Name, rtc.machineType, rtc.guestAccelerators, rtc.existingAnnotations, nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(c.Create(ctx, machineSet)).To(Succeed())

		Eventually(func() map[string]string {
			m := &machinev1.MachineSet{}
			key := client.ObjectKey{Namespace: machineSet.Namespace, Name: machineSet.Name}
			err := c.Get(ctx, key, m)
			if err != nil {
				return nil
			}
			annotations := m.GetAnnotations()
			if annotations != nil {
				return annotations
			}
			// Return an empty map to distinguish between empty annotations and errors
			return make(map[string]string)
		}, timeout).Should(Equal(rtc.expectedAnnotations))

		// Check which event types were sent
		Eventually(fakeRecorder.Events, timeout).Should(HaveLen(len(rtc.expectedEvents)))
		receivedEvents := []string{}
		eventMatchers := []gtypes.GomegaMatcher{}
		for _, ev := range rtc.expectedEvents {
			receivedEvents = append(receivedEvents, <-fakeRecorder.Events)
			eventMatchers = append(eventMatchers, ContainSubstring(fmt.Sprintf(" %s ", ev)))
		}
		Expect(receivedEvents).To(ConsistOf(eventMatchers))
	},
		Entry("with no vmSize set", reconcileTestCase{
			machineType:         "",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: make(map[string]string),
			expectedEvents:      []string{"ReconcileError"},
		}),
		Entry("with a n1-standard-2", reconcileTestCase{
			machineType:         "n1-standard-2",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "7680",
				gpuKey:    "0",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectedEvents: []string{},
		}),
		Entry("with a t2a-standard-2 (arm64)", reconcileTestCase{
			machineType:         "t2a-standard-2",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "7680",
				gpuKey:    "0",
				labelsKey: "kubernetes.io/arch=arm64",
			},
			expectedEvents: []string{},
		}),
		Entry("with a n1-standard-2 and with guestAccelerators", reconcileTestCase{
			machineType:         "n1-standard-2",
			guestAccelerators:   []machinev1.GCPGPUConfig{{Type: "nvidia-tesla-p100", Count: 2}},
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "7680",
				gpuKey:    "2",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectedEvents: []string{},
		}),
		Entry("with a n2-highcpu-16", reconcileTestCase{
			machineType:         "n2-highcpu-16",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "16",
				memoryKey: "16384",
				gpuKey:    "0",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectedEvents: []string{},
		}),
		Entry("with a a2-highgpu-2g", reconcileTestCase{
			machineType:         "a2-highgpu-2g",
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "24",
				memoryKey: "174080",
				gpuKey:    "2",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectedEvents: []string{},
		}),
		Entry("with existing annotations", reconcileTestCase{
			machineType: "n1-standard-2",
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
				cpuKey:     "2",
				memoryKey:  "7680",
				gpuKey:     "0",
				labelsKey:  "kubernetes.io/arch=amd64",
			},
			expectedEvents: []string{},
		}),
		Entry("with an invalid machineType", reconcileTestCase{
			machineType: "r4.xLarge",
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedEvents: []string{"ReconcileError"},
		}),
	)
})

func deleteMachineSets(c client.Client, namespaceName string) error {
	machineSets := &machinev1.MachineSetList{}
	err := c.List(ctx, machineSets, client.InNamespace(namespaceName))
	if err != nil {
		return err
	}

	for _, ms := range machineSets.Items {
		err := c.Delete(ctx, &ms)
		if err != nil {
			return err
		}
	}

	Eventually(func() error {
		machineSets := &machinev1.MachineSetList{}
		err := c.List(ctx, machineSets)
		if err != nil {
			return err
		}
		if len(machineSets.Items) > 0 {
			return errors.New("MachineSets not deleted")
		}
		return nil
	}, timeout).Should(Succeed())

	return nil
}

func TestReconcile(t *testing.T) {
	testCases := []struct {
		name                string
		machineType         string
		guestAccelerators   []machinev1.GCPGPUConfig
		mockMachineTypesGet func(project string, zone string, machineType string) (*compute.MachineType, error)
		existingAnnotations map[string]string
		expectedAnnotations map[string]string
		expectedEvents      []string
		expectErr           bool
	}{
		{
			name:        "with no machineType set",
			machineType: "",
			mockMachineTypesGet: func(_ string, _ string, _ string) (*compute.MachineType, error) {
				return nil, errors.New("machineType should not be empty")
			},
			existingAnnotations: make(map[string]string),
			expectedAnnotations: make(map[string]string),
			expectErr:           true,
		},
		{
			name:        "with machineType not found by GCP",
			machineType: "",
			mockMachineTypesGet: func(_ string, _ string, _ string) (*compute.MachineType, error) {
				return nil, &googleapi.Error{Code: 404, Message: "Machine type is not found"}
			},
			existingAnnotations: make(map[string]string),
			expectedAnnotations: make(map[string]string),
			// Controller should stop reconciling on unknown instance type reported by the cloud
			expectErr: false,
		},
		{
			name:                "with a n1-standard-2",
			machineType:         "n1-standard-2",
			mockMachineTypesGet: mockMachineTypesFunc,
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "7680",
				gpuKey:    "0",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectErr: false,
		},
		{
			name:                "with a t2a-standard-2 (arm64)",
			machineType:         "t2a-standard-2",
			mockMachineTypesGet: mockMachineTypesFunc,
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "7680",
				gpuKey:    "0",
				labelsKey: "kubernetes.io/arch=arm64",
			},
			expectErr: false,
		},
		{
			name:                "with a n1-standard-2and guestAccelerators",
			machineType:         "n1-standard-2",
			guestAccelerators:   []machinev1.GCPGPUConfig{{Type: "nvidia-tesla-p100", Count: 2}},
			mockMachineTypesGet: mockMachineTypesFunc,
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "2",
				memoryKey: "7680",
				gpuKey:    "2",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectErr: false,
		},
		{
			name:                "with a n2-highcpu-16",
			machineType:         "n2-highcpu-16",
			mockMachineTypesGet: mockMachineTypesFunc,
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "16",
				memoryKey: "16384",
				gpuKey:    "0",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectErr: false,
		},
		{
			name:                "with a a2-highgpu-2g",
			machineType:         "a2-highgpu-2g",
			mockMachineTypesGet: mockMachineTypesFunc,
			existingAnnotations: make(map[string]string),
			expectedAnnotations: map[string]string{
				cpuKey:    "24",
				memoryKey: "174080",
				gpuKey:    "2",
				labelsKey: "kubernetes.io/arch=amd64",
			},
			expectErr: false,
		},
		{
			name:                "with existing annotations",
			machineType:         "n1-standard-2",
			mockMachineTypesGet: mockMachineTypesFunc,
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
				cpuKey:     "2",
				memoryKey:  "7680",
				gpuKey:     "0",
				labelsKey:  "kubernetes.io/arch=amd64",
			},
			expectErr: false,
		},
		{
			name:                "with an invalid machineType",
			machineType:         "r4.xLarge",
			mockMachineTypesGet: mockMachineTypesFunc,
			existingAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectedAnnotations: map[string]string{
				"existing": "annotation",
				"annother": "existingAnnotation",
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(tt *testing.T) {
			g := NewWithT(tt)

			_, service := computeservice.NewComputeServiceMock()
			if tc.mockMachineTypesGet != nil {
				service.MockMachineTypesGet = tc.mockMachineTypesGet
			}

			r := &Reconciler{
				recorder: record.NewFakeRecorder(1),
				cache:    newMachineTypesCache(),
				getGCPService: func(_ string, _ machinev1.GCPMachineProviderSpec) (computeservice.GCPComputeService, error) {
					return service, nil
				},
			}

			machineSet, err := newTestMachineSet("default", tc.machineType, tc.guestAccelerators, tc.existingAnnotations, nil)
			g.Expect(err).ToNot(HaveOccurred())

			_, err = r.reconcile(machineSet)
			g.Expect(err != nil).To(Equal(tc.expectErr))
			g.Expect(machineSet.Annotations).To(Equal(tc.expectedAnnotations))
		})
	}
}

func TestReconcileDisks(t *testing.T) {
	testCases := []struct {
		name           string
		disks          []*machinev1.GCPDisk
		expectDisabled bool
	}{
		{
			name: "boot disk without UEFI",
			disks: []*machinev1.GCPDisk{
				{Boot: true},
			},
			expectDisabled: true,
		},
		{
			name: "boot disk with UEFI",
			disks: []*machinev1.GCPDisk{
				{Boot: true, Image: "uefi"},
			},
			expectDisabled: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(tt *testing.T) {
			g := NewWithT(tt)
			machineSet, err := newTestMachineSet("default", "n1-standard-2", nil, make(map[string]string), tc.disks)
			g.Expect(err).ToNot(HaveOccurred())

			_, service := computeservice.NewComputeServiceMock()
			service.MockMachineTypesGet = mockMachineTypesFunc
			r := &Reconciler{
				recorder: record.NewFakeRecorder(1),
				cache:    newMachineTypesCache(),
				getGCPService: func(_ string, _ machinev1.GCPMachineProviderSpec) (computeservice.GCPComputeService, error) {
					return service, nil
				},
			}

			_, err = r.reconcile(machineSet)
			g.Expect(err).NotTo(HaveOccurred())

			providerConfig, err := getproviderConfig(machineSet)
			g.Expect(err).NotTo(HaveOccurred())

			if tc.expectDisabled {
				g.Expect(providerConfig.ShieldedInstanceConfig.SecureBoot).To(Equal(machinev1.SecureBootPolicyDisabled))
				g.Expect(providerConfig.ShieldedInstanceConfig.IntegrityMonitoring).To(Equal(machinev1.IntegrityMonitoringPolicyDisabled))
				g.Expect(providerConfig.ShieldedInstanceConfig.VirtualizedTrustedPlatformModule).To(Equal(machinev1.VirtualizedTrustedPlatformModulePolicyDisabled))
			}

			if !tc.expectDisabled {
				g.Expect(providerConfig.ShieldedInstanceConfig).To(BeEquivalentTo(machinev1.GCPShieldedInstanceConfig{}))
			}

		})
	}
}

func newTestMachineSet(namespace string, machineType string, guestAccelerators []machinev1.GCPGPUConfig, existingAnnotations map[string]string, disks []*machinev1.GCPDisk) (*machinev1.MachineSet, error) {
	// Copy anntotations map so we don't modify the input
	annotations := make(map[string]string)
	for k, v := range existingAnnotations {
		annotations[k] = v
	}

	machineProviderSpec := &machinev1.GCPMachineProviderSpec{
		MachineType: machineType,
		GPUs:        guestAccelerators,
		Disks:       disks,
	}
	providerSpec, err := providerSpecFromMachine(machineProviderSpec)
	if err != nil {
		return nil, err
	}

	return &machinev1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations:  annotations,
			GenerateName: "test-machineset-",
			Namespace:    namespace,
		},
		Spec: machinev1.MachineSetSpec{
			Template: machinev1.MachineTemplateSpec{
				Spec: machinev1.MachineSpec{
					ProviderSpec: providerSpec,
				},
			},
		},
	}, nil
}

func providerSpecFromMachine(in *machinev1.GCPMachineProviderSpec) (machinev1.ProviderSpec, error) {
	bytes, err := json.Marshal(in)
	if err != nil {
		return machinev1.ProviderSpec{}, err
	}
	return machinev1.ProviderSpec{
		Value: &runtime.RawExtension{Raw: bytes},
	}, nil
}
