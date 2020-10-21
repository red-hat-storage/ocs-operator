package persistentvolume

import (
	"context"
	"testing"

	api "github.com/openshift/ocs-operator/api/v1"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var mockStorageClass = &storagev1.StorageClass{
	ObjectMeta: metav1.ObjectMeta{
		Name: "test-sc",
	},
	Parameters: map[string]string{},
}

var mockPersistentVolume = &corev1.PersistentVolume{
	ObjectMeta: metav1.ObjectMeta{
		Name: "test-pv",
	},
	Spec: corev1.PersistentVolumeSpec{
		PersistentVolumeSource: corev1.PersistentVolumeSource{
			CSI: &corev1.CSIPersistentVolumeSource{
				ControllerExpandSecretRef: &corev1.SecretReference{},
			},
		},
	},
}

func TestEnsureExpansionSecret(t *testing.T) {
	cases := []struct {
		label            string
		storageClassName string
		secretName       string
		secretNamespace  string
	}{
		{
			label: "no StorageClass",
		},
		{
			label:            "valid StorageClass, no secret",
			storageClassName: "test-sc",
		},
		{
			label:            "valid StorageClass, valid secret",
			storageClassName: "test-sc",
			secretName:       "expand-secret",
			secretNamespace:  "test-namespace",
		},
	}

	for i, c := range cases {
		t.Logf("Case %d: %s\n", i+1, c.label)

		testPV := &corev1.PersistentVolume{}
		mockPersistentVolume.DeepCopyInto(testPV)
		testPV.Spec.StorageClassName = c.storageClassName

		testSC := &storagev1.StorageClass{}
		mockStorageClass.DeepCopyInto(testSC)
		testSC.Parameters[csiExpansionSecretName] = c.secretName
		testSC.Parameters[csiExpansionSecretNamespace] = c.secretName

		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testPV.GetName(),
				Namespace: "",
			},
		}
		reconciler := createFakePersistentVolumeReconciler(t, testPV, testSC)
		result, err := reconciler.Reconcile(request)
		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)

		actual := &corev1.PersistentVolume{}
		err = reconciler.Client.Get(context.TODO(), request.NamespacedName, actual)
		assert.NoError(t, err)

		actualSecretRef := actual.Spec.PersistentVolumeSource.CSI.ControllerExpandSecretRef
		assert.Equal(t, testSC.Parameters[csiExpansionSecretName], actualSecretRef.Name)
		assert.Equal(t, testSC.Parameters[csiExpansionSecretNamespace], actualSecretRef.Namespace)
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme, err := api.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	err = corev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add corev1 scheme")
	}
	err = storagev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add storagev1 scheme")
	}
	return scheme
}

func createFakePersistentVolumeReconciler(t *testing.T, obj ...runtime.Object) PersistentVolumeReconciler {
	scheme := createFakeScheme(t)
	client := fake.NewFakeClientWithScheme(scheme, obj...)

	return PersistentVolumeReconciler{
		Client: client,
		Scheme: scheme,
		Log:    logf.Log.WithName("controller_persistentvolume_test"),
	}
}
