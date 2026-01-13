package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"k8s.io/klog/v2"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Reference taken from OCP console proxy implementation: https://github.com/openshift/console/blob/main/pkg/proxy/proxy.go

type Proxy struct {
	reverseProxy *httputil.ReverseProxy
	endpoint     string
}

type Config struct {
	Endpoint string // e.g., "https://external-rgw.example.com"
	CACert   []byte // CA certificate for TLS validation
}

var (
	// NOTE: In real implementation, these will be read from:
	// - endpoint: CephObjectStore CR status (external RGW endpoint)
	// - caCert: Secret containing CA certificate for external RGW
	// using static configuration just for overview purpose currently
	staticRGWEndpoint = "https://external-rgw.example.com"
	staticCACert      = []byte(`-----BEGIN CERTIFICATE-----
TODO: Replace with actual CA certificate from Secret
-----END CERTIFICATE-----`)
)

func HandleProxy(w http.ResponseWriter, r *http.Request, client ctrlclient.Client, namespace string) {
	// URL format:  /proxy/{endpoint}/*
	// endpoint = "rgw" (currently only supported endpoint)
	// Extract the "endpoint" from the URL path and create proxy object based on the type
	// Return error if the endpoint is not supported

	proxy, err := GetRGWProxy()
	if err != nil {
		klog.Errorf("Failed to create RGW proxy: %v", err)
		http.Error(w, "Failed to initialize proxy", http.StatusInternalServerError)
		return
	}

	// Preserve query parameters
	// Preserve request method
	// Preserve request body
	// Forward the request
	proxy.ServeHTTP(w, r)
}

func NewProxy(cfg *Config) (*Proxy, error) {
	endpointURL, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}

	// NOTE: In real implementation, will load CA cert from Secret/CRs and can also add validations
	tlsConfig := &tls.Config{
		// RootCAs: customCA,
		// InsecureSkipVerify: false,
	}

	if len(cfg.CACert) > 0 {
		rootCAs := x509.NewCertPool()
		if !rootCAs.AppendCertsFromPEM(cfg.CACert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = rootCAs
		tlsConfig.InsecureSkipVerify = false
	}

	transport := &http.Transport{
		// MaxIdleConns:        <configurable>,
		// MaxIdleConnsPerHost: <configurable>,
		// IdleConnTimeout:     <configurable>,
		// DisableKeepAlives:   <configurable>,
		// DialContext: (&net.Dialer{
		// 	Timeout:   <configurable>,
		// 	KeepAlive: <configurable>,
		// }).DialContext,
		// TLSHandshakeTimeout:   <configurable>,
		// ResponseHeaderTimeout: <configurable>,
		// ExpectContinueTimeout: <configurable>,
		// Any other transport configuration can be added here (need to check what all is supported)
		TLSClientConfig: tlsConfig,
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(endpointURL)
	reverseProxy.Transport = transport
	reverseProxy.FlushInterval = 100 * time.Millisecond

	// Remove problematic headers
	// These headers aren't things that proxies should pass along. Some are forbidden by http2.
	// This fixes the bug where Chrome users saw a ERR_SPDY_PROTOCOL_ERROR for all proxied requests.
	// Reference: https://github.com/openshift/console/blob/main/pkg/proxy/proxy.go
	reverseProxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Connection")
		resp.Header.Del("Keep-Alive")
		resp.Header.Del("Proxy-Connection")
		resp.Header.Del("Transfer-Encoding")
		resp.Header.Del("Upgrade")
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")
		// Any other header can be removed here (need to check what all is not needed)

		return nil
	}

	return &Proxy{
		reverseProxy: reverseProxy,
		endpoint:     cfg.Endpoint,
	}, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	klog.V(4).Infof("Proxying %s request to %s%s", r.Method, p.endpoint, r.URL.Path)

	// Authentication/Authorization
	// Already handled by kube-rbac-proxy sidecar before request reaches here

	// passing "endpoint" as a path parameter to the URL so that we explicitly handle only the supported endpoints
	// kind of whitelisting for only the supported/desired external endpoints

	p.reverseProxy.ServeHTTP(w, r)
}

func GetRGWProxy() (*Proxy, error) {
	cfg := &Config{
		Endpoint: staticRGWEndpoint,
		CACert:   staticCACert,
	}
	return NewProxy(cfg)
}
