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
	"fmt"
	"strconv"
	"strings"

	"github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/util"

	"github.com/go-logr/logr"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	mapierrors "github.com/openshift/machine-api-operator/pkg/controller/machine"
	mapiutil "github.com/openshift/machine-api-operator/pkg/util"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	// This exposes compute information based on the providerSpec input.
	// This is needed by the autoscaler to foresee upcoming capacity when scaling from zero.
	// https://github.com/openshift/enhancements/pull/186
	cpuKey    = "machine.openshift.io/vCPU"
	memoryKey = "machine.openshift.io/memoryMb"
	gpuKey    = "machine.openshift.io/GPU"
	labelsKey = "capacity.cluster-autoscaler.kubernetes.io/labels"
)

// Reconciler reconciles machineSets.
type Reconciler struct {
	Client client.Client
	Log    logr.Logger

	recorder record.EventRecorder
	scheme   *runtime.Scheme
	cache    *machineTypesCache

	// Allow a mock GCPComputeService to be injected during testing
	getGCPService func(namespace string, providerConfig machinev1.GCPMachineProviderSpec) (computeservice.GCPComputeService, error)
}

// SetupWithManager creates a new controller for a manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&machinev1.MachineSet{}).
		WithOptions(options).
		Build(r)

	if err != nil {
		return fmt.Errorf("failed setting up with a controller manager: %w", err)
	}

	r.cache = newMachineTypesCache()
	r.recorder = mgr.GetEventRecorderFor("machineset-controller")
	r.scheme = mgr.GetScheme()

	if r.getGCPService == nil {
		r.getGCPService = r.getRealGCPService
	}
	return nil
}

// Reconcile implements controller runtime Reconciler interface.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("machineset", req.Name, "namespace", req.Namespace)
	logger.V(3).Info("Reconciling")

	machineSet := &machinev1.MachineSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, machineSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Ignore deleted MachineSets, this can happen when foregroundDeletion
	// is enabled
	if !machineSet.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	originalMachineSetToPatch := client.MergeFrom(machineSet.DeepCopy())

	result, err := r.reconcile(machineSet)
	if err != nil {
		logger.Error(err, "Failed to reconcile MachineSet")
		r.recorder.Eventf(machineSet, corev1.EventTypeWarning, "ReconcileError", "%v", err)
		// we don't return here so we want to attempt to patch the machine regardless of an error.
	}

	if err := r.Client.Patch(ctx, machineSet, originalMachineSetToPatch); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch machineSet: %v", err)
	}

	if isInvalidConfigurationError(err) {
		// For situations where requeuing won't help we don't return error.
		// https://github.com/kubernetes-sigs/controller-runtime/issues/617
		return result, nil
	}

	return result, err
}

func isInvalidConfigurationError(err error) bool {
	switch t := err.(type) {
	case *mapierrors.MachineError:
		if t.Reason == machinev1.InvalidConfigurationMachineError {
			return true
		}
	}
	return false
}

func (r *Reconciler) reconcile(machineSet *machinev1.MachineSet) (ctrl.Result, error) {
	providerConfig, err := getproviderConfig(machineSet)
	if err != nil {
		return ctrl.Result{}, mapierrors.InvalidMachineConfiguration("failed to get providerConfig: %v", err)
	}

	gceService, err := r.getGCPService(machineSet.GetNamespace(), *providerConfig)
	if err != nil {
		return ctrl.Result{}, err
	}

	machineType, err := r.cache.getMachineTypeFromCache(gceService, providerConfig.ProjectID, providerConfig.Zone, providerConfig.MachineType)
	if err != nil {
		return ctrl.Result{}, mapierrors.InvalidMachineConfiguration("error fetching machine type %q: %v", providerConfig.MachineType, err)
	} else if machineType == nil {
		// Returning no error to prevent further reconciliation, as user intervention is now required but emit an informational event
		r.recorder.Eventf(machineSet, corev1.EventTypeWarning, "FailedUpdate", "Failed to set autoscaling from zero annotations, machine type unknown")
		return ctrl.Result{}, nil
	}

	if machineSet.Annotations == nil {
		machineSet.Annotations = make(map[string]string)
	}

	// TODO: get annotations keys from machine API
	machineSet.Annotations[cpuKey] = strconv.FormatInt(machineType.GuestCpus, 10)
	machineSet.Annotations[memoryKey] = strconv.FormatInt(machineType.MemoryMb, 10)

	switch {
	case len(providerConfig.GPUs) > 0:
		// Guest accelerators will always be max size of 1
		machineSet.Annotations[gpuKey] = strconv.FormatInt(int64(providerConfig.GPUs[0].Count), 10)
	case len(machineType.Accelerators) > 0:
		// Accelerators will always be max size of 1
		machineSet.Annotations[gpuKey] = strconv.FormatInt(machineType.Accelerators[0].GuestAcceleratorCount, 10)
	default:
		machineSet.Annotations[gpuKey] = strconv.FormatInt(0, 10)
	}

	// We guarantee that any existing labels provided via the capacity annotations are preserved.
	// See https://github.com/kubernetes/autoscaler/pull/5382 and https://github.com/kubernetes/autoscaler/pull/5697
	machineSet.Annotations[labelsKey] = mapiutil.MergeCommaSeparatedKeyValuePairs(
		fmt.Sprintf("kubernetes.io/arch=%s", util.CPUArchitecture(providerConfig.MachineType)),
		machineSet.Annotations[labelsKey])

	// When upgrading from 4.12 on GCP marketplace, the MachineSets refer to images that do not support UEFI & shielded VMs.
	// However, GCP defaulted to shielded VMs sometime between 4.12 and 4.13.
	// Therefore, we should disable the shielded instance config in the MachineSet's template, so that new Machines created from it will boot.
	uefiCompatible, err := isUEFICompatible(gceService, providerConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error fetching disk information: %s", err)
	}

	if !uefiCompatible && providerConfig.ShieldedInstanceConfig == (machinev1.GCPShieldedInstanceConfig{}) {
		providerConfig.ShieldedInstanceConfig = machinev1.GCPShieldedInstanceConfig{
			SecureBoot:                       machinev1.SecureBootPolicyDisabled,
			VirtualizedTrustedPlatformModule: machinev1.VirtualizedTrustedPlatformModulePolicyDisabled,
			IntegrityMonitoring:              machinev1.IntegrityMonitoringPolicyDisabled,
		}

		ext, err := util.RawExtensionFromProviderSpec(providerConfig)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("could not marshal shielded instance config: %s", err)
		}

		machineSet.Spec.Template.Spec.ProviderSpec.Value = ext
	}
	return ctrl.Result{}, nil
}

func getproviderConfig(machineSet *machinev1.MachineSet) (*machinev1.GCPMachineProviderSpec, error) {
	return util.ProviderSpecFromRawExtension(machineSet.Spec.Template.Spec.ProviderSpec.Value)
}

// getRealGCPService constructs a real GCPService for talking to GCP
func (r *Reconciler) getRealGCPService(namespace string, providerConfig machinev1.GCPMachineProviderSpec) (computeservice.GCPComputeService, error) {
	serviceAccountJSON, err := util.GetCredentialsSecret(r.Client, namespace, providerConfig)
	if err != nil {
		return nil, err
	}

	computeService, err := computeservice.NewComputeService(serviceAccountJSON)
	if err != nil {
		return nil, mapierrors.InvalidMachineConfiguration("error creating compute service: %v", err)
	}
	return computeService, nil
}

// isUEFICompatible will detect if the machine's boot disk was created with a UEFI image.
// Shielded VMs can only be made with UEFI-compatible images. However, OpenShift images listed on the GCP marketplace
// are not updated with every release, and the 4.8 image is used until 4.12 and was not created with UEFI support.
func isUEFICompatible(gceService computeservice.GCPComputeService, providerConfig *machinev1.GCPMachineProviderSpec) (bool, error) {
	for _, disk := range providerConfig.Disks {
		if !disk.Boot {
			continue
		}
		img, err := gceService.ImageGet(providerConfig.ProjectID, disk.Image)
		if err != nil {
			return false, fmt.Errorf("unable to retrieve image %s in project %s: %s", disk.Image, providerConfig.ProjectID, err)
		}
		for _, feat := range img.GuestOsFeatures {
			if strings.Contains(feat.Type, util.UEFICompatible) {
				return true, nil
			}
		}

	}
	return false, nil
}
