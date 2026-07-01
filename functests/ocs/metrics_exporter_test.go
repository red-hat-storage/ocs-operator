package ocs_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	tests "github.com/red-hat-storage/ocs-operator/v4/functests"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	metricsExporterDeployName  = "ocs-metrics-exporter"
	metricsExporterPodLabels   = "app.kubernetes.io/name=ocs-metrics-exporter"
	metricsExporterServiceName = "ocs-metrics-exporter"
	metricsPath                = "/metrics"
	metricsHTTPSPort           = "8443"
	// curl image from Docker Hub for in-cluster HTTPS scrape (short-lived Job).
	metricsScrapeCurlImage     = "curlimages/curl:8.10.1"
	metricsScrapeContainerName = "curl"
	// Must match ruleName in internal/controller/storagecluster/prometheus.go.
	ocsPrometheusRulesName = "ocs-prometheus-rules"
)

// metricsSubstrings must appear in the Prometheus exposition from ocs-metrics-exporter
// (see metrics/internal/collectors: ceph-cluster, storage-cluster, ceph-object-store).
var metricsSubstrings = []string{
	"ocs_mirror_daemon_count",
	"ocs_storagecluster_kms_connection_status",
	"ocs_rgw_health_status",
}

type metricsExporterCheck struct {
	kubeClient    client.Client
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface
	namespace     string
}

func newMetricsExporterCheck() (*metricsExporterCheck, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("KUBECONFIG is not set")
	}
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &metricsExporterCheck{
		kubeClient:    tests.DeployManager.Client,
		clientset:     cs,
		dynamicClient: dyn,
		namespace:     tests.InstallNamespace,
	}, nil
}

func (m *metricsExporterCheck) deploymentReady() error {
	dep := &appsv1.Deployment{}
	err := m.kubeClient.Get(context.TODO(), client.ObjectKey{
		Namespace: m.namespace,
		Name:      metricsExporterDeployName,
	}, dep)
	if err != nil {
		return fmt.Errorf("get deployment %s: %w", metricsExporterDeployName, err)
	}
	want := int32(1)
	if dep.Spec.Replicas != nil {
		want = *dep.Spec.Replicas
	}
	if want == 0 {
		return nil
	}
	if dep.Status.AvailableReplicas < want {
		return fmt.Errorf("deployment %s has %d/%d available replicas",
			metricsExporterDeployName, dep.Status.AvailableReplicas, want)
	}
	if dep.Generation != dep.Status.ObservedGeneration {
		return fmt.Errorf("deployment %s rollout in progress (generation %d, observed %d)",
			metricsExporterDeployName, dep.Generation, dep.Status.ObservedGeneration)
	}
	return nil
}

func (m *metricsExporterCheck) podRunning() error {
	pods := &corev1.PodList{}
	sel, err := labels.Parse(metricsExporterPodLabels)
	if err != nil {
		return err
	}
	err = m.kubeClient.List(context.TODO(), pods, client.InNamespace(m.namespace), &client.ListOptions{
		LabelSelector: sel,
	})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods with label %s", metricsExporterPodLabels)
	}
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning {
			for _, c := range p.Status.ContainerStatuses {
				if c.Name == metricsExporterDeployName && c.Ready {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("no running ready pod for %s", metricsExporterDeployName)
}

// fetchMetricsBody runs a short-lived Job that curls the metrics Service in-cluster.
// Uses curlimages/curl (Docker Hub). Avoids k8s.io/client-go/tools/portforward, which is not vendored.
func (m *metricsExporterCheck) fetchMetricsBody(ctx context.Context) ([]byte, error) {
	jobName := fmt.Sprintf("ocs-metricscheck-%d", time.Now().UnixNano())
	curlURL := fmt.Sprintf("https://%s:%s%s", metricsExporterServiceName, metricsHTTPSPort, metricsPath)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: m.namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			TTLSecondsAfterFinished: ptr.To(int32(600)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:    metricsScrapeContainerName,
						Image:   metricsScrapeCurlImage,
						Command: []string{"/bin/sh", "-c", "exec curl -sfk --connect-timeout 60 " + curlURL},
					}},
				},
			},
		},
	}
	if err := m.kubeClient.Create(ctx, job); err != nil {
		return nil, err
	}
	defer func() {
		_ = m.kubeClient.Delete(context.Background(), job, client.PropagationPolicy(metav1.DeletePropagationBackground))
	}()

	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		j := &batchv1.Job{}
		if err := m.kubeClient.Get(ctx, client.ObjectKey{Namespace: m.namespace, Name: jobName}, j); err != nil {
			return false, err
		}
		for _, c := range j.Status.Conditions {
			if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
				return true, nil
			}
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				return false, fmt.Errorf("metrics scrape job failed: %s", c.Message)
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	podList, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pod for job %s", jobName)
	}
	podName := podList.Items[0].Name
	req := m.clientset.CoreV1().Pods(m.namespace).GetLogs(podName, &corev1.PodLogOptions{Container: metricsScrapeContainerName})
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(stream)
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close log stream: %w", closeErr)
	}
	return body, nil
}

func (m *metricsExporterCheck) metricsExposeExpectedSeries(body []byte) error {
	text := string(body)
	var missing []string
	for _, sub := range metricsSubstrings {
		if !strings.Contains(text, sub) {
			missing = append(missing, sub)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("metrics response missing expected series: %v", missing)
	}
	return nil
}

func (m *metricsExporterCheck) prometheusOCSRulesPresent(ctx context.Context) error {
	gvr := schema.GroupVersionResource{
		Group:    "monitoring.coreos.com",
		Version:  "v1",
		Resource: "prometheusrules",
	}
	_, err := m.dynamicClient.Resource(gvr).Namespace(m.namespace).Get(ctx, ocsPrometheusRulesName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get PrometheusRule %q: %w", ocsPrometheusRulesName, err)
	}
	return nil
}

var _ = ginkgo.Describe("OCS metrics exporter", metricsExporterTest)

func metricsExporterTest() {
	var chk *metricsExporterCheck
	var err error

	ginkgo.BeforeEach(func() {
		chk, err = newMetricsExporterCheck()
		gomega.Expect(err).To(gomega.BeNil())
	})

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			tests.SuiteFailed = tests.SuiteFailed || true
		}
	})

	ginkgo.It("deployment is ready and core OCS metrics are exposed", func() {
		ginkgo.By("Checking ocs-metrics-exporter Deployment rollout")
		gomega.Eventually(chk.deploymentReady, 300*time.Second, 5*time.Second).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Checking metrics-exporter Pod is running and container ready")
		gomega.Eventually(chk.podRunning, 120*time.Second, 5*time.Second).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("Scraping /metrics and verifying representative series from collectors")
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		var body []byte
		gomega.Eventually(func() error {
			b, e := chk.fetchMetricsBody(ctx)
			if e != nil {
				return e
			}
			body = b
			return chk.metricsExposeExpectedSeries(b)
		}, 5*time.Minute, 15*time.Second).ShouldNot(gomega.HaveOccurred())
		gomega.Expect(len(body)).To(gomega.BeNumerically(">", 100))

		ginkgo.By("Checking OCS PrometheusRule (alert/recording definitions) exists")
		gomega.Eventually(func() error {
			return chk.prometheusOCSRulesPresent(context.Background())
		}, 120*time.Second, 5*time.Second).ShouldNot(gomega.HaveOccurred())
	})
}
