package storagecluster

import (
	"context"
	"testing"

	openshiftv1 "github.com/openshift/api/template/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestJobTemplates(t *testing.T) {

	var cases = []struct {
		jobTemplateName    string
		templateParameters []string
		jobCmds            []string
		jobFunc            func(*api.StorageCluster, string, []string) *batchv1.Job
	}{
		{
			jobTemplateName:    "ocs-osd-removal",
			templateParameters: []string{"FAILED_OSD_IDS", "FORCE_OSD_REMOVAL"},
			jobCmds:            osdCleanupArgs,
			jobFunc:            newosdCleanUpJob,
		},
		{
			jobTemplateName:    "ocs-extend-cluster",
			templateParameters: []string{"RECONFIGURE"},
			jobCmds:            extendClusterCommand,
			jobFunc:            newExtendClusterJob,
		},
	}

	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, nil, nil)

	for _, c := range cases {
		template := &openshiftv1.Template{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.jobTemplateName,
				Namespace: cr.Namespace,
			},
		}

		request.Name = c.jobTemplateName
		err := reconciler.Client.Get(context.TODO(), request.NamespacedName, template)

		assert.NoError(t, err)
		for i, param := range c.templateParameters {
			assert.Equal(t, param, template.Parameters[i].Name)
		}

		expectedJob := c.jobFunc(cr, c.jobTemplateName, c.jobCmds)
		actualJob := &batchv1.Job{}
		actualRaw := template.Objects[0].Raw

		err = runtime.DecodeInto(unstructured.UnstructuredJSONScheme, actualRaw, actualJob)
		assert.NoError(t, err)

		assert.Equal(t, expectedJob, actualJob)
	}

	actualTemplateList := &openshiftv1.TemplateList{}
	err := reconciler.Client.List(context.TODO(), actualTemplateList)
	assert.NoError(t, err)
	assert.Equal(t, len(cases), len(actualTemplateList.Items))

}
