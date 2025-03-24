package storageautoscaler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	prometheusconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
)

const clientTimeout = 10 * time.Second

func getPrometheusURL(operatorNamespace string) (string, error) {
	prometheusURL := "https://prometheus-k8s.openshift-monitoring.svc.cluster.local:9091"
	if isROSAHCP, err := platform.IsPlatformROSAHCP(); err != nil {
		return "", err
	} else if isROSAHCP {
		prometheusURL = fmt.Sprintf("https://prometheus.%s.svc.cluster.local:9339", operatorNamespace)
	}

	return prometheusURL, nil
}

func getClient(ctx context.Context, log logr.Logger) (*http.Client, error) {
	secrets := "/var/run/secrets/kubernetes.io/serviceaccount"
	caCertPath := secrets + "/service-ca.crt"
	tokenPath := secrets + "/token"

	// create auth roundtripper
	tlsConfig, err := prometheusconfig.NewTLSConfig(&prometheusconfig.TLSConfig{
		CAFile: caCertPath,
	})
	if err != nil {
		log.Error(err, "failed to create tls config")
		return nil, err
	}
	settings := prometheusconfig.TLSRoundTripperSettings{}
	newRT := func(cfg *tls.Config) (http.RoundTripper, error) {
		return &http.Transport{TLSClientConfig: cfg}, nil
	}
	tlsRoundTripper, err := prometheusconfig.NewTLSRoundTripperWithContext(ctx, tlsConfig, settings, newRT)
	if err != nil {
		log.Error(err, "failed to create tls round tripper")
		return nil, err
	}
	roundTripper := prometheusconfig.NewAuthorizationCredentialsRoundTripper("Bearer", prometheusconfig.NewFileSecret(tokenPath), tlsRoundTripper)
	// create prometheus client
	client := &http.Client{
		Transport: roundTripper,
		Timeout:   clientTimeout,
	}

	return client, nil
}

type PrometheusResponse struct {
	Status string       `json:"status"`
	Data   VectorResult `json:"data"`
}

// VectorResult contains only the vector result type
type VectorResult struct {
	ResultType string       `json:"resultType"`
	Result     model.Vector `json:"result"`
}

func queryMetrics(client *http.Client, operatorNamespace string, query string, log logr.Logger) (model.Vector, error) {
	prometheusURL, err := getPrometheusURL(operatorNamespace)
	if err != nil {
		log.Error(err, "failed to determine if ROSA HCP cluster")
		return nil, err
	}
	queryUrl := fmt.Sprintf("%s/api/v1/query?query=%s", prometheusURL, url.QueryEscape(query))

	resp, err := client.Get(queryUrl)
	if err != nil {
		log.Error(err, "error creating get request")
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "failed to read get response")
		return nil, err
	}
	defer resp.Body.Close()

	var promResp PrometheusResponse
	if err := json.Unmarshal([]byte(body), &promResp); err != nil {
		log.Error(err, "error unmarshaling prometheusResponse JSON")
		return nil, err
	}
	vector := promResp.Data.Result

	return vector, nil
}

func scrapeMetrics(ctx context.Context, operatorNamespace string, query string, log logr.Logger) (model.Vector, error) {
	client, err := getClient(ctx, log)
	if err != nil {
		log.Error(err, "failed to get scraper")
		return nil, err
	}
	return queryMetrics(client, operatorNamespace, query, log)
}
