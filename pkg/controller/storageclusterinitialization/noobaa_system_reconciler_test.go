package storageclusterinitialization

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	v1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	coreEnvVar          = "NOOBAA_CORE_IMAGE"
	dbEnvVar            = "NOOBAA_DB_IMAGE"
	defaultStorageClass = "noobaa-ceph-rbd"
)

var nooBaaReconcileTestLogger = logf.Log.WithName("noobaa_system_reconciler_test")

func TestEnsureNooBaaSystem(t *testing.T) {
	namespacedName := types.NamespacedName{
		Name:      "noobaa",
		Namespace: "test_ns",
	}
	sci := v1.StorageClusterInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
	}
	noobaa := v1alpha1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
	}
	addressableStorageClass := defaultStorageClass

	cases := []struct {
		label          string
		namespacedName types.NamespacedName
		sci            v1.StorageClusterInitialization
		noobaa         v1alpha1.NooBaa
		isCreate       bool
	}{
		{
			label:          "case 1", //ensure create logic
			namespacedName: namespacedName,
			sci:            sci,
			noobaa:         noobaa,
			isCreate:       true,
		},
		{
			label:          "case 2", //ensure update logic
			namespacedName: namespacedName,
			sci:            sci,
			noobaa:         noobaa,
		},
		{
			label:          "case 3", //equal, no update
			namespacedName: namespacedName,
			sci:            sci,
			noobaa: v1alpha1.NooBaa{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
				Spec: v1alpha1.NooBaaSpec{
					DBStorageClass:            &addressableStorageClass,
					PVPoolDefaultStorageClass: &addressableStorageClass,
				},
			},
		},
	}

	for _, c := range cases {
		reconciler := getReconciler(t, &v1alpha1.NooBaa{})

		if c.isCreate {
			err := reconciler.client.Get(context.TODO(), namespacedName, &c.noobaa)
			assert.True(t, errors.IsNotFound(err))
		} else {
			err := reconciler.client.Create(context.TODO(), &c.noobaa)
			assert.NoError(t, err)
		}
		err := reconciler.ensureNoobaaSystem(&sci, nooBaaReconcileTestLogger)
		assert.NoError(t, err)

		noobaa = v1alpha1.NooBaa{}
		err = reconciler.client.Get(context.TODO(), namespacedName, &noobaa)
		assert.Equal(t, noobaa.Name, namespacedName.Name)
		assert.Equal(t, noobaa.Namespace, namespacedName.Namespace)
		if !c.isCreate {
			assert.Equal(t, *noobaa.Spec.DBStorageClass, defaultStorageClass)
			assert.Equal(t, *noobaa.Spec.PVPoolDefaultStorageClass, defaultStorageClass)
		}
	}
}

func TestNewNooBaaSystem(t *testing.T) {
	defaultInput := v1.StorageClusterInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test_name",
		},
	}
	cases := []struct {
		label       string
		envCore     string
		envDB       string
		initialData v1.StorageClusterInitialization
	}{
		{
			label:       "case 1", // both envVars carry through to created NooBaaSystem
			envCore:     "FOO",
			envDB:       "BAR",
			initialData: defaultInput,
		},
		{
			label:       "case 2", // missing core envVar causes no issue
			envDB:       "BAR",
			initialData: defaultInput,
		},
		{
			label:       "case 3", // missing db envVar causes no issue
			envCore:     "FOO",
			initialData: defaultInput,
		},
		{
			label:       "case 4", // neither envVar set, no issues occur
			initialData: defaultInput,
		},
		{
			label:       "case 5", // missing initData namespace does not cause error
			initialData: v1.StorageClusterInitialization{},
		},
	}

	for _, c := range cases {

		if c.envCore != "" {
			err := os.Setenv(coreEnvVar, c.envCore)
			if err != nil {
				assert.Failf(t, "[%s] unable to set env_var %s", c.label, coreEnvVar)
			}
		}
		if c.envDB != "" {
			err := os.Setenv(dbEnvVar, c.envDB)
			if err != nil {
				assert.Failf(t, "[%s] unable to set env_var %s", c.label, dbEnvVar)
			}
		}
		reconciler := ReconcileStorageClusterInitialization{}
		nooBaa := reconciler.newNooBaaSystem(&c.initialData, nooBaaReconcileTestLogger)

		assert.Equalf(t, nooBaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.NotEmptyf(t, nooBaa.Labels, "[%s] expected noobaa Labels not found", c.label)
		assert.Equalf(t, nooBaa.Labels["app"], "noobaa", "[%s] expected noobaa Label mismatch", c.label)
		assert.Equalf(t, nooBaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.Equal(t, *nooBaa.Spec.DBStorageClass, fmt.Sprintf("%s-ceph-rbd", c.initialData.Name))
		assert.Equal(t, *nooBaa.Spec.PVPoolDefaultStorageClass, fmt.Sprintf("%s-ceph-rbd", c.initialData.Name))
		assert.Equalf(t, nooBaa.Namespace, c.initialData.Namespace, "[%s] namespace mismatch", c.label)
		if c.envCore != "" {
			assert.Equalf(t, *nooBaa.Spec.Image, c.envCore, "[%s] core envVar not applied to noobaa spec", c.label)
		}
		if c.envDB != "" {
			assert.Equalf(t, *nooBaa.Spec.DBImage, c.envDB, "[%s] core envVar not applied to noobaa spec", c.label)
		}
	}
}

func getReconciler(t *testing.T, objs ...runtime.Object) ReconcileStorageClusterInitialization {
	registerObjs := []runtime.Object{&v1.StorageClusterInitialization{}}
	registerObjs = append(registerObjs, objs...)
	v1.SchemeBuilder.Register(registerObjs...)

	scheme, err := v1.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	client := fake.NewFakeClientWithScheme(scheme, registerObjs...)

	return ReconcileStorageClusterInitialization{
		scheme: scheme,
		client: client,
	}
}
