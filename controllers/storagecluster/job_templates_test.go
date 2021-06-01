package storagecluster

import (
	"context"
	"testing"

	openshiftv1 "github.com/openshift/api/template/v1"
	api "github.com/openshift/ocs-operator/api/v1"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var expectedOcsRemovalParameterName = "FAILED_OSD_IDS"
var expectedOcsExtendParameterName = "RECONFIGURE"
var templateNames = []string{
	"ocs-osd-removal",
	"ocs-extend-cluster",
}

func TestTemplateCreated(t *testing.T) {

	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
		t, nil, nil)

	for _, s := range templateNames {
		assertTemplates(s, t, reconciler, cr, request)
	}

	expectedTemplates(t, reconciler, cr, request)
}

func assertTemplates(name string, t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {

	var expectedParameterName = ""

	template := &openshiftv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.Namespace,
		},
	}

	if name == "ocs-extend-cluster" {
		expectedParameterName = expectedOcsExtendParameterName
	} else if name == "ocs-osd-removal" {
		expectedParameterName = expectedOcsRemovalParameterName
	}
	request.Name = name
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, template)
	assert.NoError(t, err)
	assert.Equal(t, template.Parameters[0].Name, expectedParameterName)
}

func expectedTemplates(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {

	var count = 0

	TestedJob :=
		map[string]bool{
			"ocs-osd-removal":    true,
			"ocs-extend-cluster": true,
		}

	actualJobList := &batchv1.JobList{}
	err := reconciler.Client.List(context.TODO(), actualJobList)
	assert.NoError(t, err)
	for _, node := range actualJobList.Items {
		_, found := TestedJob[node.ObjectMeta.Name]
		if found == true {
			count++
		}

	}
	assert.Equalf(t, len(TestedJob), count, "Unit tests are not added for one or more Job templates")
}
