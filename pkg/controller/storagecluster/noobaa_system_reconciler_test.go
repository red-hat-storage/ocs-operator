package storagecluster

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	v1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
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
	sc := v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
	}
	noobaa := v1alpha1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
			SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
		},
	}

	cephCluster := cephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephClusterFromString(namespacedName.Name),
			Namespace: namespacedName.Namespace,
		},
	}
	cephCluster.Status.State = cephv1.ClusterStateCreated

	addressableStorageClass := defaultStorageClass

	cases := []struct {
		label          string
		namespacedName types.NamespacedName
		sc             v1.StorageCluster
		noobaa         v1alpha1.NooBaa
		isCreate       bool
	}{
		{
			label:          "case 1", //ensure create logic
			namespacedName: namespacedName,
			sc:             sc,
			noobaa:         noobaa,
			isCreate:       true,
		},
		{
			label:          "case 2", //ensure update logic
			namespacedName: namespacedName,
			sc:             sc,
			noobaa:         noobaa,
		},
		{
			label:          "case 3", //equal, no update
			namespacedName: namespacedName,
			sc:             sc,
			noobaa: v1alpha1.NooBaa{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
					SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
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
		reconciler.client.Create(context.TODO(), &cephCluster)

		if c.isCreate {
			err := reconciler.client.Get(context.TODO(), namespacedName, &c.noobaa)
			assert.True(t, errors.IsNotFound(err))
		} else {
			err := reconciler.client.Create(context.TODO(), &c.noobaa)
			assert.NoError(t, err)
		}
		err := reconciler.ensureNoobaaSystem(&sc, nooBaaReconcileTestLogger)
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
	defaultInput := v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test_name",
		},
	}
	cases := []struct {
		label   string
		envCore string
		envDB   string
		sc      v1.StorageCluster
	}{
		{
			label:   "case 1", // both envVars carry through to created NooBaaSystem
			envCore: "FOO",
			envDB:   "BAR",
			sc:      defaultInput,
		},
		{
			label: "case 2", // missing core envVar causes no issue
			envDB: "BAR",
			sc:    defaultInput,
		},
		{
			label:   "case 3", // missing db envVar causes no issue
			envCore: "FOO",
			sc:      defaultInput,
		},
		{
			label: "case 4", // neither envVar set, no issues occur
			sc:    defaultInput,
		},
		{
			label: "case 5", // missing initData namespace does not cause error
			sc:    v1.StorageCluster{},
		},
	}

	for _, c := range cases {

		err := os.Setenv(coreEnvVar, c.envCore)
		if err != nil {
			assert.Failf(t, "[%s] unable to set env_var %s", c.label, coreEnvVar)
		}
		err = os.Setenv(dbEnvVar, c.envDB)
		if err != nil {
			assert.Failf(t, "[%s] unable to set env_var %s", c.label, dbEnvVar)
		}

		reconciler := ReconcileStorageCluster{}
		reconciler.initializeImageVars()
		nooBaa := reconciler.newNooBaaSystem(&c.sc, nooBaaReconcileTestLogger)

		assert.Equalf(t, nooBaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.NotEmptyf(t, nooBaa.Labels, "[%s] expected noobaa Labels not found", c.label)
		assert.Equalf(t, nooBaa.Labels["app"], "noobaa", "[%s] expected noobaa Label mismatch", c.label)
		assert.Equalf(t, nooBaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.Equal(t, *nooBaa.Spec.DBStorageClass, fmt.Sprintf("%s-ceph-rbd", c.sc.Name))
		assert.Equal(t, *nooBaa.Spec.PVPoolDefaultStorageClass, fmt.Sprintf("%s-ceph-rbd", c.sc.Name))
		assert.Equalf(t, nooBaa.Namespace, c.sc.Namespace, "[%s] namespace mismatch", c.label)
		if c.envCore != "" {
			assert.Equalf(t, *nooBaa.Spec.Image, c.envCore, "[%s] core envVar not applied to noobaa spec", c.label)
		}
		if c.envDB != "" {
			assert.Equalf(t, *nooBaa.Spec.DBImage, c.envDB, "[%s] db envVar not applied to noobaa spec", c.label)
		}
	}
}

func getReconciler(t *testing.T, objs ...runtime.Object) ReconcileStorageCluster {
	registerObjs := []runtime.Object{&v1.StorageCluster{}}
	registerObjs = append(registerObjs, objs...)
	v1.SchemeBuilder.Register(registerObjs...)

	scheme, err := v1.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	err = cephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1 scheme")
	}
	client := fake.NewFakeClientWithScheme(scheme, registerObjs...)

	return ReconcileStorageCluster{
		scheme:   scheme,
		client:   client,
		platform: &CloudPlatform{},
	}
}
