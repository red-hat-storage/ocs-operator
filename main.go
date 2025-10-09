/*
Copyright 2020 Red Hat OpenShift Container Storage.

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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	nadscheme "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/scheme"
	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	nbapis "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	openshiftConfigv1 "github.com/openshift/api/config/v1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	openshiftv1 "github.com/openshift/api/template/v1"
	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	apiv2 "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/operator-framework/operator-lib/conditions"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	odfgsapiv1b1 "github.com/red-hat-storage/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/util/retry"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	apiclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/mirroring"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/ocsinitialization"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/storageautoscaler"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/storagecluster"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/storageclusterpeer"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"sigs.k8s.io/controller-runtime/pkg/event"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = apiruntime.NewScheme()
	setupLog = ctrl.Log.WithName("cmd")
)

const (
	defaultOnboardingTokenLifetimeInHours = 48
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiv2.AddToScheme(scheme))
	utilruntime.Must(ocsv1.AddToScheme(scheme))
	utilruntime.Must(cephv1.AddToScheme(scheme))
	utilruntime.Must(storagev1.AddToScheme(scheme))
	utilruntime.Must(nbapis.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(openshiftv1.AddToScheme(scheme))
	utilruntime.Must(snapapi.AddToScheme(scheme))
	utilruntime.Must(groupsnapapi.AddToScheme(scheme))
	utilruntime.Must(odfgsapiv1b1.AddToScheme(scheme))
	utilruntime.Must(openshiftConfigv1.AddToScheme(scheme))
	utilruntime.Must(extv1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(quotav1.AddToScheme(scheme))
	utilruntime.Must(ocsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterv1alpha1.AddToScheme(scheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(nadscheme.AddToScheme(scheme))
	utilruntime.Must(ocsclientv1a1.AddToScheme(scheme))
	utilruntime.Must(csiaddonsv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

func main() {
	var probeAddr string
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	loggerOpts := zap.Options{}
	loggerOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&loggerOpts))
	ctrl.SetLogger(logger)

	printVersion()
	if loggerOpts.Development {
		setupLog.Info("running in development mode")
	}

	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		setupLog.Error(err, "unable to get operator namespace")
		os.Exit(1)
	}

	defaultNamespaces := map[string]cache.Config{
		operatorNamespace:            {},
		"openshift-storage-extended": {},
	}

	platform.Detect()
	isOpenShift, err := platform.IsPlatformOpenShift()
	if err != nil {
		setupLog.Error(err, "unable to detect platform")
		os.Exit(1)
	}
	if isOpenShift {
		setupLog.Info("Cluster is running on OpenShift.")
	} else {
		setupLog.Info("Cluster is not running on OpenShift.")
	}

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metrics.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "ab76f4c9.openshift.io",
		LeaderElectionNamespace: operatorNamespace,
		Cache:                   cache.Options{DefaultNamespaces: defaultNamespaces},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	condition, err := storagecluster.NewUpgradeable(mgr.GetClient())
	if err != nil {
		setupLog.Error(err, "Unable to get OperatorCondition")
		os.Exit(1)
	}
	// apiclient.New() returns a client without cache.
	// cache is not initialized before mgr.Start()
	// we need this because we need to interact with OperatorCondition
	apiClient, err := apiclient.New(mgr.GetConfig(), apiclient.Options{
		Scheme: mgr.GetScheme(),
	})
	if err != nil {
		setupLog.Error(err, "Unable to get Client")
		os.Exit(1)
	}

	availCrds, err := getAvailableCRDNames(context.Background(), apiClient)
	if err != nil {
		setupLog.Error(err, "Unable get a list of available CRD names")
		os.Exit(1)
	}

	if err = (&ocsinitialization.OCSInitializationReconciler{
		Client:            mgr.GetClient(),
		Log:               ctrl.Log.WithName("controllers").WithName("OCSInitialization"),
		Scheme:            mgr.GetScheme(),
		SecurityClient:    secv1client.NewForConfigOrDie(mgr.GetConfig()),
		OperatorNamespace: operatorNamespace,
		AvailableCrds:     availCrds,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OCSInitialization")
		os.Exit(1)
	}

	if err = (&storagecluster.StorageClusterReconciler{
		Client:            mgr.GetClient(),
		Log:               ctrl.Log.WithName("controllers").WithName("StorageCluster"),
		Scheme:            mgr.GetScheme(),
		OperatorNamespace: operatorNamespace,
		OperatorCondition: condition,
		AvailableCrds:     availCrds,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StorageCluster")
		os.Exit(1)
	}

	onboardingTokenLifetimeInHours, err := util.ReadEnvVar("ONBOARDING_TOKEN_LIFETIME", defaultOnboardingTokenLifetimeInHours, strconv.Atoi)
	if err != nil {
		onboardingTokenLifetimeInHours = defaultOnboardingTokenLifetimeInHours
		setupLog.Info("unable to parse ONBOARDING_TOKEN_LIFETIME environment value", "error", err, "using default", onboardingTokenLifetimeInHours)
	}
	if err = (&controllers.StorageConsumerReconciler{
		Client:               mgr.GetClient(),
		Log:                  ctrl.Log.WithName("controllers").WithName("StorageConsumer"),
		Scheme:               mgr.GetScheme(),
		TokenLifetimeInHours: onboardingTokenLifetimeInHours,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StorageConsumer")
		os.Exit(1)
	}

	if err = (&storageclusterpeer.StorageClusterPeerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StorageClusterPeer")
		os.Exit(1)
	}

	if err = (&mirroring.MirroringReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Mirroring")
		os.Exit(1)
	}

	// shared event channel and sync map and scrapeInterval between the scraper and the reconciler
	eventCh := make(chan event.GenericEvent)
	syncMap := &sync.Map{}
	scrapeInterval := 10 * time.Minute

	if err = mgr.Add(
		manager.RunnableFunc(func(ctx context.Context) error {
			// run a go routine to scrape the metrics from prometheus periodically
			scraper := (&storageautoscaler.StorageAutoscalerScraper{
				Client:            mgr.GetClient(),
				Log:               ctrl.Log.WithName("scraper").WithName("StorageAutoscalerScraper"),
				OperatorNamespace: operatorNamespace,
				ScrapeInterval:    scrapeInterval,
				SyncMap:           syncMap,
				EventCh:           eventCh,
			})
			scraper.ScrapeMetricsPeriodically(ctx)

			return nil
		}),
	); err != nil {
		setupLog.Error(err, "unable add StorageAutoscaler scraper runnable to the manager")
		os.Exit(1)
	}

	if err = (&storageautoscaler.StorageAutoscalerReconciler{
		Client:            mgr.GetClient(),
		OperatorNamespace: operatorNamespace,
		Log:               ctrl.Log.WithName("controllers").WithName("StorageAutoScaler"),
		SyncMap:           syncMap,
		EventCh:           eventCh,
		ScrapeInterval:    scrapeInterval,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StorageAutoscaler")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// Create OCSInitialization CR if it's not present
	ocsNamespacedName := ocsinitialization.InitNamespacedName()
	client := mgr.GetClient()
	err = client.Create(context.TODO(), &ocsv1.OCSInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocsNamespacedName.Name,
			Namespace: ocsNamespacedName.Namespace,
		},
	})
	switch {
	case err == nil:
		setupLog.Info("created OCSInitialization resource")
	case errors.IsAlreadyExists(err):
		setupLog.Info("OCSInitialization resource already exists")
	default:
		setupLog.Error(err, "failed to create OCSInitialization custom resource")
		os.Exit(1)
	}

	// Add readiness probe
	if err := mgr.AddReadyzCheck("readyz", storagecluster.ReadinessChecker); err != nil {
		setupLog.Error(err, "unable add a readiness check")
		os.Exit(1)
	}

	// Set OperatorCondition Upgradeable to True
	// We have to at least default the condition to True or
	// OLM will use the Readiness condition via our readiness probe instead:
	// https://olm.operatorframework.io/docs/advanced-tasks/communicating-operator-conditions-to-olm/#setting-defaults
	condition, err = storagecluster.NewUpgradeable(apiClient)
	if err != nil {
		setupLog.Error(err, "Unable to get OperatorCondition")
		os.Exit(1)
	}

	// retry for sometime till OperatorCondition CR is available
	err = wait.ExponentialBackoff(retry.DefaultRetry, func() (bool, error) {
		err = condition.Set(context.TODO(), metav1.ConditionTrue, conditions.WithMessage("Operator is ready"), conditions.WithReason("Ready"))
		return err == nil, err
	})

	if err != nil {
		setupLog.Error(err, "Unable to update OperatorCondition")
		os.Exit(1)
	}

	storagecluster.ReadinessSet()
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getAvailableCRDNames(ctx context.Context, cl apiclient.Client) (map[string]bool, error) {
	crdExist := map[string]bool{}
	crdList := &metav1.PartialObjectMetadataList{}
	crdList.SetGroupVersionKind(extv1.SchemeGroupVersion.WithKind("CustomResourceDefinitionList"))
	if err := cl.List(ctx, crdList); err != nil {
		return nil, fmt.Errorf("error listing CRDs, %v", err)
	}
	// Iterate over the list and populate the map
	for i := range crdList.Items {
		crdExist[crdList.Items[i].Name] = true
	}
	return crdExist, nil
}
