package machine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	tagservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/tags"
	compute "google.golang.org/api/compute/v1"
	googleapi "google.golang.org/api/googleapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	controllerfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type capacityTestOpts struct {
	name       string
	zone       string
	projectID  string
	providerID string
	status     *machinev1.GCPMachineProviderStatus
}

func newCapacityTestReconciler(t *testing.T, mock *computeservice.GCPComputeServiceMock, opts capacityTestOpts) *Reconciler {
	t.Helper()
	if opts.name == "" {
		opts.name = "capacity-machine"
	}
	if opts.zone == "" {
		opts.zone = "us-central1-a"
	}
	if opts.projectID == "" {
		opts.projectID = "test-project"
	}
	if opts.status == nil {
		opts.status = &machinev1.GCPMachineProviderStatus{}
	}
	gate, err := NewDefaultMutableFeatureGate(nil)
	if err != nil {
		t.Fatalf("failed to configure feature gates: %s", err.Error())
	}
	infraObj := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       configv1.InfrastructureSpec{PlatformSpec: configv1.PlatformSpec{Type: configv1.GCPPlatformType}},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: "test-748kjf",
			PlatformStatus:     &configv1.PlatformStatus{Type: configv1.GCPPlatformType, GCP: &configv1.GCPPlatformStatus{}},
		},
	}
	return newReconciler(&machineScope{
		machine: &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      opts.name,
				Namespace: "default",
				Labels: map[string]string{
					machinev1.MachineClusterIDLabel: "CLUSTERID",
				},
			},
		},
		coreClient: controllerfake.NewClientBuilder().WithObjects(infraObj).WithScheme(scheme.Scheme).Build(),
		providerSpec: &machinev1.GCPMachineProviderSpec{
			ProjectID:   opts.projectID,
			Region:      "us-central1",
			Zone:        opts.zone,
			MachineType: "n1-standard-1",
			Disks: []*machinev1.GCPDisk{
				{Boot: true, Image: "projects/fooproject/global/images/uefi-image"},
			},
		},
		providerStatus: opts.status,
		providerID:     opts.providerID,
		projectID:      opts.projectID,
		computeService: mock,
		featureGates:   gate,
		tagService:     tagservice.NewMockTagService(),
	})
}

func TestCreateSyncCapacityDeadline(t *testing.T) {
	syncCapacityErr := &googleapi.Error{
		Code:    503,
		Message: "The zone does not have enough resources available to fulfill the request.",
		Errors: []googleapi.ErrorItem{
			{Reason: "ZONE_RESOURCE_POOL_EXHAUSTED", Message: "The zone does not have enough resources available to fulfill the request."},
		},
	}

	newCreateReconciler := func(t *testing.T, status *machinev1.GCPMachineProviderStatus) (*Reconciler, *computeservice.GCPComputeServiceMock) {
		t.Helper()
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			return &compute.OperationList{}, nil
		}
		mock.MockInstancesInsert = func(project string, zone string, instance *compute.Instance) (*compute.Operation, error) {
			return nil, syncCapacityErr
		}
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return nil, &googleapi.Error{Code: 404, Message: "not found"}
		}
		return newCapacityTestReconciler(t, mock, capacityTestOpts{
			name:   "sync-capacity-machine",
			status: status,
		}), mock
	}

	t.Run("past 30m with empty op list returns InvalidMachineConfiguration", func(t *testing.T) {
		status := &machinev1.GCPMachineProviderStatus{
			Conditions: []metav1.Condition{{
				Type:               string(machinev1.MachineCreated),
				Status:             metav1.ConditionFalse,
				Reason:             machineCreationFailedReason,
				Message:            syncCapacityErr.Error(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-31 * time.Minute)),
			}},
		}
		r, mock := newCreateReconciler(t, status)
		insertCalled := false
		mock.MockInstancesInsert = func(project string, zone string, instance *compute.Instance) (*compute.Operation, error) {
			insertCalled = true
			return nil, syncCapacityErr
		}
		err := r.create()
		if err == nil {
			t.Fatal("expected terminal capacity deadline error")
		}
		if !isInvalidMachineConfigurationError(err) {
			t.Fatalf("expected InvalidMachineConfiguration, got %T %v", err, err)
		}
		if !strings.Contains(err.Error(), "capacity exhausted after") {
			t.Fatalf("expected deadline message, got %v", err)
		}
		if insertCalled {
			t.Fatal("InstancesInsert must not run after capacity deadline")
		}
		var requeueErr *machinecontroller.RequeueAfterError
		if errors.As(err, &requeueErr) {
			t.Fatalf("past deadline should not keep requeueing, got %v", err)
		}
	})

	t.Run("under 30m with empty op list still inserts then requeues", func(t *testing.T) {
		status := &machinev1.GCPMachineProviderStatus{
			Conditions: []metav1.Condition{{
				Type:               string(machinev1.MachineCreated),
				Status:             metav1.ConditionFalse,
				Reason:             machineCreationFailedReason,
				Message:            syncCapacityErr.Error(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
			}},
		}
		r, mock := newCreateReconciler(t, status)
		insertCalled := false
		mock.MockInstancesInsert = func(project string, zone string, instance *compute.Instance) (*compute.Operation, error) {
			insertCalled = true
			return nil, syncCapacityErr
		}
		err := r.create()
		if err == nil {
			t.Fatal("expected capacity requeue under deadline")
		}
		if isInvalidMachineConfigurationError(err) {
			t.Fatalf("under deadline must not return InvalidMachineConfiguration, got %v", err)
		}
		if !insertCalled {
			t.Fatal("expected InstancesInsert under deadline when insert op is missing")
		}
		var requeueErr *machinecontroller.RequeueAfterError
		if !errors.As(err, &requeueErr) {
			t.Fatalf("expected RequeueAfterError, got %v", err)
		}
		if requeueErr.RequeueAfter != requeueAfterSeconds*time.Second {
			t.Fatalf("expected %ds requeue, got %v", requeueAfterSeconds, requeueErr.RequeueAfter)
		}
	})
}

func TestWaitForRunningAndCapacityRetry(t *testing.T) {
	asyncFailureOp := failedInsertOperation("ZONE_RESOURCE_POOL_EXHAUSTED", testZoneResourcePoolExhaustedMessage)
	asyncFailureErr := mustOperationError(t, asyncFailureOp)
	instanceName := "capacity-machine"
	zone := "us-central1-a"
	projectID := "test-project"
	providerID := fmt.Sprintf("gce://%s/%s/%s", projectID, zone, instanceName)
	selfLink := testInstanceSelfLink(projectID, zone, instanceName)

	baseScope := func(t *testing.T, mock *computeservice.GCPComputeServiceMock, status *machinev1.GCPMachineProviderStatus) *Reconciler {
		t.Helper()
		return newCapacityTestReconciler(t, mock, capacityTestOpts{
			name:       instanceName,
			zone:       zone,
			projectID:  projectID,
			providerID: providerID,
			status:     status,
		})
	}

	assertNotProvisioned := func(t *testing.T, r *Reconciler) {
		t.Helper()
		if r.machine.Spec.ProviderID != nil {
			t.Fatalf("expected providerID unset, got %q", *r.machine.Spec.ProviderID)
		}
		if len(r.machine.Status.Addresses) != 0 {
			t.Fatalf("expected addresses unset, got %#v", r.machine.Status.Addresses)
		}
		cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
		if cond != nil && cond.Status == metav1.ConditionTrue {
			t.Fatalf("expected MachineCreated not True, got %#v", cond)
		}
	}

	t.Run("PROVISIONING does not set providerID", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return &compute.Instance{
				Name:   instanceName,
				Status: "PROVISIONING",
				NetworkInterfaces: []*compute.NetworkInterface{
					{NetworkIP: "10.0.0.15"},
				},
			}, nil
		}
		r := baseScope(t, mock, nil)
		err := r.update()
		var requeueErr *machinecontroller.RequeueAfterError
		if !errors.As(err, &requeueErr) {
			t.Fatalf("expected requeue, got %v", err)
		}
		assertNotProvisioned(t, r)
		if got := ptr.Deref(r.providerStatus.InstanceState, ""); got != "PROVISIONING" {
			t.Fatalf("expected InstanceState PROVISIONING, got %q", got)
		}
	})

	t.Run("STAGING does not set providerID", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return &compute.Instance{
				Name:   instanceName,
				Status: "STAGING",
				NetworkInterfaces: []*compute.NetworkInterface{
					{NetworkIP: "10.0.0.15"},
				},
			}, nil
		}
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			return &compute.OperationList{
				Items: []*compute.Operation{{Status: "DONE", TargetLink: selfLink}},
			}, nil
		}
		r := baseScope(t, mock, nil)
		err := r.update()
		var requeueErr *machinecontroller.RequeueAfterError
		if !errors.As(err, &requeueErr) {
			t.Fatalf("expected requeue, got %v", err)
		}
		assertNotProvisioned(t, r)
	})

	t.Run("capacity DONE while visible sets False and 20s requeue", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return &compute.Instance{
				Name:   instanceName,
				Status: "PROVISIONING",
				NetworkInterfaces: []*compute.NetworkInterface{
					{NetworkIP: "10.0.0.15"},
				},
			}, nil
		}
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			return &compute.OperationList{
				Items: []*compute.Operation{withTargetLink(asyncFailureOp, selfLink)},
			}, nil
		}
		mock.MockInstancesInsert = func(project string, zone string, instance *compute.Instance) (*compute.Operation, error) {
			t.Fatal("Update must not call InstancesInsert on capacity")
			return nil, nil
		}
		r := baseScope(t, mock, nil)
		err := r.update()
		var requeueErr *machinecontroller.RequeueAfterError
		if !errors.As(err, &requeueErr) {
			t.Fatalf("expected capacity requeue, got %v", err)
		}
		if requeueErr.RequeueAfter != requeueAfterSeconds*time.Second {
			t.Fatalf("expected %ds requeue, got %v", requeueAfterSeconds, requeueErr.RequeueAfter)
		}
		assertNotProvisioned(t, r)
		cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
		if cond == nil || cond.Status != metav1.ConditionFalse || cond.Message != asyncFailureErr.Error() {
			t.Fatalf("expected MachineCreated=False with capacity message, got %#v", cond)
		}
	})

	t.Run("Update non-capacity insert op requeues without InvalidMachineConfiguration", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return &compute.Instance{
				Name:   instanceName,
				Status: "PROVISIONING",
				NetworkInterfaces: []*compute.NetworkInterface{
					{NetworkIP: "10.0.0.15"},
				},
			}, nil
		}
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			return &compute.OperationList{
				Items: []*compute.Operation{
					withTargetLink(failedInsertOperation("INVALID_IMAGE", "bad image"), selfLink),
				},
			}, nil
		}
		r := baseScope(t, mock, nil)
		err := r.update()
		if isInvalidMachineConfigurationError(err) {
			t.Fatalf("Update should not return InvalidMachineConfiguration, got %v", err)
		}
		var requeueErr *machinecontroller.RequeueAfterError
		if !errors.As(err, &requeueErr) {
			t.Fatalf("expected requeue, got %v", err)
		}
		assertNotProvisioned(t, r)
		cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
		if cond == nil || cond.Status != metav1.ConditionFalse {
			t.Fatalf("expected MachineCreated=False, got %#v", cond)
		}
		if !strings.Contains(cond.Message, "INVALID_IMAGE") {
			t.Fatalf("expected INVALID_IMAGE in condition message, got %#v", cond)
		}
	})

	t.Run("Update past 30m capacity deadline keeps requeueing while visible", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return &compute.Instance{
				Name:   instanceName,
				Status: "STOPPING",
				NetworkInterfaces: []*compute.NetworkInterface{
					{NetworkIP: "10.0.0.15"},
				},
			}, nil
		}
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			return &compute.OperationList{
				Items: []*compute.Operation{withTargetLink(asyncFailureOp, selfLink)},
			}, nil
		}
		status := &machinev1.GCPMachineProviderStatus{
			Conditions: []metav1.Condition{{
				Type:               string(machinev1.MachineCreated),
				Status:             metav1.ConditionFalse,
				Reason:             machineCreationFailedReason,
				Message:            asyncFailureErr.Error(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-31 * time.Minute)),
			}},
		}
		r := baseScope(t, mock, status)
		err := r.update()
		if isInvalidMachineConfigurationError(err) {
			t.Fatalf("Update must not return InvalidMachineConfiguration past deadline while visible, got %v", err)
		}
		var requeueErr *machinecontroller.RequeueAfterError
		if !errors.As(err, &requeueErr) {
			t.Fatalf("expected capacity requeue, got %v", err)
		}
		if requeueErr.RequeueAfter != requeueAfterSeconds*time.Second {
			t.Fatalf("expected %ds requeue, got %v", requeueAfterSeconds, requeueErr.RequeueAfter)
		}
		assertNotProvisioned(t, r)
	})

	t.Run("RUNNING sets providerID without listing insert ops", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			t.Fatal("ZoneOperationsList must not run when instance is RUNNING")
			return nil, errors.New("zone operations list failed")
		}
		r := baseScope(t, mock, nil)
		if err := r.update(); err != nil {
			t.Fatalf("expected success when RUNNING, got %v", err)
		}
		if r.machine.Spec.ProviderID == nil {
			t.Fatal("expected providerID set when RUNNING")
		}
		cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
		if cond == nil || cond.Status != metav1.ConditionTrue {
			t.Fatalf("expected MachineCreated=True, got %#v", cond)
		}
	})

	t.Run("DONE capacity op re-inserts in the same Create", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		insertCalled := false
		mock.MockInstancesInsert = func(project string, zone string, instance *compute.Instance) (*compute.Operation, error) {
			insertCalled = true
			return &compute.Operation{Name: "retry-op", Status: "RUNNING"}, nil
		}
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return nil, &googleapi.Error{Code: 404, Message: "not found"}
		}
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			return &compute.OperationList{
				Items: []*compute.Operation{withTargetLink(asyncFailureOp, selfLink)},
			}, nil
		}
		status := &machinev1.GCPMachineProviderStatus{
			Conditions: []metav1.Condition{{
				Type:               string(machinev1.MachineCreated),
				Status:             metav1.ConditionFalse,
				Reason:             machineCreationFailedReason,
				Message:            asyncFailureErr.Error(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-30 * time.Second)),
			}},
		}
		r := baseScope(t, mock, status)
		err := r.create()
		if !insertCalled {
			t.Fatal("expected InstancesInsert after DONE capacity op")
		}
		if isInvalidMachineConfigurationError(err) {
			t.Fatalf("under deadline must not return InvalidMachineConfiguration, got %v", err)
		}
		var requeueErr *machinecontroller.RequeueAfterError
		if !errors.As(err, &requeueErr) {
			t.Fatalf("expected requeue after retry insert, got %v", err)
		}
		assertNotProvisioned(t, r)
		cond := findCondition(r.providerStatus.Conditions, string(machinev1.MachineCreated))
		if cond == nil || cond.Status != metav1.ConditionFalse {
			t.Fatalf("expected MachineCreated=False, got %#v", cond)
		}
	})

	t.Run("past 30m capacity deadline is terminal", func(t *testing.T) {
		_, mock := computeservice.NewComputeServiceMock()
		mock.MockZoneOperationsList = func(project string, zone string, filter string) (*compute.OperationList, error) {
			return &compute.OperationList{
				Items: []*compute.Operation{withTargetLink(asyncFailureOp, selfLink)},
			}, nil
		}
		mock.MockInstancesGet = func(project string, zone string, instance string) (*compute.Instance, error) {
			return nil, &googleapi.Error{Code: 404, Message: "not found"}
		}
		mock.MockInstancesInsert = func(project string, zone string, instance *compute.Instance) (*compute.Operation, error) {
			t.Fatal("InstancesInsert must not run after capacity deadline")
			return nil, nil
		}
		status := &machinev1.GCPMachineProviderStatus{
			Conditions: []metav1.Condition{{
				Type:               string(machinev1.MachineCreated),
				Status:             metav1.ConditionFalse,
				Reason:             machineCreationFailedReason,
				Message:            asyncFailureErr.Error(),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-31 * time.Minute)),
			}},
		}
		r := baseScope(t, mock, status)

		err := r.create()
		if err == nil {
			t.Fatal("expected terminal capacity deadline error")
		}
		if !isInvalidMachineConfigurationError(err) {
			t.Fatalf("expected InvalidMachineConfiguration, got %v", err)
		}
		if !strings.Contains(err.Error(), "capacity exhausted after") {
			t.Fatalf("expected deadline message, got %v", err)
		}
		if !strings.Contains(err.Error(), "ZONE_RESOURCE_POOL_EXHAUSTED") {
			t.Fatalf("expected capacity code in deadline error, got %v", err)
		}
		assertNotProvisioned(t, r)
	})
}
