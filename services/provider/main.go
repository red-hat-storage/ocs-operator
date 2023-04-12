package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"github.com/red-hat-storage/ocs-operator/controllers/util"
	"github.com/red-hat-storage/ocs-operator/services/provider/server"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

var (
	port = flag.Int("port", 50051, "The server port")
)

func main() {
	flag.Parse()

	klog.Info("Starting Provider API server")

	namespace, err := util.GetWatchNamespace()
	if err != nil {
		klog.Errorf("failed to get provider cluster namespace. %v", err)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	providerServer, err := server.NewOCSProviderServer(ctx, namespace)
	if err != nil {
		klog.Errorf("failed to start provider server. %v", err)
		return
	}

	providerServer.Start(*port, []grpc.ServerOption{})
}
