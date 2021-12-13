package main

import (
	"flag"

	"github.com/red-hat-storage/ocs-operator/services/provider/server"
	"google.golang.org/grpc"
)

var (
	port = flag.Int("port", 50051, "The server port")
)

func main() {
	flag.Parse()
	var opts []grpc.ServerOption
	server.Start(*port, opts)
}
