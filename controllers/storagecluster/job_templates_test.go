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
var expectedlabels = map[string]string{
	"app": "ceph-toolbox-job",
}

var expectedAnnotations = map[string]string{
	"template.alpha.openshift.io/wait-for-ready": "true",
}

func TestTemplateCreated(t *testing.T) {

	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
		t, nil, nil)

	for _, s := range templateNames {
		assertJobs(s, t, reconciler, cr, request)
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

func assertJobs(name string, t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {

	ocsJob := &batchv1.Job{

		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.Namespace,
		},
	}

	request.Name = name
	if name == "ocs-extend-cluster" {
		err := reconciler.Client.Create(context.TODO(), newExtendClusterJob(cr, name, extendClusterCommand))
		assert.NoError(t, err)
		err = reconciler.Client.Get(context.TODO(), request.NamespacedName, ocsJob)
		assert.NoError(t, err)
		assert.Equal(t, ocsJob.Spec.Template.Spec.Containers[0].Command, extendClusterCommand)
		assertLabelsAndAnnotations(ocsJob.Annotations, ocsJob.Labels, t)
	} else if name == "ocs-osd-removal" {
		err := reconciler.Client.Create(context.TODO(), newosdCleanUpJob(cr, name, osdCleanupArgs))
		assert.NoError(t, err)
		err = reconciler.Client.Get(context.TODO(), request.NamespacedName, ocsJob)
		assert.NoError(t, err)
		assert.Equal(t, ocsJob.Spec.Template.Spec.Containers[0].Args, osdCleanupArgs)
		assertLabelsAndAnnotations(ocsJob.Annotations, ocsJob.Labels, t)

	}

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

func assertLabelsAndAnnotations(annotations map[string]string, labels map[string]string, t *testing.T) {
	assert.Equal(t, annotations, expectedAnnotations)
	assert.Equal(t, labels, expectedlabels)

}
