package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/go-logr/zapr"
	snapapi "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	nbapis "github.com/noobaa/noobaa-operator/v2/pkg/apis"
	openshiftConfigv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	openshiftv1 "github.com/openshift/api/template/v1"
	"github.com/openshift/ocs-operator/pkg/apis"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller"
	"github.com/openshift/ocs-operator/pkg/controller/ocsinitialization"
	"github.com/openshift/ocs-operator/pkg/controller/util"
	"github.com/operator-framework/operator-lib/leader"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	kzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var log = logf.Log.WithName("cmd")

// isDevelopmentEnv is a comman line option that takes boolean value.
// It defaults to 'false' and indicates if the cluster is running in Production
// or not. This helps us configure logger accordingly.
var isDevelopmentEnv bool

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

func main() {
	flag.BoolVar(&isDevelopmentEnv, "development", false, "Enable/Disable running operator in development environment")
	flag.Parse()

	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	initLogger(isDevelopmentEnv)

	printVersion()
	log.Info(fmt.Sprintf("Running in development mode: %v", isDevelopmentEnv))

	// TODO: Remove once migrated to new project structure based on Kubebuilder
	// The way operator namespace is handled is different in newer version.
	namespace, err := util.GetWatchNamespace()
	if err != nil {
		log.Error(err, "failed to get watch namespace")
		os.Exit(1)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Become the leader before proceeding
	err = leader.Become(context.TODO(), "ocs-operator-lock")
	if err != nil {
		log.Error(err, "")
	}

	// TODO: Remove once migrated to new project structure based on Kubebuilder
	// The way Readiness is handled is different in newer version.
	r := util.NewFileReady()
	err = r.Set()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}
	defer func() {
		err := r.Unset()
		if err != nil {
			log.Error(err, "")
		}
	}()

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{Namespace: namespace})
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all api resources
	mgrScheme := mgr.GetScheme()

	if err := apis.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := cephv1.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding cephv1 to scheme")
		os.Exit(1)
	}

	if err := storagev1.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding storage/v1 to scheme")
		os.Exit(1)
	}

	if err := nbapis.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding noobaa apis to scheme")
		os.Exit(1)
	}

	if err := monitoringv1.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding monitoring/v1 apis to scheme")
		os.Exit(1)
	}

	if err := corev1.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding core/v1 to scheme")
		os.Exit(1)
	}

	if err := openshiftv1.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding openshift/v1 to scheme")
		os.Exit(1)
	}

	if err := snapapi.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding volume-snapshot apis to scheme")
		os.Exit(1)
	}

	if err := openshiftConfigv1.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding openshift/api/config/v1 to scheme")
		os.Exit(1)
	}

	if err := consolev1.AddToScheme(mgrScheme); err != nil {
		log.Error(err, "Failed adding openshift/api/console/v1 to scheme")
		os.Exit(1)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Create CR if it's not there
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
		log.Info("Created OCSInitialization resource")
	case errors.IsAlreadyExists(err):
		log.Info("OCSInitialization resource already exists")
	default:
		log.Error(err, "Failed to create OCSInitialization custom resource")
		os.Exit(1)
	}

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "manager exited non-zero")
		os.Exit(1)
	}
}

func initLogger(isDevelopmentEnv bool) {
	logger := zapr.NewLogger(getZapLogger(os.Stderr, isDevelopmentEnv))
	logf.SetLogger(logger)
}

func getZapLogger(destWriter io.Writer, development bool, opts ...zap.Option) *zap.Logger {
	sink := zapcore.AddSync(destWriter)

	var enc zapcore.Encoder
	var lvl zap.AtomicLevel
	if development {
		encCfg := zap.NewDevelopmentEncoderConfig()
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		enc = zapcore.NewConsoleEncoder(encCfg)
		// when running in development mode, all logs starting from Debug level is logged
		lvl = zap.NewAtomicLevelAt(zap.DebugLevel)
		// when running in development mode, stacktrace is logged for Warning level logs and above
		opts = append(opts, zap.Development(), zap.AddStacktrace(zap.WarnLevel))
	} else {
		encCfg := zap.NewProductionEncoderConfig()
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		enc = zapcore.NewJSONEncoder(encCfg)
		// when running in production mode, all logs starting from Info level is logged
		lvl = zap.NewAtomicLevelAt(zap.InfoLevel)
		// when running in production mode, stacktrace is logged for Error level logs and above
		opts = append(opts, zap.AddStacktrace(zap.ErrorLevel),
			zap.WrapCore(func(core zapcore.Core) zapcore.Core {
				// if more Entries with the same level and message are seen during
				// the same interval, every Mth message is logged and the rest are dropped.
				// In this case, first 3 similar messages are logged. After that every 10th
				// similar message is logged.
				return zapcore.NewSampler(core, time.Second, 3, 10)
			}))
	}
	opts = append(opts, zap.AddCallerSkip(1), zap.ErrorOutput(sink))
	log := zap.New(zapcore.NewCore(&kzap.KubeAwareEncoder{Encoder: enc, Verbose: development}, sink, lvl))
	log = log.WithOptions(opts...)
	return log
}
