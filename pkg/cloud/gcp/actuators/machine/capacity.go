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

// allCapacity is true when every non-empty code is pool-exhausted and at least one exists.
// mixed is stockout plus any other code.
func classifyCapacityCodes(codes []string) (allCapacity, mixed bool) {
	sawCapacity := false
	sawOther := false
	for _, code := range codes {
		if code == "" {
			continue
		}
		if capacityErrorCodes.Has(code) {
			sawCapacity = true
			continue
		}
		sawOther = true
	}
	return sawCapacity && !sawOther, sawCapacity && sawOther
}

func allCapacityCodes(codes []string) bool {
	all, _ := classifyCapacityCodes(codes)
	return all
}

func isCapacityOperationError(op *compute.Operation) bool {
	if op == nil || op.Error == nil {
		return false
	}
	codes := make([]string, 0, len(op.Error.Errors))
	for _, e := range op.Error.Errors {
		if e == nil {
			continue
		}
		codes = append(codes, e.Code)
	}
	return allCapacityCodes(codes)
}

func googleAPIErrorReasons(err error) []string {
	var googleError *googleapi.Error
	if !errors.As(err, &googleError) {
		return nil
	}
	codes := make([]string, 0, len(googleError.Errors))
	for _, item := range googleError.Errors {
		codes = append(codes, item.Reason)
	}
	return codes
}

func isCapacityAPIError(err error) bool {
	return allCapacityCodes(googleAPIErrorReasons(err))
}

func isMixedAPIError(err error) bool {
	_, mixed := classifyCapacityCodes(googleAPIErrorReasons(err))
	return mixed
}

const gcpOperationFailedPrefix = "GCP operation failed: "

// Codes recorded on MachineCreated. Same all-must-be-capacity rule as ops/API errors.
func errorCodesInMessage(msg string) []string {
	if i := strings.Index(msg, gcpOperationFailedPrefix); i >= 0 {
		rest := msg[i+len(gcpOperationFailedPrefix):]
		codes := make([]string, 0)
		for _, part := range strings.Split(rest, "; ") {
			code, _, _ := strings.Cut(part, ":")
			code = strings.TrimSpace(code)
			if code != "" {
				codes = append(codes, code)
			}
		}
		return codes
	}
	var codes []string
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Reason: ") {
			continue
		}
		reason, _, _ := strings.Cut(strings.TrimPrefix(line, "Reason: "), ",")
		reason = strings.TrimSpace(reason)
		if reason != "" {
			codes = append(codes, reason)
		}
	}
	if len(codes) > 0 {
		return codes
	}
	if i := strings.LastIndex(msg, ", "); i >= 0 {
		maybe := strings.TrimSpace(msg[i+2:])
		if maybe != "" && !strings.ContainsAny(maybe, " \n") {
			return []string{maybe}
		}
	}
	return nil
}

func messageIsCapacityClass(msg string) bool {
	return allCapacityCodes(errorCodesInMessage(msg))
}

// Keep the 30m create deadline from MachineCreated=False when list missed the insert op.
func (r *Reconciler) capacityErrorFromCondition() error {
	cond := r.capacityFalseCondition()
	if cond == nil {
		return nil
	}
	return fmt.Errorf("%s", cond.Message)
}

func (r *Reconciler) capacityFalseCondition() *metav1.Condition {
	cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
	if cond == nil || cond.Status != metav1.ConditionFalse || !messageIsCapacityClass(cond.Message) {
		return nil
	}
	return cond
}

func (r *Reconciler) createCapacityMayInsert(err error) error {
	if interruptibleFromSpec(r.providerSpec.Preemptible, r.providerSpec.ProvisioningModel) {
		return r.failInstanceCreate(err)
	}
	prev := r.capacityFalseCondition()
	alreadyCapacity := prev != nil
	var previousTransition metav1.Time
	if alreadyCapacity {
		previousTransition = prev.LastTransitionTime
	}
	r.recordFailedInstanceCreate(err)
	cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		return nil
	}
	if alreadyCapacity {
		cond.LastTransitionTime = previousTransition
		if previousTransition.IsZero() {
			return nil
		}
		if time.Since(previousTransition.Time) >= capacityRetryDeadline {
			return machinecontroller.InvalidMachineConfiguration("capacity exhausted after %s: %v", capacityRetryDeadline, err)
		}
		return nil
	}
	cond.LastTransitionTime = metav1.Now()
	return nil
}

func (r *Reconciler) requeueCapacityOnCreate(err error) error {
	if deadlineErr := r.createCapacityMayInsert(err); deadlineErr != nil {
		return deadlineErr
	}
	return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
}

func isCreateClientError(err error) bool {
	var googleError *googleapi.Error
	if !errors.As(err, &googleError) {
		return false
	}
	return googleError.Code >= 400 && googleError.Code < 500
}

// Keep a capacity-class False through 5xx so the 30m clock stays.
func (r *Reconciler) recordFailedInstanceCreateUnlessCapacity(err error) {
	if r.capacityFalseCondition() != nil {
		return
	}
	r.recordFailedInstanceCreate(err)
}

// MAO ignores InvalidMachineConfiguration on Update, so this only requeues.
func (r *Reconciler) requeueCapacityOnUpdate(err error) error {
	r.recordFailedInstanceCreate(err)
	return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
}
