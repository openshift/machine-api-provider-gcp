package machine

// This is a thin layer to implement the machine actuator interface with cloud provider details.
// The lifetime of scope and reconciler is a machine actuator operation.
// when scope is closed, it will persist to etcd the given machine spec and machine status (if modified)
import (
	"context"
	"fmt"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	controllerclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	scopeFailFmt      = "%s: failed to create scope for machine: %w"
	reconcilerFailFmt = "%s: reconciler failed to %s machine: %w"
	createEventAction = "Create"
	updateEventAction = "Update"
	deleteEventAction = "Delete"
	noEventAction     = ""
)

// Actuator is responsible for performing machine reconciliation.
type Actuator struct {
	coreClient           controllerclient.Client
	eventRecorder        record.EventRecorder
	computeClientBuilder computeservice.BuilderFuncType
}

// ActuatorParams holds parameter information for Actuator.
type ActuatorParams struct {
	CoreClient           controllerclient.Client
	EventRecorder        record.EventRecorder
	ComputeClientBuilder computeservice.BuilderFuncType
}

// NewActuator returns an actuator.
func NewActuator(params ActuatorParams) *Actuator {
	return &Actuator{
		coreClient:           params.CoreClient,
		eventRecorder:        params.EventRecorder,
		computeClientBuilder: params.ComputeClientBuilder,
	}
}

// Set corresponding event based on error. It also returns the original error
// for convenience, so callers can do "return handleMachineError(...)".
func (a *Actuator) handleMachineError(machine *machinev1.Machine, err error, eventAction string) error {
	klog.Errorf("%v error: %v", machine.GetName(), err)
	if eventAction != noEventAction {
		a.eventRecorder.Eventf(machine, corev1.EventTypeWarning, "Failed"+eventAction, "%v", err)
	}
	return err
}

// Create creates a machine and is invoked by the machine controller.
func (a *Actuator) Create(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: Creating machine", machine.Name)
	klog.Infof("----------------------- DEBUG ---------------------------")
	scope, err := newMachineScope(machineScopeParams{
		Context:              ctx,
		coreClient:           a.coreClient,
		machine:              machine,
		computeClientBuilder: a.computeClientBuilder,
		eventRecorder:        a.eventRecorder,
	})
	if err != nil {
		fmtErr := fmt.Errorf(scopeFailFmt, machine.GetName(), err)
		return a.handleMachineError(machine, fmtErr, createEventAction)
	}
	if err := newReconciler(scope).create(); err != nil {
		// Update machine and machine status in case it was modified
		scope.Close()
		fmtErr := fmt.Errorf(reconcilerFailFmt, machine.GetName(), createEventAction, err)
		return a.handleMachineError(machine, fmtErr, createEventAction)
	}
	a.eventRecorder.Eventf(machine, corev1.EventTypeNormal, createEventAction, "Created Machine %v", machine.Name)
	return scope.Close()
}

func (a *Actuator) Exists(ctx context.Context, machine *machinev1.Machine) (bool, error) {
	klog.Infof("%s: Checking if machine exists", machine.Name)
	scope, err := newMachineScope(machineScopeParams{
		Context:              ctx,
		coreClient:           a.coreClient,
		machine:              machine,
		computeClientBuilder: a.computeClientBuilder,
	})
	if err != nil {
		return false, fmt.Errorf(scopeFailFmt, machine.Name, err)
	}
	// The core machine controller calls exists() + create()/update() in the same reconciling operation.
	// If exists() would store machineSpec/status object then create()/update() would still receive the local version.
	// When create()/update() try to store machineSpec/status this might result in
	// "Operation cannot be fulfilled; the object has been modified; please apply your changes to the latest version and try again."
	// Therefore we don't close the scope here and we only store spec/status atomically either in create()/update()"
	exists, err := newReconciler(scope).exists()
	if !isInvalidMachineConfigurationError(err) {
		return exists, err
	}

	// If the machine has, e.g. invalid zone, and it doesn't have a phase set yet,
	// we have to make sure the phase goes as "Failed".
	if machine.Status.Phase == nil {
		machine.Status.Phase = pointer.String("Failed")
	}

	// If the machine has, e.g. invalid zone, and we delete the invalid machinset,
	// we want to set the machine to "Deleting" phase and return nil as error.
	// We need the error to be nil so we can successfully delete the invalid machine.
	// If the machine has a provider ID, then we believe something exists in the cloud.
	// So we must not all the deletion if the provider ID is set.
	if *machine.Status.Phase == "Deleting" && machine.Spec.ProviderID == nil {
		return false, nil
	}

	return exists, err
}

func (a *Actuator) Update(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: Updating machine", machine.Name)
	scope, err := newMachineScope(machineScopeParams{
		Context:              ctx,
		coreClient:           a.coreClient,
		machine:              machine,
		computeClientBuilder: a.computeClientBuilder,
	})
	if err != nil {
		fmtErr := fmt.Errorf(scopeFailFmt, machine.GetName(), err)
		return a.handleMachineError(machine, fmtErr, updateEventAction)
	}
	if err := newReconciler(scope).update(); err != nil {
		// Update machine and machine status in case it was modified
		scope.Close()
		fmtErr := fmt.Errorf(reconcilerFailFmt, machine.GetName(), updateEventAction, err)
		return a.handleMachineError(machine, fmtErr, updateEventAction)
	}

	previousResourceVersion := scope.machine.ResourceVersion

	if err := scope.Close(); err != nil {
		return err
	}

	currentResourceVersion := scope.machine.ResourceVersion

	// Create event only if machine object was modified
	if previousResourceVersion != currentResourceVersion {
		a.eventRecorder.Eventf(machine, corev1.EventTypeNormal, updateEventAction, "Updated Machine %v", machine.Name)
	}

	return nil
}

func (a *Actuator) Delete(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: Deleting machine", machine.Name)
	scope, err := newMachineScope(machineScopeParams{
		Context:              ctx,
		coreClient:           a.coreClient,
		machine:              machine,
		computeClientBuilder: a.computeClientBuilder,
	})
	if err != nil {
		fmtErr := fmt.Errorf(scopeFailFmt, machine.GetName(), err)
		return a.handleMachineError(machine, fmtErr, deleteEventAction)
	}
	if err := newReconciler(scope).delete(); err != nil {
		fmtErr := fmt.Errorf(reconcilerFailFmt, machine.GetName(), deleteEventAction, err)
		return a.handleMachineError(machine, fmtErr, deleteEventAction)
	}
	a.eventRecorder.Eventf(machine, corev1.EventTypeNormal, deleteEventAction, "Deleted machine %v", machine.Name)
	return nil
}
