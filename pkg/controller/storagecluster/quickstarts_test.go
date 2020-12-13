package storagecluster

import (
	"context"
	"github.com/ghodss/yaml"
	consolev1 "github.com/openshift/api/console/v1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"testing"
)

var (
	cases = []struct {
		quickstartName string
	}{
		{
			quickstartName: "getting-started-ocs",
		},
		{
			quickstartName: "ocs-configuration",
		},
	}
)

func TestQuickStartYAMLs(t *testing.T) {
	for _, qs := range AllQuickStarts {
		cqs := consolev1.ConsoleQuickStart{}
		err := yaml.Unmarshal(qs, &cqs)
		assert.NoError(t, err)
	}
}
func TestEnsureQuickStarts(t *testing.T) {
	allExpectedQuickStarts := []consolev1.ConsoleQuickStart{}
	for _, qs := range AllQuickStarts {
		cqs := consolev1.ConsoleQuickStart{}
		err := yaml.Unmarshal(qs, &cqs)
		assert.NoError(t, err)
		allExpectedQuickStarts = append(allExpectedQuickStarts, cqs)
	}
	cqs := &consolev1.ConsoleQuickStart{}
	reconciler := createFakeStorageClusterReconciler(t, cqs)
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	err := reconciler.ensureQuickStarts(sc, reconciler.reqLogger)
	assert.NoError(t, err)
	for _, c := range cases {
		qs := consolev1.ConsoleQuickStart{}
		err = reconciler.client.Get(context.TODO(), types.NamespacedName{
			Name: c.quickstartName,
		}, &qs)
		assert.NoError(t, err)
		found := consolev1.ConsoleQuickStart{}
		expected := consolev1.ConsoleQuickStart{}
		for _, cqs := range allExpectedQuickStarts {
			if qs.Name == cqs.Name {
				found = qs
				expected = cqs
				break
			}
		}
		assert.Equal(t, expected.Name, found.Name)
		assert.Equal(t, expected.Namespace, found.Namespace)
		assert.Equal(t, expected.Spec.DurationMinutes, found.Spec.DurationMinutes)
		assert.Equal(t, expected.Spec.Introduction, found.Spec.Introduction)
		assert.Equal(t, expected.Spec.DisplayName, found.Spec.DisplayName)
	}
	assert.Equal(t, len(allExpectedQuickStarts), len(getActualQuickStarts(t, cases, &reconciler)))
}

func getActualQuickStarts(t *testing.T, cases []struct {
	quickstartName string
}, reconciler *ReconcileStorageCluster) []consolev1.ConsoleQuickStart {
	allActualQuickStarts := []consolev1.ConsoleQuickStart{}
	for _, c := range cases {
		qs := consolev1.ConsoleQuickStart{}
		err := reconciler.client.Get(context.TODO(), types.NamespacedName{
			Name: c.quickstartName,
		}, &qs)
		if apierrors.IsNotFound(err) {
			continue
		}
		assert.NoError(t, err)
		allActualQuickStarts = append(allActualQuickStarts, qs)
	}
	return allActualQuickStarts
}
