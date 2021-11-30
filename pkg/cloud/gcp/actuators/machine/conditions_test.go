package machine

import (
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

func TestShouldUpdateCondition(t *testing.T) {
	testCases := []struct {
		oldCondition machinev1.GCPMachineProviderCondition
		newCondition machinev1.GCPMachineProviderCondition
		expected     bool
	}{
		{
			oldCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			newCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			expected: false,
		},
		{
			oldCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			newCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "different reason",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			expected: true,
		},
		{
			oldCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "foo",
				Message: "different message",
				Status:  corev1.ConditionTrue,
			},
			newCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			expected: true,
		},
		{
			oldCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			newCondition: machinev1.GCPMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionFalse,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		if got := shouldUpdateCondition(tc.oldCondition, tc.newCondition); got != tc.expected {
			t.Errorf("Expected %t, got %t", got, tc.expected)
		}
	}
}
