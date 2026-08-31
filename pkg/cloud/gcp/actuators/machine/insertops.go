package machine

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/compute/v1"
)

const insertOperationAgeSkew = 2 * time.Minute

func (r *Reconciler) latestVisibleInsertOperation() (*compute.Operation, error) {
	selfLink := r.fmtInstanceSelfLink(r.projectID, r.providerSpec.Zone, r.machine.Name)
	expectedPath := resourcePath(selfLink)
	// targetLink:"path" returns 0 rows. Equality works.
	filter := insertOperationTargetLinkFilter(expectedPath, selfLink)
	opList, err := r.computeService.ZoneOperationsList(r.projectID, r.providerSpec.Zone, filter)
	if err != nil {
		return nil, err
	}
	if opList == nil {
		return nil, nil
	}
	// Drop ops dated before Machine.CreationTimestamp minus insertOperationAgeSkew. Untimed ops stay.
	cutoff := r.machine.CreationTimestamp.Time.Add(-insertOperationAgeSkew)
	kept := make([]*compute.Operation, 0, len(opList.Items))
	for _, op := range opList.Items {
		if op == nil {
			continue
		}
		t, ok := operationTime(op)
		if ok && t.Before(cutoff) {
			continue
		}
		kept = append(kept, op)
	}
	return latestMatchingInsertOp(&compute.OperationList{Items: kept}), nil
}

// Live insert ops store www.googleapis.com. The SDK BasePath can be a different host.
func insertOperationTargetLinkFilter(expectedPath, basePathSelfLink string) string {
	www := "https://www.googleapis.com/compute/v1" + expectedPath
	eqs := []string{fmt.Sprintf(`targetLink="%s"`, www)}
	if basePathSelfLink != "" && basePathSelfLink != www {
		eqs = append(eqs, fmt.Sprintf(`targetLink="%s"`, basePathSelfLink))
	}
	if len(eqs) == 1 {
		return fmt.Sprintf(`operationType="insert" AND %s`, eqs[0])
	}
	return fmt.Sprintf(`operationType="insert" AND (%s)`, strings.Join(eqs, " OR "))
}

// operationTime is the op's insertTime, or creationTimestamp if insertTime is empty.
// The bool is whether that string parsed as RFC3339Nano and can be used to order ops.
// Empty and unparsable timestamps return false so they are not treated as time zero.
func operationTime(op *compute.Operation) (time.Time, bool) {
	if op == nil {
		return time.Time{}, false
	}
	s := op.InsertTime
	if s == "" {
		s = op.CreationTimestamp
	}
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// List order is by name. filter+orderBy is 400, so pick newest insertTime here.
// Ops with no usable timestamp are skipped unless none of the items parsed; then
// the last non-nil item is returned so a lone empty-time op is still visible.
func latestMatchingInsertOp(opList *compute.OperationList) *compute.Operation {
	if opList == nil {
		return nil
	}
	var latest *compute.Operation
	var latestTime time.Time
	var fallback *compute.Operation
	for _, op := range opList.Items {
		if op == nil {
			continue
		}
		fallback = op
		t, ok := operationTime(op)
		if !ok {
			continue
		}
		if latest == nil || t.After(latestTime) {
			latest, latestTime = op, t
		}
	}
	if latest != nil {
		return latest
	}
	return fallback
}

func operationError(op *compute.Operation) error {
	if op == nil || op.Error == nil {
		return nil
	}
	parts := make([]string, 0, len(op.Error.Errors))
	for _, e := range op.Error.Errors {
		if e == nil {
			continue
		}
		switch {
		case e.Code != "" && e.Message != "":
			parts = append(parts, fmt.Sprintf("%s: %s", e.Code, e.Message))
		case e.Code != "":
			parts = append(parts, e.Code)
		case e.Message != "":
			parts = append(parts, e.Message)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("GCP operation failed: %s", strings.Join(parts, "; "))
}

type insertOperationClass int

const (
	insertOperationAbsent insertOperationClass = iota
	insertOperationInFlight
	insertOperationSucceeded
	insertOperationCapacityFailed
	insertOperationTerminalFailed
)

func classifyInsertOperation(op *compute.Operation) (insertOperationClass, error) {
	if op == nil {
		return insertOperationAbsent, nil
	}
	// Error is unset until DONE. Classify failures only then so a RUNNING op
	// is not retried as a capacity miss.
	if op.Status != "DONE" {
		return insertOperationInFlight, nil
	}
	if operationErr := operationError(op); operationErr != nil {
		if isCapacityOperationError(op) {
			return insertOperationCapacityFailed, operationErr
		}
		return insertOperationTerminalFailed, operationErr
	}
	return insertOperationSucceeded, nil
}
