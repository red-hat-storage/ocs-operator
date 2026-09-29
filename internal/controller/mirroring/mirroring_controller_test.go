package mirroring

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	providerClient "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/client"
	storageclusterctrl "github.com/red-hat-storage/ocs-operator/v4/internal/controller/storagecluster"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	providerServer "github.com/red-hat-storage/ocs-operator/v4/services/provider/server"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const testNamespace = "test-namespace"

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		rookCephv1.AddToScheme,
		ocsv1alpha1.AddToScheme,
		ocsv1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return scheme
}

func newReconciler(fakeClient client.Client, scheme *runtime.Scheme) *MirroringReconciler {
	return &MirroringReconciler{
		Client: fakeClient,
		Scheme: scheme,
		ctx:    context.TODO(),
		log:    logf.Log.WithName("mirroring_test"),
	}
}

func newConfigMap(name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			UID:       types.UID("configmap-uid"),
		},
	}
}

func newStorageCluster(name string) *ocsv1.StorageCluster {
	return &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
	}
}

func newCephBlockPool(name string, labels map[string]string) *rookCephv1.CephBlockPool {
	return &rookCephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    labels,
		},
	}
}

func annotationIndexer(obj client.Object) []string {
	consumer := obj.(*ocsv1alpha1.StorageConsumer)
	keys := make([]string, 0, len(consumer.Annotations))
	for k := range consumer.Annotations {
		keys = append(keys, k)
	}
	return keys
}

func buildFakeClient(scheme *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&ocsv1alpha1.StorageConsumer{}, util.AnnotationIndexName, annotationIndexer).
		Build()
}

func TestReconcileRbdMirror(t *testing.T) {
	ctx := context.TODO()

	t.Run("creates CephRBDMirror with correct placement", func(t *testing.T) {
		scheme := newScheme(t)
		sc := newStorageCluster("test-sc")
		cm := newConfigMap("test-cm")

		r := newReconciler(buildFakeClient(scheme, cm, sc), scheme)
		err := r.reconcileRbdMirror(cm, true)
		assert.NoError(t, err)

		rbdMirror := &rookCephv1.CephRBDMirror{}
		err = r.Get(ctx, types.NamespacedName{Name: util.CephRBDMirrorName, Namespace: testNamespace}, rbdMirror)
		assert.NoError(t, err)
		assert.Equal(t, 1, rbdMirror.Spec.Count)
		assert.Equal(t, storageclusterctrl.GetPlacement(sc, "rbd-mirror"), rbdMirror.Spec.Placement)
	})

	t.Run("deletes CephRBDMirror when shouldMirror is false", func(t *testing.T) {
		scheme := newScheme(t)
		sc := newStorageCluster("test-sc")
		cm := newConfigMap("test-cm")
		existingMirror := &rookCephv1.CephRBDMirror{
			ObjectMeta: metav1.ObjectMeta{Name: util.CephRBDMirrorName, Namespace: testNamespace},
			Spec:       rookCephv1.RBDMirroringSpec{Count: 1},
		}

		r := newReconciler(buildFakeClient(scheme, cm, sc, existingMirror), scheme)
		err := r.reconcileRbdMirror(cm, false)
		assert.NoError(t, err)

		rbdMirror := &rookCephv1.CephRBDMirror{}
		err = r.Get(ctx, types.NamespacedName{Name: util.CephRBDMirrorName, Namespace: testNamespace}, rbdMirror)
		assert.Error(t, err, "CephRBDMirror should be deleted")
	})

	t.Run("deletes CephRBDMirror when maintenance mode requested", func(t *testing.T) {
		scheme := newScheme(t)
		sc := newStorageCluster("test-sc")
		cm := newConfigMap("test-cm")
		existingMirror := &rookCephv1.CephRBDMirror{
			ObjectMeta: metav1.ObjectMeta{Name: util.CephRBDMirrorName, Namespace: testNamespace},
			Spec:       rookCephv1.RBDMirroringSpec{Count: 1},
		}
		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "consumer-1",
				Namespace: testNamespace,
				Annotations: map[string]string{
					util.RequestMaintenanceModeAnnotation: "true",
				},
			},
		}

		r := newReconciler(buildFakeClient(scheme, cm, sc, existingMirror, consumer), scheme)
		err := r.reconcileRbdMirror(cm, true)
		assert.NoError(t, err)

		rbdMirror := &rookCephv1.CephRBDMirror{}
		err = r.Get(ctx, types.NamespacedName{Name: util.CephRBDMirrorName, Namespace: testNamespace}, rbdMirror)
		assert.Error(t, err, "CephRBDMirror should be deleted when maintenance mode requested")
	})

	t.Run("errors when multiple CephRBDMirrors exist", func(t *testing.T) {
		scheme := newScheme(t)
		sc := newStorageCluster("test-sc")
		cm := newConfigMap("test-cm")
		mirror1 := &rookCephv1.CephRBDMirror{
			ObjectMeta: metav1.ObjectMeta{Name: util.CephRBDMirrorName, Namespace: testNamespace},
		}
		mirror2 := &rookCephv1.CephRBDMirror{
			ObjectMeta: metav1.ObjectMeta{Name: "other-mirror", Namespace: testNamespace},
		}

		r := newReconciler(buildFakeClient(scheme, cm, sc, mirror1, mirror2), scheme)
		err := r.reconcileRbdMirror(cm, true)
		assert.Error(t, err)
	})

	t.Run("errors when CephRBDMirror name does not match", func(t *testing.T) {
		scheme := newScheme(t)
		sc := newStorageCluster("test-sc")
		cm := newConfigMap("test-cm")
		wrongNameMirror := &rookCephv1.CephRBDMirror{
			ObjectMeta: metav1.ObjectMeta{Name: "wrong-name", Namespace: testNamespace},
		}

		r := newReconciler(buildFakeClient(scheme, cm, sc, wrongNameMirror), scheme)
		err := r.reconcileRbdMirror(cm, true)
		assert.Error(t, err)
	})

	t.Run("adds maintenance mode annotation to StorageCluster", func(t *testing.T) {
		scheme := newScheme(t)
		sc := newStorageCluster("test-sc")
		cm := newConfigMap("test-cm")
		existingMirror := &rookCephv1.CephRBDMirror{
			ObjectMeta: metav1.ObjectMeta{Name: util.CephRBDMirrorName, Namespace: testNamespace},
			Spec:       rookCephv1.RBDMirroringSpec{Count: 1},
		}
		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "consumer-1",
				Namespace: testNamespace,
				Annotations: map[string]string{
					util.RequestMaintenanceModeAnnotation: "true",
				},
			},
		}

		r := newReconciler(buildFakeClient(scheme, cm, sc, existingMirror, consumer), scheme)
		err := r.reconcileRbdMirror(cm, true)
		assert.NoError(t, err)

		updatedSC := &ocsv1.StorageCluster{}
		err = r.Get(ctx, types.NamespacedName{Name: sc.Name, Namespace: testNamespace}, updatedSC)
		assert.NoError(t, err)
		assert.Equal(t, "true", updatedSC.GetAnnotations()[util.InMaintenanceModeAnnotation])
	})

	t.Run("removes maintenance mode annotation from StorageCluster", func(t *testing.T) {
		scheme := newScheme(t)
		sc := newStorageCluster("test-sc")
		sc.Annotations = map[string]string{util.InMaintenanceModeAnnotation: "true"}
		cm := newConfigMap("test-cm")

		r := newReconciler(buildFakeClient(scheme, cm, sc), scheme)
		err := r.reconcileRbdMirror(cm, true)
		assert.NoError(t, err)

		updatedSC := &ocsv1.StorageCluster{}
		err = r.Get(ctx, types.NamespacedName{Name: sc.Name, Namespace: testNamespace}, updatedSC)
		assert.NoError(t, err)
		_, exists := updatedSC.GetAnnotations()[util.InMaintenanceModeAnnotation]
		assert.False(t, exists)
	})
}

func TestReconcileBlockPoolMirroring(t *testing.T) {
	ctx := context.TODO()

	t.Run("enables mirroring with token and secret", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool := newCephBlockPool("pool-1", nil)

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{Items: []rookCephv1.CephBlockPool{*pool}}
		remoteInfo := map[string]*pb.BlockPoolInfo{
			"pool-1": {BlockPoolName: "pool-1", MirroringToken: "test-token", BlockPoolID: "bp-id-1"},
		}

		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, remoteInfo)
		assert.NoError(t, err)

		updatedPool := &rookCephv1.CephBlockPool{}
		err = fc.Get(ctx, types.NamespacedName{Name: "pool-1", Namespace: testNamespace}, updatedPool)
		assert.NoError(t, err)
		assert.True(t, updatedPool.Spec.Mirroring.Enabled)
		assert.Equal(t, "init-only", updatedPool.Spec.Mirroring.Mode)
		assert.Equal(t, "bp-id-1", updatedPool.GetAnnotations()[util.BlockPoolMirroringTargetIDAnnotation])

		secretName := GetMirroringSecretName("pool-1")
		assert.NotNil(t, updatedPool.Spec.Mirroring.Peers)
		assert.Contains(t, updatedPool.Spec.Mirroring.Peers.SecretNames, secretName)

		secret := &corev1.Secret{}
		err = fc.Get(ctx, types.NamespacedName{Name: secretName, Namespace: testNamespace}, secret)
		assert.NoError(t, err)
		assert.Equal(t, []byte("pool-1"), secret.Data["pool"])
		assert.Equal(t, []byte("test-token"), secret.Data["token"])
	})

	t.Run("disables mirroring when remote info is nil", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool := newCephBlockPool("pool-1", nil)
		pool.Spec.Mirroring = rookCephv1.MirroringSpec{Enabled: true, Mode: "init-only"}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{Items: []rookCephv1.CephBlockPool{*pool}}
		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, nil)
		assert.NoError(t, err)

		updatedPool := &rookCephv1.CephBlockPool{}
		err = fc.Get(ctx, types.NamespacedName{Name: "pool-1", Namespace: testNamespace}, updatedPool)
		assert.NoError(t, err)
		assert.False(t, updatedPool.Spec.Mirroring.Enabled)
	})

	t.Run("skips internal-use-only pool", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool := newCephBlockPool("internal-pool", map[string]string{
			util.ForInternalUseOnlyLabelKey: "true",
		})

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{Items: []rookCephv1.CephBlockPool{*pool}}
		remoteInfo := map[string]*pb.BlockPoolInfo{
			"internal-pool": {BlockPoolName: "internal-pool", MirroringToken: "token", BlockPoolID: "id"},
		}

		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, remoteInfo)
		assert.NoError(t, err)

		updatedPool := &rookCephv1.CephBlockPool{}
		err = fc.Get(ctx, types.NamespacedName{Name: "internal-pool", Namespace: testNamespace}, updatedPool)
		assert.NoError(t, err)
		assert.False(t, updatedPool.Spec.Mirroring.Enabled)
	})

	t.Run("skips forbid-mirroring pool", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool := newCephBlockPool("forbid-pool", map[string]string{
			util.ForbidMirroringLabel: "true",
		})

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{Items: []rookCephv1.CephBlockPool{*pool}}
		remoteInfo := map[string]*pb.BlockPoolInfo{
			"forbid-pool": {BlockPoolName: "forbid-pool", MirroringToken: "token", BlockPoolID: "id"},
		}

		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, remoteInfo)
		assert.NoError(t, err)

		updatedPool := &rookCephv1.CephBlockPool{}
		err = fc.Get(ctx, types.NamespacedName{Name: "forbid-pool", Namespace: testNamespace}, updatedPool)
		assert.NoError(t, err)
		assert.False(t, updatedPool.Spec.Mirroring.Enabled)
	})

	t.Run("skips erasure-coded pool", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool := newCephBlockPool("ec-pool", nil)
		pool.Spec.ErasureCoded = rookCephv1.ErasureCodedSpec{CodingChunks: 1, DataChunks: 2}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{Items: []rookCephv1.CephBlockPool{*pool}}
		remoteInfo := map[string]*pb.BlockPoolInfo{
			"ec-pool": {BlockPoolName: "ec-pool", MirroringToken: "token", BlockPoolID: "id"},
		}

		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, remoteInfo)
		assert.NoError(t, err)

		updatedPool := &rookCephv1.CephBlockPool{}
		err = fc.Get(ctx, types.NamespacedName{Name: "ec-pool", Namespace: testNamespace}, updatedPool)
		assert.NoError(t, err)
		assert.False(t, updatedPool.Spec.Mirroring.Enabled)
	})

	t.Run("errors when mirroring token is empty", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool := newCephBlockPool("pool-1", nil)

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{Items: []rookCephv1.CephBlockPool{*pool}}
		remoteInfo := map[string]*pb.BlockPoolInfo{
			"pool-1": {BlockPoolName: "pool-1", MirroringToken: "", BlockPoolID: "bp-id-1"},
		}

		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, remoteInfo)
		assert.Error(t, err)

		updatedPool := &rookCephv1.CephBlockPool{}
		err = fc.Get(ctx, types.NamespacedName{Name: "pool-1", Namespace: testNamespace}, updatedPool)
		assert.NoError(t, err)
		assert.True(t, updatedPool.Spec.Mirroring.Enabled, "mirroring should still be enabled even without token")
	})

	t.Run("disables mirroring when pool is not in remote map", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool := newCephBlockPool("pool-1", nil)
		pool.Spec.Mirroring = rookCephv1.MirroringSpec{Enabled: true, Mode: "init-only"}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{Items: []rookCephv1.CephBlockPool{*pool}}
		remoteInfo := map[string]*pb.BlockPoolInfo{
			"other-pool": {BlockPoolName: "other-pool", MirroringToken: "token", BlockPoolID: "id"},
		}

		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, remoteInfo)
		assert.NoError(t, err)

		updatedPool := &rookCephv1.CephBlockPool{}
		err = fc.Get(ctx, types.NamespacedName{Name: "pool-1", Namespace: testNamespace}, updatedPool)
		assert.NoError(t, err)
		assert.False(t, updatedPool.Spec.Mirroring.Enabled)
	})

	t.Run("handles multiple pools with mixed eligibility", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		pool1 := newCephBlockPool("pool-1", nil)
		pool2 := newCephBlockPool("pool-2", nil)
		internalPool := newCephBlockPool("internal-pool", map[string]string{
			util.ForInternalUseOnlyLabelKey: "true",
		})

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, pool1, pool2, internalPool).Build()
		r := newReconciler(fc, scheme)

		blockPoolList := &rookCephv1.CephBlockPoolList{
			Items: []rookCephv1.CephBlockPool{*pool1, *pool2, *internalPool},
		}
		remoteInfo := map[string]*pb.BlockPoolInfo{
			"pool-1":        {BlockPoolName: "pool-1", MirroringToken: "token-1", BlockPoolID: "id-1"},
			"pool-2":        {BlockPoolName: "pool-2", MirroringToken: "token-2", BlockPoolID: "id-2"},
			"internal-pool": {BlockPoolName: "internal-pool", MirroringToken: "token-3", BlockPoolID: "id-3"},
		}

		err := r.reconcileBlockPoolMirroring(cm, blockPoolList, remoteInfo)
		assert.NoError(t, err)

		for _, tc := range []struct {
			name           string
			expectMirror   bool
			expectSecretOk bool
		}{
			{"pool-1", true, true},
			{"pool-2", true, true},
			{"internal-pool", false, false},
		} {
			updatedPool := &rookCephv1.CephBlockPool{}
			err = fc.Get(ctx, types.NamespacedName{Name: tc.name, Namespace: testNamespace}, updatedPool)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectMirror, updatedPool.Spec.Mirroring.Enabled, "pool %s", tc.name)

			if tc.expectSecretOk {
				secret := &corev1.Secret{}
				err = fc.Get(ctx, types.NamespacedName{Name: GetMirroringSecretName(tc.name), Namespace: testNamespace}, secret)
				assert.NoError(t, err, "mirroring secret for pool %s", tc.name)
			}
		}
	})
}

func TestReconcileRadosNamespaceMirroring(t *testing.T) {
	ctx := context.TODO()

	t.Run("enables mirroring on rados namespace", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		cm.Data = map[string]string{"local-client-id": "remote-client-id"}

		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "consumer-1", Namespace: testNamespace, UID: types.UID("consumer-uid"),
			},
			Status: ocsv1alpha1.StorageConsumerStatus{
				Client: &ocsv1alpha1.ClientStatus{ID: "local-client-id"},
			},
		}
		rns := &rookCephv1.CephBlockPoolRadosNamespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "rns-1", Namespace: testNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "StorageConsumer", Name: "consumer-1", UID: consumer.UID},
				},
			},
			Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{BlockPoolName: "pool-1"},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, rns).Build()
		r := newReconciler(fc, scheme)

		err := r.reconcileRadosNamespaceMirroring(
			cm,
			map[string]*ocsv1alpha1.StorageConsumer{"consumer-1": consumer},
			map[string]*pb.ClientInfo{"remote-client-id": {ClientID: "remote-client-id", RadosNamespace: "remote-rns"}},
			map[string]*pb.BlockPoolInfo{"pool-1": {BlockPoolName: "pool-1", MirroringToken: "token", BlockPoolID: "id"}},
		)
		assert.NoError(t, err)

		updatedRNS := &rookCephv1.CephBlockPoolRadosNamespace{}
		err = fc.Get(ctx, types.NamespacedName{Name: "rns-1", Namespace: testNamespace}, updatedRNS)
		assert.NoError(t, err)
		assert.NotNil(t, updatedRNS.Spec.Mirroring)
		assert.Equal(t, ptr.To("remote-rns"), updatedRNS.Spec.Mirroring.RemoteNamespace)
		assert.Equal(t, rookCephv1.RadosNamespaceMirroringMode("image"), updatedRNS.Spec.Mirroring.Mode)
	})

	t.Run("disables mirroring when remote info is nil", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		rns := &rookCephv1.CephBlockPoolRadosNamespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "rns-1", Namespace: testNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "StorageConsumer", Name: "consumer-1", UID: "consumer-uid"},
				},
			},
			Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
				BlockPoolName: "pool-1",
				Mirroring:     &rookCephv1.RadosNamespaceMirroring{RemoteNamespace: ptr.To("remote-rns"), Mode: "image"},
			},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, rns).Build()
		r := newReconciler(fc, scheme)

		err := r.reconcileRadosNamespaceMirroring(cm, map[string]*ocsv1alpha1.StorageConsumer{}, nil, nil)
		assert.NoError(t, err)

		updatedRNS := &rookCephv1.CephBlockPoolRadosNamespace{}
		err = fc.Get(ctx, types.NamespacedName{Name: "rns-1", Namespace: testNamespace}, updatedRNS)
		assert.NoError(t, err)
		assert.Nil(t, updatedRNS.Spec.Mirroring)
	})

	t.Run("skips rados namespace without StorageConsumer owner", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		rns := &rookCephv1.CephBlockPoolRadosNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: "rns-no-owner", Namespace: testNamespace},
			Spec:       rookCephv1.CephBlockPoolRadosNamespaceSpec{BlockPoolName: "pool-1"},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, rns).Build()
		r := newReconciler(fc, scheme)

		err := r.reconcileRadosNamespaceMirroring(
			cm,
			map[string]*ocsv1alpha1.StorageConsumer{},
			map[string]*pb.ClientInfo{"remote-client-id": {ClientID: "remote-client-id", RadosNamespace: "remote-rns"}},
			map[string]*pb.BlockPoolInfo{"pool-1": {BlockPoolName: "pool-1"}},
		)
		assert.NoError(t, err)

		updatedRNS := &rookCephv1.CephBlockPoolRadosNamespace{}
		err = fc.Get(ctx, types.NamespacedName{Name: "rns-no-owner", Namespace: testNamespace}, updatedRNS)
		assert.NoError(t, err)
		assert.Nil(t, updatedRNS.Spec.Mirroring)
	})

	t.Run("disables mirroring when block pool is not in remote map", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		cm.Data = map[string]string{"local-client-id": "remote-client-id"}

		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "consumer-1", Namespace: testNamespace, UID: types.UID("consumer-uid"),
			},
			Status: ocsv1alpha1.StorageConsumerStatus{
				Client: &ocsv1alpha1.ClientStatus{ID: "local-client-id"},
			},
		}
		rns := &rookCephv1.CephBlockPoolRadosNamespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "rns-1", Namespace: testNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "StorageConsumer", Name: "consumer-1", UID: consumer.UID},
				},
			},
			Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
				BlockPoolName: "pool-not-in-remote",
				Mirroring:     &rookCephv1.RadosNamespaceMirroring{RemoteNamespace: ptr.To("old-rns"), Mode: "image"},
			},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, rns).Build()
		r := newReconciler(fc, scheme)

		err := r.reconcileRadosNamespaceMirroring(
			cm,
			map[string]*ocsv1alpha1.StorageConsumer{"consumer-1": consumer},
			map[string]*pb.ClientInfo{"remote-client-id": {ClientID: "remote-client-id", RadosNamespace: "remote-rns"}},
			map[string]*pb.BlockPoolInfo{"pool-1": {BlockPoolName: "pool-1"}},
		)
		assert.NoError(t, err)

		updatedRNS := &rookCephv1.CephBlockPoolRadosNamespace{}
		err = fc.Get(ctx, types.NamespacedName{Name: "rns-1", Namespace: testNamespace}, updatedRNS)
		assert.NoError(t, err)
		assert.Nil(t, updatedRNS.Spec.Mirroring)
	})
}

func TestReconcileStorageConsumer(t *testing.T) {
	ctx := context.TODO()

	t.Run("adds mirroring info annotation", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		cm.Data = map[string]string{"local-client-id": "remote-client-id"}
		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "consumer-1", Namespace: testNamespace},
			Status:     ocsv1alpha1.StorageConsumerStatus{Client: &ocsv1alpha1.ClientStatus{ID: "local-client-id"}},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, consumer).Build()
		r := newReconciler(fc, scheme)

		consumerList := &ocsv1alpha1.StorageConsumerList{Items: []ocsv1alpha1.StorageConsumer{*consumer}}
		remoteInfo := map[string]*pb.ClientInfo{
			"remote-client-id": {ClientID: "remote-client-id", RadosNamespace: "remote-rns", RbdStorageID: "storage-id"},
		}

		err := r.reconcileStorageConsumer(consumerList, cm, remoteInfo)
		assert.NoError(t, err)

		updatedConsumer := &ocsv1alpha1.StorageConsumer{}
		err = fc.Get(ctx, types.NamespacedName{Name: "consumer-1", Namespace: testNamespace}, updatedConsumer)
		assert.NoError(t, err)

		annotationValue := updatedConsumer.GetAnnotations()[util.StorageConsumerMirroringInfoAnnotation]
		assert.NotEmpty(t, annotationValue)

		var clientInfo pb.ClientInfo
		err = json.Unmarshal([]byte(annotationValue), &clientInfo)
		assert.NoError(t, err)
		assert.Equal(t, "remote-client-id", clientInfo.ClientID)
		assert.Equal(t, "remote-rns", clientInfo.RadosNamespace)
	})

	t.Run("removes mirroring info annotation when remote info is nil", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "consumer-1", Namespace: testNamespace,
				Annotations: map[string]string{util.StorageConsumerMirroringInfoAnnotation: `{"clientID":"old"}`},
			},
			Status: ocsv1alpha1.StorageConsumerStatus{Client: &ocsv1alpha1.ClientStatus{ID: "local-client-id"}},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, consumer).Build()
		r := newReconciler(fc, scheme)

		consumerList := &ocsv1alpha1.StorageConsumerList{Items: []ocsv1alpha1.StorageConsumer{*consumer}}
		err := r.reconcileStorageConsumer(consumerList, cm, nil)
		assert.NoError(t, err)

		updatedConsumer := &ocsv1alpha1.StorageConsumer{}
		err = fc.Get(ctx, types.NamespacedName{Name: "consumer-1", Namespace: testNamespace}, updatedConsumer)
		assert.NoError(t, err)
		_, exists := updatedConsumer.GetAnnotations()[util.StorageConsumerMirroringInfoAnnotation]
		assert.False(t, exists)
	})

	t.Run("no update when annotation already has correct value", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		cm.Data = map[string]string{"local-client-id": "remote-client-id"}

		clientInfo := &pb.ClientInfo{ClientID: "remote-client-id", RadosNamespace: "remote-rns"}
		marshaledInfo, _ := json.Marshal(clientInfo)

		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "consumer-1", Namespace: testNamespace,
				Annotations: map[string]string{util.StorageConsumerMirroringInfoAnnotation: string(marshaledInfo)},
			},
			Status: ocsv1alpha1.StorageConsumerStatus{Client: &ocsv1alpha1.ClientStatus{ID: "local-client-id"}},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, consumer).Build()
		r := newReconciler(fc, scheme)

		consumerList := &ocsv1alpha1.StorageConsumerList{Items: []ocsv1alpha1.StorageConsumer{*consumer}}
		err := r.reconcileStorageConsumer(consumerList, cm, map[string]*pb.ClientInfo{"remote-client-id": clientInfo})
		assert.NoError(t, err)

		updatedConsumer := &ocsv1alpha1.StorageConsumer{}
		err = fc.Get(ctx, types.NamespacedName{Name: "consumer-1", Namespace: testNamespace}, updatedConsumer)
		assert.NoError(t, err)
		assert.Equal(t, string(marshaledInfo), updatedConsumer.GetAnnotations()[util.StorageConsumerMirroringInfoAnnotation])
	})

	t.Run("skips consumer with no client status", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "consumer-no-client", Namespace: testNamespace},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, consumer).Build()
		r := newReconciler(fc, scheme)

		consumerList := &ocsv1alpha1.StorageConsumerList{Items: []ocsv1alpha1.StorageConsumer{*consumer}}
		err := r.reconcileStorageConsumer(consumerList, cm, map[string]*pb.ClientInfo{"some-id": {ClientID: "some-id"}})
		assert.NoError(t, err)

		updatedConsumer := &ocsv1alpha1.StorageConsumer{}
		err = fc.Get(ctx, types.NamespacedName{Name: "consumer-no-client", Namespace: testNamespace}, updatedConsumer)
		assert.NoError(t, err)
		_, exists := updatedConsumer.GetAnnotations()[util.StorageConsumerMirroringInfoAnnotation]
		assert.False(t, exists)
	})

	t.Run("skips consumer with unmapped client ID", func(t *testing.T) {
		scheme := newScheme(t)
		cm := newConfigMap("test-cm")
		cm.Data = map[string]string{"other-client": "remote-id"}
		consumer := &ocsv1alpha1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "consumer-1", Namespace: testNamespace},
			Status:     ocsv1alpha1.StorageConsumerStatus{Client: &ocsv1alpha1.ClientStatus{ID: "unmapped-client-id"}},
		}

		fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, consumer).Build()
		r := newReconciler(fc, scheme)

		consumerList := &ocsv1alpha1.StorageConsumerList{Items: []ocsv1alpha1.StorageConsumer{*consumer}}
		err := r.reconcileStorageConsumer(consumerList, cm, map[string]*pb.ClientInfo{"remote-id": {ClientID: "remote-id"}})
		assert.NoError(t, err)

		updatedConsumer := &ocsv1alpha1.StorageConsumer{}
		err = fc.Get(ctx, types.NamespacedName{Name: "consumer-1", Namespace: testNamespace}, updatedConsumer)
		assert.NoError(t, err)
		_, exists := updatedConsumer.GetAnnotations()[util.StorageConsumerMirroringInfoAnnotation]
		assert.False(t, exists)
	})
}

func TestGetBlockPoolsInfo(t *testing.T) {
	ctx := context.Background()

	t.Run("success with valid response", func(t *testing.T) {
		mockServer := &providerServer.MockOCSProviderServer{
			GetBlockPoolsInfoFunc: func(ctx context.Context, req *pb.BlockPoolsInfoRequest) (*pb.BlockPoolsInfoResponse, error) {
				return &pb.BlockPoolsInfoResponse{
					BlockPoolsInfo: []*pb.BlockPoolInfo{
						{
							BlockPoolName:  "pool1",
							MirroringToken: "token1",
							BlockPoolID:    "id1",
						},
						{
							BlockPoolName:  "pool2",
							MirroringToken: "token2",
							BlockPoolID:    "id2",
						},
					},
				}, nil
			},
		}

		addr, cleanup := providerServer.StartMockGRPCServer(t, mockServer)
		defer cleanup()

		client, err := providerClient.NewProviderClient(ctx, addr, 10*time.Second)
		require.NoError(t, err)
		defer client.Close()

		scheme := newScheme(t)
		fc := buildFakeClient(scheme)
		r := newReconciler(fc, scheme)

		storageClusterPeer := &ocsv1.StorageClusterPeer{
			Status: ocsv1.StorageClusterPeerStatus{
				PeerInfo: &ocsv1.PeerInfo{
					StorageClusterUid: "test-uid",
				},
			},
		}

		cephBlockPoolsList := &rookCephv1.CephBlockPoolList{
			Items: []rookCephv1.CephBlockPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "pool1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pool2"}},
			},
		}

		remoteBlockPoolInfoByName := map[string]*pb.BlockPoolInfo{}

		err = r.getBlockPoolsInfo(client, storageClusterPeer, cephBlockPoolsList, remoteBlockPoolInfoByName)
		assert.NoError(t, err)
		assert.Len(t, remoteBlockPoolInfoByName, 2)
		assert.Equal(t, "pool1", remoteBlockPoolInfoByName["pool1"].BlockPoolName)
		assert.Equal(t, "pool2", remoteBlockPoolInfoByName["pool2"].BlockPoolName)
		assert.Equal(t, "token1", remoteBlockPoolInfoByName["pool1"].MirroringToken)
	})

	t.Run("grpc error returns ErrNetwork", func(t *testing.T) {
		mockServer := &providerServer.MockOCSProviderServer{
			GetBlockPoolsInfoFunc: func(ctx context.Context, req *pb.BlockPoolsInfoRequest) (*pb.BlockPoolsInfoResponse, error) {
				return nil, status.Error(codes.Unavailable, "service unavailable")
			},
		}

		addr, cleanup := providerServer.StartMockGRPCServer(t, mockServer)
		defer cleanup()

		client, err := providerClient.NewProviderClient(ctx, addr, 10*time.Second)
		require.NoError(t, err)
		defer client.Close()

		scheme := newScheme(t)
		fc := buildFakeClient(scheme)
		r := newReconciler(fc, scheme)

		storageClusterPeer := &ocsv1.StorageClusterPeer{
			Status: ocsv1.StorageClusterPeerStatus{
				PeerInfo: &ocsv1.PeerInfo{
					StorageClusterUid: "test-uid",
				},
			},
		}

		cephBlockPoolsList := &rookCephv1.CephBlockPoolList{
			Items: []rookCephv1.CephBlockPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "pool1"}},
			},
		}

		remoteBlockPoolInfoByName := map[string]*pb.BlockPoolInfo{}

		err = r.getBlockPoolsInfo(client, storageClusterPeer, cephBlockPoolsList, remoteBlockPoolInfoByName)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrNetwork, "expected error to be ErrNetwork")
	})

	t.Run("partial failure returns regular error", func(t *testing.T) {
		mockServer := &providerServer.MockOCSProviderServer{
			GetBlockPoolsInfoFunc: func(ctx context.Context, req *pb.BlockPoolsInfoRequest) (*pb.BlockPoolsInfoResponse, error) {
				return &pb.BlockPoolsInfoResponse{
					BlockPoolsInfo: []*pb.BlockPoolInfo{
						{
							BlockPoolName:  "pool1",
							MirroringToken: "token1",
							BlockPoolID:    "id1",
						},
					},
					Errors: []*pb.BlockPoolInfoError{
						{
							BlockPoolName: "pool2",
							Message:       "pool not found",
						},
					},
				}, nil
			},
		}

		addr, cleanup := providerServer.StartMockGRPCServer(t, mockServer)
		defer cleanup()

		client, err := providerClient.NewProviderClient(ctx, addr, 10*time.Second)
		require.NoError(t, err)
		defer client.Close()

		scheme := newScheme(t)
		fc := buildFakeClient(scheme)
		r := newReconciler(fc, scheme)

		storageClusterPeer := &ocsv1.StorageClusterPeer{
			Status: ocsv1.StorageClusterPeerStatus{
				PeerInfo: &ocsv1.PeerInfo{
					StorageClusterUid: "test-uid",
				},
			},
		}

		cephBlockPoolsList := &rookCephv1.CephBlockPoolList{
			Items: []rookCephv1.CephBlockPool{
				{ObjectMeta: metav1.ObjectMeta{Name: "pool1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pool2"}},
			},
		}

		remoteBlockPoolInfoByName := map[string]*pb.BlockPoolInfo{}

		err = r.getBlockPoolsInfo(client, storageClusterPeer, cephBlockPoolsList, remoteBlockPoolInfoByName)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrNetwork, "expected error to NOT be ErrNetwork (partial failure)")
		assert.Len(t, remoteBlockPoolInfoByName, 1)
		assert.Equal(t, "pool1", remoteBlockPoolInfoByName["pool1"].BlockPoolName)
	})
}

func TestGetStorageClientsInfo(t *testing.T) {
	ctx := context.Background()

	t.Run("success with valid response", func(t *testing.T) {
		mockServer := &providerServer.MockOCSProviderServer{
			GetStorageClientsInfoFunc: func(ctx context.Context, req *pb.StorageClientsInfoRequest) (*pb.StorageClientsInfoResponse, error) {
				return &pb.StorageClientsInfoResponse{
					ClientsInfo: []*pb.ClientInfo{
						{
							ClientID:       "client1",
							RadosNamespace: "rns1",
						},
						{
							ClientID:       "client2",
							RadosNamespace: "rns2",
						},
					},
				}, nil
			},
		}

		addr, cleanup := providerServer.StartMockGRPCServer(t, mockServer)
		defer cleanup()

		client, err := providerClient.NewProviderClient(ctx, addr, 10*time.Second)
		require.NoError(t, err)
		defer client.Close()

		scheme := newScheme(t)
		fc := buildFakeClient(scheme)
		r := newReconciler(fc, scheme)

		storageClusterPeer := &ocsv1.StorageClusterPeer{
			Status: ocsv1.StorageClusterPeerStatus{
				PeerInfo: &ocsv1.PeerInfo{
					StorageClusterUid: "test-uid",
				},
			},
		}

		remoteClientIds := []string{"client1", "client2"}
		remoteClientInfoById := map[string]*pb.ClientInfo{}

		err = r.getStorageClientsInfo(client, storageClusterPeer, remoteClientIds, remoteClientInfoById)
		assert.NoError(t, err)
		assert.Len(t, remoteClientInfoById, 2)
		assert.Equal(t, "client1", remoteClientInfoById["client1"].ClientID)
		assert.Equal(t, "client2", remoteClientInfoById["client2"].ClientID)
		assert.Equal(t, "rns1", remoteClientInfoById["client1"].RadosNamespace)
	})

	t.Run("grpc error returns ErrNetwork", func(t *testing.T) {
		mockServer := &providerServer.MockOCSProviderServer{
			GetStorageClientsInfoFunc: func(ctx context.Context, req *pb.StorageClientsInfoRequest) (*pb.StorageClientsInfoResponse, error) {
				return nil, status.Error(codes.DeadlineExceeded, "deadline exceeded")
			},
		}

		addr, cleanup := providerServer.StartMockGRPCServer(t, mockServer)
		defer cleanup()

		client, err := providerClient.NewProviderClient(ctx, addr, 10*time.Second)
		require.NoError(t, err)
		defer client.Close()

		scheme := newScheme(t)
		fc := buildFakeClient(scheme)
		r := newReconciler(fc, scheme)

		storageClusterPeer := &ocsv1.StorageClusterPeer{
			Status: ocsv1.StorageClusterPeerStatus{
				PeerInfo: &ocsv1.PeerInfo{
					StorageClusterUid: "test-uid",
				},
			},
		}

		remoteClientIds := []string{"client1"}
		remoteClientInfoById := map[string]*pb.ClientInfo{}

		err = r.getStorageClientsInfo(client, storageClusterPeer, remoteClientIds, remoteClientInfoById)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrNetwork, "expected error to be ErrNetwork")
	})

	t.Run("partial failure returns regular error", func(t *testing.T) {
		mockServer := &providerServer.MockOCSProviderServer{
			GetStorageClientsInfoFunc: func(ctx context.Context, req *pb.StorageClientsInfoRequest) (*pb.StorageClientsInfoResponse, error) {
				return &pb.StorageClientsInfoResponse{
					ClientsInfo: []*pb.ClientInfo{
						{
							ClientID:       "client1",
							RadosNamespace: "rns1",
						},
					},
					Errors: []*pb.StorageClientInfoError{
						{
							ClientID: "client2",
							Message:  "client not found",
						},
					},
				}, nil
			},
		}

		addr, cleanup := providerServer.StartMockGRPCServer(t, mockServer)
		defer cleanup()

		client, err := providerClient.NewProviderClient(ctx, addr, 10*time.Second)
		require.NoError(t, err)
		defer client.Close()

		scheme := newScheme(t)
		fc := buildFakeClient(scheme)
		r := newReconciler(fc, scheme)

		storageClusterPeer := &ocsv1.StorageClusterPeer{
			Status: ocsv1.StorageClusterPeerStatus{
				PeerInfo: &ocsv1.PeerInfo{
					StorageClusterUid: "test-uid",
				},
			},
		}

		remoteClientIds := []string{"client1", "client2"}
		remoteClientInfoById := map[string]*pb.ClientInfo{}

		err = r.getStorageClientsInfo(client, storageClusterPeer, remoteClientIds, remoteClientInfoById)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrNetwork, "expected error to NOT be ErrNetwork (partial failure)")
		assert.Len(t, remoteClientInfoById, 1)
		assert.Equal(t, "client1", remoteClientInfoById["client1"].ClientID)
	})
}
