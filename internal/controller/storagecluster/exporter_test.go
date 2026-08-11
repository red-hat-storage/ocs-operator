package storagecluster

import (
	"context"
	"os"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func TestDeployMetricsExporterSetsTLSProfileEnvironment(t *testing.T) {
	scheme := createFakeScheme(t)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &StorageClusterReconciler{
		Client:            kubeClient,
		OperatorNamespace: "openshift-storage",
		images:            ImageMap{OCSMetricsExporter: "metrics-exporter:test"},
	}
	storageCluster := &ocsv1.StorageCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: ocsv1.GroupVersion.String(), Kind: "StorageCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-storagecluster",
			Namespace: "openshift-storage-extended",
		},
	}
	profile := &ocstlsv1.TLSProfile{ObjectMeta: metav1.ObjectMeta{Generation: 7}}

	require.NoError(t, deployMetricsExporter(context.Background(), r, storageCluster, profile))

	deployment := &appsv1.Deployment{}
	require.NoError(t, kubeClient.Get(context.Background(), types.NamespacedName{
		Name: metricsExporterName, Namespace: storageCluster.Namespace,
	}, deployment))
	env := deployment.Spec.Template.Spec.Containers[0].Env
	assert.Contains(t, env, corev1.EnvVar{Name: util.OperatorNamespaceEnvVar, Value: r.OperatorNamespace})
	assert.Contains(t, env, corev1.EnvVar{Name: tlsProfileGenerationEnvName, Value: "7"})
}

func TestMetricsExporterCanGetTLSProfiles(t *testing.T) {
	scheme := createFakeScheme(t)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &StorageClusterReconciler{Client: kubeClient}

	require.NoError(t, updateMetricsExporterClusterRoles(context.Background(), r))

	role := &rbacv1.ClusterRole{}
	require.NoError(t, kubeClient.Get(context.Background(), types.NamespacedName{Name: metricsExporterName}, role))
	assert.Contains(t, role.Rules, rbacv1.PolicyRule{
		APIGroups: []string{"ocs.openshift.io"},
		Resources: []string{"tlsprofiles"},
		Verbs:     []string{"get"},
	})
}

func TestMetricsExporterNetworkPolicyMatchesYAML(t *testing.T) {
	const namespace = "openshift-storage"

	scheme := createFakeScheme(t)
	require.NoError(t, networkingv1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &StorageClusterReconciler{
		Client: kubeClient,
		Scheme: scheme,
	}
	instance := &ocsv1.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: ocsv1.GroupVersion.String(),
			Kind:       "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storagecluster",
			Namespace: namespace,
		},
	}

	require.NoError(t, createMetricsExporterNetworkPolicy(context.Background(), r, instance))

	generated := &networkingv1.NetworkPolicy{}
	require.NoError(t, kubeClient.Get(context.Background(), types.NamespacedName{
		Name: metricsExporterName, Namespace: namespace,
	}, generated))

	yamlBytes, err := os.ReadFile("../../../metrics/deploy/networkpolicy.yaml")
	require.NoError(t, err, "failed to read networkpolicy.yaml — if the file moved, update the path")

	fromYAML := &networkingv1.NetworkPolicy{}
	require.NoError(t, yaml.Unmarshal(yamlBytes, fromYAML))

	assert.Equal(t, fromYAML.Spec, generated.Spec,
		"the YAML in metrics/deploy/networkpolicy.yaml has drifted from the Go code in exporter.go — update one to match the other")
}
