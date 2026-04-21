package machine

import (
	"strings"
	"testing"

	compute "google.golang.org/api/compute/v1"
)

func TestClassifyInsertOp(t *testing.T) {
	runningOp := &compute.Operation{Status: "RUNNING"}
	cleanDone := &compute.Operation{Status: "DONE"}
	runningCapacity := &compute.Operation{
		Status: "RUNNING",
		Error: &compute.OperationError{
			Errors: []*compute.OperationErrorErrors{{Code: "ZONE_RESOURCE_POOL_EXHAUSTED"}},
		},
	}
	capacityPool := &compute.Operation{
		Status: "DONE",
		Error: &compute.OperationError{
			Errors: []*compute.OperationErrorErrors{
				{
					Code:    "ZONE_RESOURCE_POOL_EXHAUSTED",
					Message: "The zone 'zones/us-central1-a' does not have enough resources available to fulfill the request.",
				},
			},
		},
	}
	terminal := &compute.Operation{
		Status: "DONE",
		Error: &compute.OperationError{
			Errors: []*compute.OperationErrorErrors{{Code: "INVALID_IMAGE", Message: "bad image"}},
		},
	}
	firstOfMany := &compute.Operation{
		Status: "DONE",
		Error: &compute.OperationError{
			Errors: []*compute.OperationErrorErrors{
				{Code: "ZONE_RESOURCE_POOL_EXHAUSTED", Message: "no resources"},
				{Code: "INVALID_IMAGE", Message: "bad image"},
			},
		},
	}
	skipNilElem := &compute.Operation{
		Status: "DONE",
		Error: &compute.OperationError{
			Errors: []*compute.OperationErrorErrors{
				nil,
				{Code: "ZONE_RESOURCE_POOL_EXHAUSTED", Message: "no resources"},
			},
		},
	}
	onlyNilElems := &compute.Operation{
		Status: "DONE",
		Error: &compute.OperationError{
			Errors: []*compute.OperationErrorErrors{nil, nil},
		},
	}

	cases := []struct {
		name      string
		op        *compute.Operation
		wantClass insertOperationClass
		wantErr   string
	}{
		{name: "nil", op: nil, wantClass: insertOperationAbsent},
		{name: "RUNNING", op: runningOp, wantClass: insertOperationInFlight},
		{name: "RUNNING with capacity error is still in flight", op: runningCapacity, wantClass: insertOperationInFlight},
		{name: "DONE clean", op: cleanDone, wantClass: insertOperationSucceeded},
		{name: "DONE capacity ZONE_RESOURCE_POOL_EXHAUSTED", op: capacityPool, wantClass: insertOperationCapacityFailed, wantErr: "GCP operation failed: ZONE_RESOURCE_POOL_EXHAUSTED: The zone 'zones/us-central1-a' does not have enough resources available to fulfill the request."},
		{name: "DONE terminal INVALID_IMAGE", op: terminal, wantClass: insertOperationTerminalFailed, wantErr: "GCP operation failed: INVALID_IMAGE: bad image"},
		{name: "multiple errors first wins", op: firstOfMany, wantClass: insertOperationCapacityFailed, wantErr: "GCP operation failed: ZONE_RESOURCE_POOL_EXHAUSTED: no resources"},
		{name: "nil error elements are skipped", op: skipNilElem, wantClass: insertOperationCapacityFailed, wantErr: "GCP operation failed: ZONE_RESOURCE_POOL_EXHAUSTED: no resources"},
		{name: "only nil error elements is succeeded DONE", op: onlyNilElems, wantClass: insertOperationSucceeded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, err := classifyInsertOperation(tc.op)
			if class != tc.wantClass {
				t.Fatalf("classifyInsertOperation() class=%v, want %v", class, tc.wantClass)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("classifyInsertOperation() err=%v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("classifyInsertOperation() err=%v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLatestMatchingInsertOp(t *testing.T) {
	link := testCreateInstanceSelfLink()
	// Zone ops populate insertTime. creationTimestamp is often empty.
	older := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		InsertTime: "2026-01-01T00:00:00.000-00:00",
		Name:       "older",
	}
	newer := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		InsertTime: "2026-08-05T16:00:00.000-00:00",
		Name:       "newer",
	}

	got := latestMatchingInsertOp(&compute.OperationList{
		Items: []*compute.Operation{older, nil, newer},
	})
	if got == nil || got.Name != "newer" {
		t.Fatalf("expected newest op, got %#v", got)
	}

	if latestMatchingInsertOp(nil) != nil {
		t.Fatal("expected nil for nil list")
	}

	// One op only has creationTimestamp, the other only insertTime.
	byCreationOnly := &compute.Operation{
		Status:            "DONE",
		TargetLink:        link,
		CreationTimestamp: "2026-08-05T17:00:00.000-00:00",
		Name:              "creation-only-newer",
	}
	byInsertOnly := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		InsertTime: "2026-08-05T16:00:00.000-00:00",
		Name:       "insert-only-older",
	}
	got = latestMatchingInsertOp(&compute.OperationList{
		Items: []*compute.Operation{byInsertOnly, byCreationOnly},
	})
	if got == nil || got.Name != "creation-only-newer" {
		t.Fatalf("expected creationTimestamp-fallback op to win when newer, got %#v", got)
	}

	// 10:00-07:00 is 17:00 UTC, newer than 16:00Z. String compare would pick 16:00Z.
	offsetNewer := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		InsertTime: "2026-08-05T10:00:00-07:00",
		Name:       "offset-newer",
	}
	zuluOlder := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		InsertTime: "2026-08-05T16:00:00Z",
		Name:       "zulu-older",
	}
	got = latestMatchingInsertOp(&compute.OperationList{
		Items: []*compute.Operation{zuluOlder, offsetNewer},
	})
	if got == nil || got.Name != "offset-newer" {
		t.Fatalf("expected the offset timestamp to win, got %#v", got)
	}

	emptyTime := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		Name:       "empty-time",
	}
	got = latestMatchingInsertOp(&compute.OperationList{
		Items: []*compute.Operation{emptyTime},
	})
	if got == nil || got.Name != "empty-time" {
		t.Fatalf("expected lone op with empty timestamps kept, got %#v", got)
	}

	unparsed := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		InsertTime: "not-a-timestamp",
		Name:       "unparsed",
	}
	parsed := &compute.Operation{
		Status:     "DONE",
		TargetLink: link,
		InsertTime: "2026-08-05T16:00:00Z",
		Name:       "parsed",
	}
	got = latestMatchingInsertOp(&compute.OperationList{
		Items: []*compute.Operation{unparsed, parsed},
	})
	if got == nil || got.Name != "parsed" {
		t.Fatalf("expected unparsable timestamps skipped, got %#v", got)
	}

	// Empty time must not sort as time zero (oldest) ahead of a dated op.
	got = latestMatchingInsertOp(&compute.OperationList{
		Items: []*compute.Operation{emptyTime, parsed},
	})
	if got == nil || got.Name != "parsed" {
		t.Fatalf("expected dated op over empty timestamps, got %#v", got)
	}

	got = latestMatchingInsertOp(&compute.OperationList{Items: []*compute.Operation{unparsed}})
	if got == nil || got.Name != "unparsed" {
		t.Fatalf("expected lone unparsable op kept, got %#v", got)
	}
}

func TestInsertOperationTargetLinkFilter(t *testing.T) {
	path := "/projects/openshift-gce-devel/zones/us-central1-f/instances/rmanak-dev-3-6j6w5-worker-f-227l5"
	www := `targetLink="https://www.googleapis.com/compute/v1` + path + `"`

	computeBase := "https://compute.googleapis.com/compute/v1" + path
	filter := insertOperationTargetLinkFilter(path, computeBase)
	if strings.Contains(filter, `targetLink:"`) {
		t.Fatalf("filter used targetLink: HAS, got %q", filter)
	}
	if !strings.Contains(filter, `operationType="insert"`) {
		t.Fatalf("expected operationType insert equality, got %q", filter)
	}
	if !strings.Contains(filter, www) {
		t.Fatalf("expected www host equality in filter, got %q", filter)
	}
	if !strings.Contains(filter, `targetLink="`+computeBase+`"`) {
		t.Fatalf("expected distinct BasePath in filter, got %q", filter)
	}
	if strings.Count(filter, `targetLink="`) != 2 {
		t.Fatalf("expected 2 targetLink clauses (www + distinct BasePath), got %q", filter)
	}

	wwwSelfLink := "https://www.googleapis.com/compute/v1" + path
	filter = insertOperationTargetLinkFilter(path, wwwSelfLink)
	if strings.Count(filter, `targetLink="`) != 1 {
		t.Fatalf("expected 1 clause when BasePath is already www, got %q", filter)
	}

	sovereign := "https://compute.example-universe.goog/compute/v1" + path
	filter = insertOperationTargetLinkFilter(path, sovereign)
	if !strings.Contains(filter, `targetLink="`+sovereign+`"`) {
		t.Fatalf("expected distinct BasePath host in filter, got %q", filter)
	}
	if !strings.Contains(filter, www) {
		t.Fatalf("expected www host retained alongside sovereign BasePath, got %q", filter)
	}
	if strings.Count(filter, `targetLink="`) != 2 {
		t.Fatalf("expected 2 targetLink clauses (www + sovereign BasePath), got %q", filter)
	}
}
