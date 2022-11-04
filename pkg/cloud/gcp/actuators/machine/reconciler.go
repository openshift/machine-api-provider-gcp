package machine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/util/windows"
	"google.golang.org/api/compute/v1"
	googleapi "google.golang.org/api/googleapi"
	corev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	userDataSecretKey         = "userData"
	requeueAfterSeconds       = 20
	instanceLinkFmt           = "https://www.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s"
	kmsKeyNameFmt             = "projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s"
	machineTypeFmt            = "zones/%s/machineTypes/%s"
	acceleratorTypeFmt        = "zones/%s/acceleratorTypes/%s"
	windowsScriptMetadataKey  = "sysprep-specialize-script-ps1"
	openshiftMachineRoleLabel = "machine.openshift.io/cluster-api-machine-role"
	masterMachineRole         = "master"
)

// Reconciler are list of services required by machine actuator, easy to create a fake
type Reconciler struct {
	*machineScope
}

// NewReconciler populates all the services based on input scope
func newReconciler(scope *machineScope) *Reconciler {
	return &Reconciler{
		scope,
	}
}

var (
	supportedGpuTypes = map[string]string{
		"nvidia-tesla-k80":  "NVIDIA_K80_GPUS",
		"nvidia-tesla-p100": "NVIDIA_P100_GPUS",
		"nvidia-tesla-v100": "NVIDIA_V100_GPUS",
		"nvidia-tesla-a100": "NVIDIA_A100_GPUS",
		"nvidia-tesla-p4":   "NVIDIA_P4_GPUS",
		"nvidia-tesla-t4":   "NVIDIA_T4_GPUS",
	}
)

func containsString(sli []string, str string) bool {
	for _, elem := range sli {
		if elem == str {
			return true
		}
	}
	return false
}

func restartPolicyToBool(policy machinev1.GCPRestartPolicyType) *bool {
	restart := policy == machinev1.RestartPolicyAlways
	return &restart
}

// machineTypeAcceleratorCount represents nvidia-tesla-A100 GPUs which are only compatible with A2 machine family
func (r *Reconciler) checkQuota(machineTypeAcceleratorCount int64) error {
	region, err := r.computeService.RegionGet(r.projectID, r.providerSpec.Region)
	if err != nil {
		return machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("Failed to get region %s via compute service: %v", r.providerSpec.Region, err))
	}
	quotas := region.Quotas
	var guestAccelerators = []machinev1.GCPGPUConfig{}
	// When the machine type has associated accelerator instances (A2 machine family), accelerators will be nvidia-tesla-A100s.
	// Additional guest accelerators are not allowed so ignore the providerSpec GuestAccelerators.
	if machineTypeAcceleratorCount != 0 {
		guestAccelerators = append(guestAccelerators, machinev1.GCPGPUConfig{Type: "nvidia-tesla-a100", Count: int32(machineTypeAcceleratorCount)})
	} else {
		guestAccelerators = r.providerSpec.GPUs
	}
	// validate zone and then quota
	// guestAccelerators slice can not store more than 1 element.
	// More than one accelerator included in request results in error -> googleapi: Error 413: Value for field 'resource.guestAccelerators' is too large: maximum size 1 element(s); actual size 2., fieldSizeTooLarge
	accelerator := guestAccelerators[0]
	_, err = r.computeService.AcceleratorTypeGet(r.projectID, r.providerSpec.Zone, accelerator.Type)
	if err != nil {
		return machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("AcceleratorType %s not available in the zone %s : %v", accelerator.Type, r.providerSpec.Zone, err))
	}
	metric := supportedGpuTypes[accelerator.Type]
	if metric == "" {
		return machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("Unsupported accelerator type %s", accelerator.Type))
	}
	// preemptible instances have separate quota
	if r.providerSpec.Preemptible {
		metric = "PREEMPTIBLE_" + metric
	}
	// check quota for GA
	for i, q := range quotas {
		if q.Metric == metric {
			if int32(q.Usage)+accelerator.Count > int32(q.Limit) {
				return machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("Quota exceeded. Metric: %s. Usage: %v. Limit: %v.", metric, q.Usage, q.Limit))
			}
			break
		}
		if i == len(quotas)-1 {
			return machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("No quota found. Metric: %s.", metric))
		}
	}
	return nil
}

func (r *Reconciler) validateGuestAccelerators() error {
	if len(r.providerSpec.GPUs) == 0 && !strings.HasPrefix(r.providerSpec.MachineType, "a2-") {
		// no accelerators to validate so return nil
		return nil
	}
	if len(r.providerSpec.GPUs) > 0 && strings.HasPrefix(r.providerSpec.MachineType, "a2-") {
		return machinecontroller.InvalidMachineConfiguration("A2 Machine types have pre-attached guest accelerators. Adding additional guest accelerators is not supported")
	}
	if !strings.HasPrefix(r.providerSpec.MachineType, "n1-") && !strings.HasPrefix(r.providerSpec.MachineType, "a2-") {
		return machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("MachineType %s does not support accelerators. Only A2 and N1 machine type families support guest acceleartors.", r.providerSpec.MachineType))
	}
	a2MachineFamily, n1MachineFamily := r.computeService.GPUCompatibleMachineTypesList(r.providerSpec.ProjectID, r.providerSpec.Zone, r.Context)
	machineType := r.providerSpec.MachineType
	switch {
	case a2MachineFamily[machineType] != 0:
		// a2 family machine - has fixed type and count of GPUs
		return r.checkQuota(a2MachineFamily[machineType])
	case containsString(n1MachineFamily, machineType):
		// n1 family machine
		return r.checkQuota(0)
	default:
		// any other machine type
		return machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("MachineType %s is not available in the zone %s.", r.providerSpec.MachineType, r.providerSpec.Zone))
	}
}

// Create creates machine if and only if machine exists, handled by cluster-api
func (r *Reconciler) create() error {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return machinecontroller.InvalidMachineConfiguration("failed validating machine provider spec: %v", err)
	}

	zone := r.providerSpec.Zone
	instance := &compute.Instance{
		CanIpForward:       r.providerSpec.CanIPForward,
		DeletionProtection: r.providerSpec.DeletionProtection,
		Labels:             r.providerSpec.Labels,
		MachineType:        fmt.Sprintf(machineTypeFmt, zone, r.providerSpec.MachineType),
		Name:               r.machine.Name,
		Tags: &compute.Tags{
			Items: r.providerSpec.Tags,
		},
		Scheduling: &compute.Scheduling{
			Preemptible:       r.providerSpec.Preemptible,
			AutomaticRestart:  restartPolicyToBool(r.providerSpec.RestartPolicy),
			OnHostMaintenance: string(r.providerSpec.OnHostMaintenance),
		},
	}

	var guestAccelerators = []*compute.AcceleratorConfig{}

	if l := len(r.providerSpec.GPUs); l == 1 {
		guestAccelerators = append(guestAccelerators, &compute.AcceleratorConfig{
			AcceleratorType:  fmt.Sprintf(acceleratorTypeFmt, zone, r.providerSpec.GPUs[0].Type),
			AcceleratorCount: int64(r.providerSpec.GPUs[0].Count),
		})
	} else if l > 1 {
		return machinecontroller.InvalidMachineConfiguration("More than one type of accelerator provided. Instances support only one accelerator type at a time.")
	}

	instance.GuestAccelerators = guestAccelerators

	if err := r.validateGuestAccelerators(); err != nil {
		return err
	}

	if instance.Labels == nil {
		instance.Labels = map[string]string{}
	}
	instance.Labels[fmt.Sprintf("kubernetes-io-cluster-%v", r.machine.Labels[machinev1.MachineClusterIDLabel])] = "owned"

	// disks
	var disks = []*compute.AttachedDisk{}
	for _, disk := range r.providerSpec.Disks {
		srcImage := disk.Image
		if !strings.Contains(disk.Image, "/") {
			// only image name provided therefore defaulting to the current project
			srcImage = googleapi.ResolveRelative(r.computeService.BasePath(), fmt.Sprintf("projects/%s/global/images/%s", r.projectID, disk.Image))
		}

		disks = append(disks, &compute.AttachedDisk{
			AutoDelete: disk.AutoDelete,
			Boot:       disk.Boot,
			InitializeParams: &compute.AttachedDiskInitializeParams{
				DiskSizeGb:  disk.SizeGB,
				DiskType:    fmt.Sprintf("zones/%s/diskTypes/%s", zone, disk.Type),
				Labels:      disk.Labels,
				SourceImage: srcImage,
			},
			DiskEncryptionKey: generateDiskEncryptionKey(disk.EncryptionKey, r.projectID),
		})
	}
	instance.Disks = disks

	// networking
	var networkInterfaces = []*compute.NetworkInterface{}

	for _, nic := range r.providerSpec.NetworkInterfaces {
		accessConfigs := []*compute.AccessConfig{}
		if nic.PublicIP {
			accessConfigs = append(accessConfigs, &compute.AccessConfig{})
		}
		computeNIC := &compute.NetworkInterface{
			AccessConfigs: accessConfigs,
		}
		projectID := nic.ProjectID
		if projectID == "" {
			projectID = r.projectID
		}
		if len(nic.Network) != 0 {
			computeNIC.Network = fmt.Sprintf("projects/%s/global/networks/%s", projectID, nic.Network)
		}
		if len(nic.Subnetwork) != 0 {
			computeNIC.Subnetwork = fmt.Sprintf("projects/%s/regions/%s/subnetworks/%s", projectID, r.providerSpec.Region, nic.Subnetwork)
		}
		networkInterfaces = append(networkInterfaces, computeNIC)
	}
	instance.NetworkInterfaces = networkInterfaces

	// serviceAccounts
	var serviceAccounts = []*compute.ServiceAccount{}
	for _, sa := range r.providerSpec.ServiceAccounts {
		serviceAccounts = append(serviceAccounts, &compute.ServiceAccount{
			Email:  sa.Email,
			Scopes: sa.Scopes,
		})
	}
	instance.ServiceAccounts = serviceAccounts

	// userData
	userData, err := r.getCustomUserData()
	if err != nil {
		return fmt.Errorf("error getting custom user data: %v", err)
	}
	// check to see if this is a windows machine, if so then the user data secret
	// should be set in the metadata using a key to designate that it is a windows
	// boot script.
	userdataKey := "user-data"
	if windows.IsMachineOSWindows(*r.machine) {
		userdataKey = windowsScriptMetadataKey
		// ensure that the powershell script is not enclosed by <powershell> tags
		userData = windows.RemovePowershellTags(userData)
	}
	var metadataItems = []*compute.MetadataItems{
		{
			Key:   userdataKey,
			Value: &userData,
		},
	}
	for _, metadata := range r.providerSpec.Metadata {
		// GCP will not allow duplicate values in the metadata, if the user has specified
		// the key for the user data, or the windows script, we should replace the value
		if metadata.Key == userdataKey {
			metadataItems[0].Value = metadata.Value
		} else {
			metadataItems = append(metadataItems, &compute.MetadataItems{
				Key:   metadata.Key,
				Value: metadata.Value,
			})
		}
	}
	instance.Metadata = &compute.Metadata{
		Items: metadataItems,
	}

	_, err = r.computeService.InstancesInsert(r.projectID, zone, instance)
	if err != nil {
		metrics.RegisterFailedInstanceCreate(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    err.Error(),
		})
		if reconcileWithCloudError := r.reconcileMachineWithCloudState(&metav1.Condition{
			Type:    string(machinev1.MachineCreated),
			Reason:  machineCreationFailedReason,
			Message: err.Error(),
			Status:  metav1.ConditionFalse,
		}); reconcileWithCloudError != nil {
			klog.Errorf("Failed to reconcile machine with cloud state: %v", reconcileWithCloudError)
		}
		if googleError, ok := err.(*googleapi.Error); ok {
			// we return InvalidMachineConfiguration for 4xx errors which by convention signal client misconfiguration
			// https://tools.ietf.org/html/rfc2616#section-6.1.1
			if strings.HasPrefix(strconv.Itoa(googleError.Code), "4") {
				klog.Infof("Error launching instance: %v", googleError)
				return machinecontroller.InvalidMachineConfiguration("error launching instance: %v", googleError.Error())
			}
		}
		return fmt.Errorf("failed to create instance via compute service: %v", err)
	}
	return r.reconcileMachineWithCloudState(nil)
}

func (r *Reconciler) update() error {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return machinecontroller.InvalidMachineConfiguration("failed validating machine provider spec: %v", err)
	}

	// Add target pools, if necessary
	if err := r.processTargetPools(true, r.addInstanceToTargetPool); err != nil {
		return err
	}

	// Add control plane machines to instance group, if necessary
	if r.machineScope.machine.ObjectMeta.Labels[openshiftMachineRoleLabel] == masterMachineRole {
		if err := r.registerInstanceToControlPlaneInstanceGroup(); err != nil {
			return fmt.Errorf("failed to register instance to instance group: %v", err)
		}
	}
	return r.reconcileMachineWithCloudState(nil)
}

// reconcileMachineWithCloudState reconcile machineSpec and status with the latest cloud state
// if a failedCondition is passed it updates the providerStatus.Conditions and return
// otherwise it fetches the relevant cloud instance and reconcile the rest of the fields
func (r *Reconciler) reconcileMachineWithCloudState(failedCondition *metav1.Condition) error {
	klog.Infof("%s: Reconciling machine object with cloud state", r.machine.Name)
	if failedCondition != nil {
		r.providerStatus.Conditions = reconcileConditions(r.providerStatus.Conditions, *failedCondition)
		return nil
	} else {
		freshInstance, err := r.computeService.InstancesGet(r.projectID, r.providerSpec.Zone, r.machine.Name)
		if err != nil {
			return fmt.Errorf("failed to get instance via compute service: %v", err)
		}

		if len(freshInstance.NetworkInterfaces) < 1 {
			return fmt.Errorf("could not find network interfaces for instance %q", freshInstance.Name)
		}
		networkInterface := freshInstance.NetworkInterfaces[0]

		nodeAddresses := []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: networkInterface.NetworkIP}}
		for _, config := range networkInterface.AccessConfigs {
			nodeAddresses = append(nodeAddresses, corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: config.NatIP})
		}
		// Since we don't know when the project was created, we must account for
		// both types of internal-dns:
		// https://cloud.google.com/compute/docs/internal-dns#instance-fully-qualified-domain-names
		// [INSTANCE_NAME].[ZONE].c.[PROJECT_ID].internal (newer)
		nodeAddresses = append(nodeAddresses, corev1.NodeAddress{
			Type:    corev1.NodeInternalDNS,
			Address: fmt.Sprintf("%s.%s.c.%s.internal", r.machine.Name, r.providerSpec.Zone, r.projectID),
		})
		// [INSTANCE_NAME].c.[PROJECT_ID].internal
		nodeAddresses = append(nodeAddresses, corev1.NodeAddress{
			Type:    corev1.NodeInternalDNS,
			Address: fmt.Sprintf("%s.c.%s.internal", r.machine.Name, r.projectID),
		})
		// Add the machine's name as a known NodeInternalDNS because GCP platform
		// provides search paths to resolve those.
		// https://cloud.google.com/compute/docs/internal-dns#resolv.conf
		nodeAddresses = append(nodeAddresses, corev1.NodeAddress{
			Type:    corev1.NodeInternalDNS,
			Address: r.machine.GetName(),
		})

		r.machine.Spec.ProviderID = &r.providerID
		r.machine.Status.Addresses = nodeAddresses
		r.providerStatus.InstanceState = &freshInstance.Status
		r.providerStatus.InstanceID = &freshInstance.Name
		succeedCondition := metav1.Condition{
			Type:    string(machinev1.MachineCreated),
			Reason:  machineCreationSucceedReason,
			Message: machineCreationSucceedMessage,
			Status:  metav1.ConditionTrue,
		}
		r.providerStatus.Conditions = reconcileConditions(r.providerStatus.Conditions, succeedCondition)

		r.setMachineCloudProviderSpecifics(freshInstance)

		if freshInstance.Status != "RUNNING" {
			klog.Infof("%s: machine status is %q, requeuing...", r.machine.Name, freshInstance.Status)
			return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
		}
	}

	return nil
}

func (r *Reconciler) setMachineCloudProviderSpecifics(instance *compute.Instance) {
	if r.machine.Labels == nil {
		r.machine.Labels = make(map[string]string)
	}

	if r.machine.Annotations == nil {
		r.machine.Annotations = make(map[string]string)
	}

	r.machine.Annotations[machinecontroller.MachineInstanceStateAnnotationName] = instance.Status
	// TODO(jchaloup): detect all three from instance rather than
	// always assuming it's the same as what is specified in the provider spec
	r.machine.Labels[machinecontroller.MachineInstanceTypeLabelName] = r.providerSpec.MachineType
	r.machine.Labels[machinecontroller.MachineRegionLabelName] = r.providerSpec.Region
	r.machine.Labels[machinecontroller.MachineAZLabelName] = r.providerSpec.Zone

	if r.providerSpec.Preemptible {
		// Label on the Machine so that an MHC can select Preemptible instances
		r.machine.Labels[machinecontroller.MachineInterruptibleInstanceLabelName] = ""

		if r.machine.Spec.Labels == nil {
			r.machine.Spec.Labels = make(map[string]string)
		}
		r.machine.Spec.Labels[machinecontroller.MachineInterruptibleInstanceLabelName] = ""
	}
}

func (r *Reconciler) getCustomUserData() (string, error) {
	if r.providerSpec.UserDataSecret == nil {
		return "", nil
	}
	var userDataSecret corev1.Secret

	if err := r.coreClient.Get(context.Background(), client.ObjectKey{Namespace: r.machine.GetNamespace(), Name: r.providerSpec.UserDataSecret.Name}, &userDataSecret); err != nil {
		if apimachineryerrors.IsNotFound(err) {
			return "", machinecontroller.InvalidMachineConfiguration("user data secret %q in namespace %q not found: %v", r.providerSpec.UserDataSecret.Name, r.machine.GetNamespace(), err)
		}
		return "", fmt.Errorf("error getting user data secret %q in namespace %q: %v", r.providerSpec.UserDataSecret.Name, r.machine.GetNamespace(), err)
	}
	data, exists := userDataSecret.Data[userDataSecretKey]
	if !exists {
		return "", machinecontroller.InvalidMachineConfiguration("secret %v/%v does not have %q field set. Thus, no user data applied when creating an instance", r.machine.GetNamespace(), r.providerSpec.UserDataSecret.Name, userDataSecretKey)
	}
	return string(data), nil
}

func validateMachine(machine machinev1.Machine, providerSpec machinev1.GCPMachineProviderSpec) error {
	// TODO (alberto): First validation should happen via webhook before the object is persisted.
	// This is a complementary validation to fail early in case of lacking proper webhook validation.
	// Default values can also be set here
	if providerSpec.TargetPools != nil {
		for _, pool := range providerSpec.TargetPools {
			if pool == "" {
				return machinecontroller.InvalidMachineConfiguration("all target pools must have valid name")
			}
		}
	}

	if machine.Labels[machinev1.MachineClusterIDLabel] == "" {
		return machinecontroller.InvalidMachineConfiguration("machine is missing %q label", machinev1.MachineClusterIDLabel)
	}

	return nil
}

// Returns true if machine exists.
func (r *Reconciler) exists() (bool, error) {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return false, fmt.Errorf("failed validating machine provider spec: %v", err)
	}
	zone := r.providerSpec.Zone
	// Need to verify that our project/zone exists before checking machine, as
	// invalid project/zone produces same 404 error as no machine.
	if err := r.validateZone(); err != nil {
		if isNotFoundError(err) {
			// this error type bubbles back up to the machine-controller to allow
			// us to delete machines that were never properly created due to
			// invalid zone.
			return false, machinecontroller.InvalidMachineConfiguration(fmt.Sprintf("%s: Machine does not exist", r.machine.Name))
		}
		return false, fmt.Errorf("unable to verify project/zone exists: %v/%v; err: %v", r.projectID, zone, err)
	}

	instance, err := r.computeService.InstancesGet(r.projectID, zone, r.machine.Name)
	if instance != nil && err == nil {
		return true, nil
	}
	if isNotFoundError(err) {
		klog.Infof("%s: Machine does not exist", r.machine.Name)
		return false, nil
	}
	return false, fmt.Errorf("error getting running instances: %v", err)
}

// Returns true if machine exists.
func (r *Reconciler) delete() error {
	// Remove instance from target pools, if necessary
	if err := r.processTargetPools(false, r.deleteInstanceFromTargetPool); err != nil {
		return err
	}

	// Remove instance from instance group, if necessary
	if r.machineScope.machine.Labels[openshiftMachineRoleLabel] == masterMachineRole {
		if err := r.unregisterInstanceFromControlPlaneInstanceGroup(); err != nil {
			return fmt.Errorf("%s: failed to unregister instance from instance group: %v", r.machine.Name, err)
		}
	}

	exists, err := r.exists()
	if err != nil {
		return err
	}
	if !exists {
		klog.Infof("%s: Machine not found during delete, skipping", r.machine.Name)
		return nil
	}
	if _, err = r.computeService.InstancesDelete(string(r.machine.UID), r.projectID, r.providerSpec.Zone, r.machine.Name); err != nil {
		metrics.RegisterFailedInstanceDelete(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    err.Error(),
		})
		return fmt.Errorf("failed to delete instance via compute service: %v", err)
	}
	klog.Infof("%s: machine status is exists, requeuing...", r.machine.Name)
	return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
}

func (r *Reconciler) validateZone() error {
	_, err := r.computeService.ZonesGet(r.projectID, r.providerSpec.Zone)
	return err
}

func isNotFoundError(err error) bool {
	switch t := err.(type) {
	case *googleapi.Error:
		return t.Code == 404
	}
	return false
}

func fmtInstanceSelfLink(project, zone, name string) string {
	return fmt.Sprintf(instanceLinkFmt, project, zone, name)
}

func (r *Reconciler) instanceExistsInPool(instanceLink string, pool string) (bool, error) {
	// Get target pool
	tp, err := r.computeService.TargetPoolsGet(r.projectID, r.providerSpec.Region, pool)
	if err != nil {
		return false, fmt.Errorf("unable to get targetpool: %v", err)
	}

	for _, link := range tp.Instances {
		if instanceLink == link {
			return true, nil
		}
	}
	return false, nil
}

type poolProcessor func(instanceLink, pool string) error

func (r *Reconciler) processTargetPools(desired bool, poolFunc poolProcessor) error {
	instanceSelfLink := fmtInstanceSelfLink(r.projectID, r.providerSpec.Zone, r.machine.Name)
	// TargetPools may be empty/nil, and that's okay.
	for _, pool := range r.providerSpec.TargetPools {
		present, err := r.instanceExistsInPool(instanceSelfLink, pool)
		if err != nil {
			return err
		}
		if present != desired {
			klog.Infof("%v: reconciling instance for targetpool with cloud provider; desired state: %v", r.machine.Name, desired)
			err := poolFunc(instanceSelfLink, pool)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// registerInstanceToControlPlaneInstanceGroup ensures that the instance is assigned to the control plane instance group of its zone.
func (r *Reconciler) registerInstanceToControlPlaneInstanceGroup() error {
	instanceSelfLink := fmtInstanceSelfLink(r.projectID, r.providerSpec.Zone, r.machine.Name)
	instanceGroupName := r.controlPlaneGroupName()

	instanceSets, err := r.fetchRunningInstancesInInstanceGroup(r.projectID, r.providerSpec.Zone, instanceGroupName)
	if err != nil {
		return fmt.Errorf("failed to fetch running instances in instance group %s: %v", instanceGroupName, err)
	}

	if !instanceSets.Has(instanceSelfLink) && pointer.StringDeref(r.providerStatus.InstanceState, "") == "RUNNING" {
		klog.V(4).Info("Registering instance in the instancegroup", "name", r.machine.Name, "instancegroup", instanceGroupName)
		_, err := r.computeService.InstanceGroupsAddInstances(
			r.projectID,
			r.providerSpec.Zone,
			instanceSelfLink,
			instanceGroupName)
		if err != nil {
			return fmt.Errorf("InstanceGroupsAddInstances request failed: %v", err)
		}
	}

	return nil
}

// unregisterInstanceFromControlPlaneInstanceGroup ensures that the instance is removed from the control plane instance group.
func (r *Reconciler) unregisterInstanceFromControlPlaneInstanceGroup() error {
	instanceSelfLink := fmtInstanceSelfLink(r.projectID, r.providerSpec.Zone, r.machine.Name)
	instanceGroupName := r.controlPlaneGroupName()

	instanceSets, err := r.fetchRunningInstancesInInstanceGroup(r.projectID, r.providerSpec.Zone, instanceGroupName)
	if err != nil {
		return fmt.Errorf("failed to fetch running instances in instance group %s: %v", instanceGroupName, err)
	}

	if len(instanceSets) > 0 && instanceSets.Has(instanceSelfLink) {
		klog.V(4).Info("Unregistering instance from the instancegroup", "name", r.machine.Name, "instancegroup", instanceGroupName)
		_, err := r.computeService.InstanceGroupsRemoveInstances(
			r.projectID,
			r.providerSpec.Zone,
			instanceSelfLink,
			instanceGroupName)
		if err != nil {
			return fmt.Errorf("InstanceGroupsRemoveInstances request failed: %v", err)
		}
	}

	return nil
}

// fetchRunningInstancesInInstanceGroup fetches all running instances and returns a set of instance links.
func (r *Reconciler) fetchRunningInstancesInInstanceGroup(projectID string, zone string, instaceGroup string) (sets.String, error) {
	instanceList, err := r.computeService.InstanceGroupsListInstances(projectID, zone, instaceGroup,
		&compute.InstanceGroupsListInstancesRequest{
			InstanceState: "RUNNING",
		},
	)
	if err != nil {
		return nil, fmt.Errorf("instanceGroupsListInstances request failed: %v", err)
	}

	instanceSets := sets.NewString()
	for _, i := range instanceList.Items {
		instanceSets.Insert(i.Instance)
	}

	return instanceSets, nil
}

// ControlPlaneGroupName generates the name of the instance group that this instace should belong to.
func (r *Reconciler) controlPlaneGroupName() string {
	return fmt.Sprintf("%s-%s-%s", r.machine.Labels[machinev1.MachineClusterIDLabel], masterMachineRole, r.providerSpec.Zone)
}

func (r *Reconciler) addInstanceToTargetPool(instanceLink string, pool string) error {
	_, err := r.computeService.TargetPoolsAddInstance(r.projectID, r.providerSpec.Region, pool, instanceLink)
	// Probably safe to disregard the returned operation; it either worked or it didn't.
	// Even if the instance doesn't exist, it will return without error and the non-existent
	// instance will be associated.
	if err != nil {
		metrics.RegisterFailedInstanceUpdate(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    err.Error(),
		})
		return fmt.Errorf("failed to add instance %v to target pool %v: %v", r.machine.Name, pool, err)
	}
	return nil
}

func (r *Reconciler) deleteInstanceFromTargetPool(instanceLink string, pool string) error {
	_, err := r.computeService.TargetPoolsRemoveInstance(r.projectID, r.providerSpec.Region, pool, instanceLink)
	if err != nil {
		metrics.RegisterFailedInstanceDelete(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    err.Error(),
		})
		return fmt.Errorf("failed to remove instance %v from target pool %v: %v", r.machine.Name, pool, err)
	}
	return nil
}

func generateDiskEncryptionKey(keyRef *machinev1.GCPEncryptionKeyReference, projectID string) *compute.CustomerEncryptionKey {
	if keyRef == nil || keyRef.KMSKey == nil {
		return nil
	}

	if keyRef.KMSKey.ProjectID != "" {
		projectID = keyRef.KMSKey.ProjectID
	}

	return &compute.CustomerEncryptionKey{
		KmsKeyName:           fmt.Sprintf(kmsKeyNameFmt, projectID, keyRef.KMSKey.Location, keyRef.KMSKey.KeyRing, keyRef.KMSKey.Name),
		KmsKeyServiceAccount: keyRef.KMSKeyServiceAccount,
	}
}
