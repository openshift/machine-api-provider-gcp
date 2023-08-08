package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
	"github.com/openshift/library-go/pkg/operator/events"
	capimachine "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/machine"
	machinesetcontroller "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/machineset"
	computeservice "github.com/openshift/machine-api-provider-gcp/pkg/cloud/gcp/actuators/services/compute"
	"github.com/openshift/machine-api-provider-gcp/pkg/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// The default durations for the leader electrion operations.
var (
	leaseDuration = 120 * time.Second
	renewDealine  = 110 * time.Second
	retryPeriod   = 20 * time.Second
)

func main() {
	printVersion := flag.Bool(
		"version",
		false,
		"print version and exit",
	)

	leaderElectResourceNamespace := flag.String(
		"leader-elect-resource-namespace",
		"",
		"The namespace of resource object that is used for locking during leader election. If unspecified and running in cluster, defaults to the service account namespace for the controller. Required for leader-election outside of a cluster.",
	)

	leaderElect := flag.Bool(
		"leader-elect",
		false,
		"Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated components for high availability.",
	)

	leaderElectLeaseDuration := flag.Duration(
		"leader-elect-lease-duration",
		leaseDuration,
		"The duration that non-leader candidates will wait after observing a leadership renewal until attempting to acquire leadership of a led but unrenewed leader slot. This is effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. This is only applicable if leader election is enabled.",
	)

	watchNamespace := flag.String(
		"namespace",
		"",
		"Namespace that the controller watches to reconcile machine-api objects. If unspecified, the controller watches for machine-api objects across all namespaces.",
	)

	healthAddr := flag.String(
		"health-addr",
		":9440",
		"The address for health checking.",
	)

	metricsAddress := flag.String(
		"metrics-bind-address",
		metrics.DefaultMachineMetricsAddress,
		"Address for hosting metrics",
	)

	klog.InitFlags(nil)
	flag.Set("logtostderr", "true")
	flag.Parse()

	if *printVersion {
		fmt.Println(version.String)
		os.Exit(0)
	}

	cfg := config.GetConfigOrDie()

	// Override the default 10 hour sync period so that we pick up external changes
	// to the VMs within a reasonable time frame.
	syncPeriod := 10 * time.Minute

	opts := manager.Options{
		LeaderElection:          *leaderElect,
		LeaderElectionNamespace: *leaderElectResourceNamespace,
		LeaderElectionID:        "cluster-api-provider-gcp-leader",
		LeaseDuration:           leaderElectLeaseDuration,
		HealthProbeBindAddress:  *healthAddr,
		SyncPeriod:              &syncPeriod,
		MetricsBindAddress:      *metricsAddress,
		// Slow the default retry and renew election rate to reduce etcd writes at idle: BZ 1858400
		RetryPeriod:   &retryPeriod,
		RenewDeadline: &renewDealine,
	}

	if *watchNamespace != "" {
		opts.Namespace = *watchNamespace
		klog.Infof("Watching machine-api objects only in namespace %q for reconciliation.", opts.Namespace)
	}

	// Setup a Manager
	mgr, err := manager.New(cfg, opts)
	if err != nil {
		klog.Fatalf("Failed to set up overall controller manager: %v", err)
	}

	stopSignalContext := ctrl.SetupSignalHandler()

	featureGateAccessor, err := createFeatureGateAccessor(
		context.Background(),
		cfg,
		"machine-api-provider-gcp",
		"openshift-machine-api",
		"machine-api-controllers",
		getReleaseVersion(),
		"0.0.1-snapshot",
		syncPeriod,
		stopSignalContext.Done(),
	)
	if err != nil {
		klog.Fatalf("failed to create feature gate accessor: %w", err)
	}

	featureGates, err := awaitEnabledFeatureGates(featureGateAccessor, 1*time.Minute)
	if err != nil {
		klog.Fatalf("failed to get feature gates: %w", err)
	}

	// Initialize machine actuator.
	machineActuator := machine.NewActuator(machine.ActuatorParams{
		CoreClient:           mgr.GetClient(),
		EventRecorder:        mgr.GetEventRecorderFor("gcpcontroller"),
		ComputeClientBuilder: computeservice.NewComputeService,
		FeatureGates:         featureGates,
	})

	if err := machinev1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	if err := configv1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	if err := capimachine.AddWithActuator(mgr, machineActuator); err != nil {
		klog.Fatal(err)
	}

	ctrl.SetLogger(klogr.New())
	setupLog := ctrl.Log.WithName("setup")
	if err = (&machinesetcontroller.Reconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("MachineSet"),
	}).SetupWithManager(mgr, controller.Options{}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MachineSet")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		klog.Fatal(err)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		klog.Fatal(err)
	}

	if err := mgr.Start(stopSignalContext); err != nil {
		klog.Fatalf("Failed to run manager: %v", err)
	}
}

func createFeatureGateAccessor(ctx context.Context, cfg *rest.Config, operatorName, deploymentNamespace, deploymentName, desiredVersion, missingVersion string, syncPeriod time.Duration, stop <-chan struct{}) (featuregates.FeatureGateAccess, error) {
	ctx, cancelFn := context.WithCancel(ctx)
	go func() {
		defer cancelFn()
		<-stop
	}()

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client: %w", err)
	}

	eventRecorder := events.NewKubeRecorder(kubeClient.CoreV1().Events(deploymentNamespace), operatorName, &corev1.ObjectReference{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Namespace:  deploymentNamespace,
		Name:       deploymentName,
	})

	configClient, err := configclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create config client: %w", err)
	}
	configInformers := configinformers.NewSharedInformerFactory(configClient, syncPeriod)

	featureGateAccessor := featuregates.NewFeatureGateAccess(
		desiredVersion, missingVersion,
		configInformers.Config().V1().ClusterVersions(), configInformers.Config().V1().FeatureGates(),
		eventRecorder,
	)
	go featureGateAccessor.Run(ctx)
	go configInformers.Start(stop)

	return featureGateAccessor, nil
}

func awaitEnabledFeatureGates(accessor featuregates.FeatureGateAccess, timeout time.Duration) (featuregates.FeatureGate, error) {
	select {
	case <-accessor.InitialFeatureGatesObserved():
		featureGates, err := accessor.CurrentFeatureGates()
		if err != nil {
			return nil, err
		} else {
			klog.Infof("featureGates initialized: knownFeatureGates=%v", featureGates.KnownFeatures())
			return featureGates, nil
		}
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for FeatureGate detection")
	}
}

func getReleaseVersion() string {
	releaseVersion := os.Getenv("RELEASE_VERSION")
	if len(releaseVersion) == 0 {
		return "0.0.1-snapshot"
	}
	return releaseVersion
}
