package machine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	capacityRetryDeadline = 30 * time.Minute
)

// Pool exhausted. Retry these; they are not a bad MachineSpec.
// https://cloud.google.com/compute/docs/reference/rest/v1/errors
var capacityErrorCodes = sets.NewString(
	"ZONE_RESOURCE_POOL_EXHAUSTED",
	"ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS",
	"REGION_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS",
	"RESOURCE_POOL_EXHAUSTED",
)

func isCapacityOperationError(op *compute.Operation) bool {
	if op == nil || op.Error == nil {
		return false
	}
	for _, e := range op.Error.Errors {
		if e != nil && capacityErrorCodes.Has(e.Code) {
			return true
		}
	}
	return false
}

func isCapacityAPIError(err error) bool {
	var googleError *googleapi.Error
	if !errors.As(err, &googleError) {
		return false
	}
	for _, item := range googleError.Errors {
		if capacityErrorCodes.Has(item.Reason) {
			return true
		}
	}
	return false
}

func messageHasCapacityErrorCode(msg string) bool {
	for _, code := range capacityErrorCodes.List() {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// Keep the 30m create deadline from MachineCreated=False when list missed the insert op.
func (r *Reconciler) capacityErrorFromCondition() (error, bool) {
	cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		return nil, false
	}
	if !messageHasCapacityErrorCode(cond.Message) {
		return nil, false
	}
	return fmt.Errorf("%s", cond.Message), true
}

func (r *Reconciler) createCapacityMayInsert(err error) error {
	r.recordFailedInstanceCreate(err)
	cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.LastTransitionTime.IsZero() {
		return nil
	}
	if time.Since(cond.LastTransitionTime.Time) >= capacityRetryDeadline {
		return machinecontroller.InvalidMachineConfiguration("capacity exhausted after %s: %v", capacityRetryDeadline, err)
	}
	return nil
}

func (r *Reconciler) requeueCapacityOnCreate(err error) error {
	if deadlineErr := r.createCapacityMayInsert(err); deadlineErr != nil {
		return deadlineErr
	}
	return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
}

// MAO ignores InvalidMachineConfiguration on Update, so this only requeues.
func (r *Reconciler) requeueCapacityOnUpdate(err error) error {
	r.recordFailedInstanceCreate(err)
	return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
}
