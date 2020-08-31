package options

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

// default options
const (
	host                      = "0.0.0.0"
	customResourceMetricsPort = 8080
	exporterMetricsPort       = 8081
)

// Options are the configurable parameters for kube-events-exporter.
type Options struct {
	Apiserver         string
	KubeconfigPath    string
	Host              string
	Port              int
	ExporterHost      string
	ExporterPort      int
	Help              bool
	AllowedNamespaces []string

	flags      *pflag.FlagSet
	StopCh     chan struct{}
	Kubeconfig *rest.Config
}

// NewOptions returns a new instance of `Options`.
func NewOptions() *Options {
	return &Options{}
}

// AddFlags creates a flagset and initializes Options
func (o *Options) AddFlags() {
	o.flags = pflag.NewFlagSet("", pflag.ExitOnError)

	o.flags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		o.flags.PrintDefaults()
	}

	o.flags.StringVar(&o.Apiserver, "apiserver", "", "The URL of the apiserver to use as a master.")
	o.flags.StringVar(&o.KubeconfigPath, "kubeconfig", os.Getenv("KUBECONFIG"), "Absolute path to the kubeconfig file.")
	o.flags.StringVar(&o.Host, "host", host, "Host to expose custom resource metrics on.")
	o.flags.IntVar(&o.Port, "port", customResourceMetricsPort, "Port to expose custom resource metrics on.")
	o.flags.StringVar(&o.ExporterHost, "exporter-host", host, "Host to expose exporter self metrics on.")
	o.flags.IntVar(&o.ExporterPort, "exporter-port", exporterMetricsPort, "Port to expose exporter self metrics on.")
	o.flags.BoolVar(&o.Help, "help", false, "To display Usage information.")
	o.flags.StringArrayVar(&o.AllowedNamespaces, "namespaces", []string{"openshift-storage"}, "List of namespaces to be monitored.")
}

// Parse parses the flags
func (o *Options) Parse() {
	err := o.flags.Parse(os.Args)
	if err != nil {
		klog.Fatalf("failed to parse options: %v", err)
	}
}

// Usage displays the usage
func (o *Options) Usage() {
	o.flags.Usage()
}
