package util

import (
	"reflect"
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	expectedProviderSpec = machinev1.GCPMachineProviderSpec{
		Zone:                   "us-east1-b",
		MachineType:            "n1-standard-1",
		Region:                 "us-east1",
		CanIPForward:           true,
		ShieldedInstanceConfig: machinev1.GCPShieldedInstanceConfig{SecureBoot: machinev1.SecureBootPolicyEnabled},
		UserDataSecret: &corev1.LocalObjectReference{
			Name: "myUserData",
		},
		NetworkInterfaces: []*machinev1.GCPNetworkInterface{
			{
				Subnetwork: "my-subnet",
			},
		},
	}
	expectedRawForProviderSpec = `{"metadata":{"creationTimestamp":null},"userDataSecret":{"name":"myUserData"},"canIPForward":true,"deletionProtection":false,"networkInterfaces":[{"subnetwork":"my-subnet"}],"serviceAccounts":null,"machineType":"n1-standard-1","region":"us-east1","zone":"us-east1-b","shieldedInstanceConfig":{"secureBoot":"Enabled"}}`

	instanceID             = "my-instance-id"
	instanceState          = "RUNNING"
	expectedProviderStatus = machinev1.GCPMachineProviderStatus{
		InstanceID:    &instanceID,
		InstanceState: &instanceState,
	}
	expectedRawForProviderStatus = `{"metadata":{"creationTimestamp":null},"instanceId":"my-instance-id","instanceState":"RUNNING"}`
)

func TestRawExtensionFromProviderSpec(t *testing.T) {
	rawExtension, err := RawExtensionFromProviderSpec(&expectedProviderSpec)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if string(rawExtension.Raw) != expectedRawForProviderSpec {
		t.Errorf("Expected: %s, got: %s", expectedRawForProviderSpec, string(rawExtension.Raw))
	}
}

func TestProviderSpecFromRawExtension(t *testing.T) {
	rawExtension := runtime.RawExtension{
		Raw: []byte(expectedRawForProviderSpec),
	}
	providerSpec, err := ProviderSpecFromRawExtension(&rawExtension)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if reflect.DeepEqual(providerSpec, expectedProviderSpec) {
		t.Errorf("Expected: %v, got: %v", expectedProviderSpec, providerSpec)
	}
}

func TestRawExtensionFromProviderStatus(t *testing.T) {
	rawExtension, err := RawExtensionFromProviderStatus(&expectedProviderStatus)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if string(rawExtension.Raw) != expectedRawForProviderStatus {
		t.Errorf("Expected: %s, got: %s", expectedRawForProviderStatus, string(rawExtension.Raw))
	}
}

func TestProviderStatusFromRawExtension(t *testing.T) {
	rawExtension := runtime.RawExtension{
		Raw: []byte(expectedRawForProviderStatus),
	}
	providerStatus, err := ProviderSpecFromRawExtension(&rawExtension)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if reflect.DeepEqual(providerStatus, expectedProviderStatus) {
		t.Errorf("Expected: %v, got: %v", expectedProviderStatus, providerStatus)
	}
}
