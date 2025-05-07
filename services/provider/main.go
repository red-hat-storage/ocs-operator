package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/provider/server"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	port = flag.Int("port", 50051, "The server port")
)

func main() {
	flag.Parse()

	klog.Info("Starting Provider API server")

	namespace := util.GetPodNamespace()
	loggerOpts := zap.Options{}
	logger := zap.New(zap.UseFlagOptions(&loggerOpts))
	ctrl.SetLogger(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	providerServer, err := server.NewOCSProviderServer(ctx, namespace, logger)
	if err != nil {
		klog.Errorf("failed to start provider server. %v", err)
		return
	}

	providerServer.Start(*port, []grpc.ServerOption{})
}
