package storagecluster

import (
	"context"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestSyncExternalConsumersStorageClasses(t *testing.T) {
	scheme := createFakeScheme(t)
	namespace := "openshift-storage"

	storageCluster := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: namespace,
		},
	}

	nvmeofSCName := util.GenerateNameForNVMeOFStorageClass(storageCluster)

	tests := []struct {
		name             string
		nvmeofEnabled    bool
		consumers        []*ocsv1a1.StorageConsumer
		wantNVMeOFInSpec map[string]bool
	}{
		{
			name:          "NVMe-oF enabled adds SC to external consumer without it",
			nvmeofEnabled: true,
			consumers: []*ocsv1a1.StorageConsumer{
				{
					ObjectMeta: metav1.ObjectMeta{Name: defaults.LocalStorageConsumerName, Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: nvmeofSCName}},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "external-consumer-1", Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-cephfs"}},
						},
					},
				},
			},
			wantNVMeOFInSpec: map[string]bool{
				defaults.LocalStorageConsumerName: true,
				"external-consumer-1":            true,
			},
		},
		{
			name:          "NVMe-oF enabled does not duplicate SC if already present",
			nvmeofEnabled: true,
			consumers: []*ocsv1a1.StorageConsumer{
				{
					ObjectMeta: metav1.ObjectMeta{Name: defaults.LocalStorageConsumerName, Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: nvmeofSCName}},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "external-consumer-1", Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: nvmeofSCName}},
						},
					},
				},
			},
			wantNVMeOFInSpec: map[string]bool{
				defaults.LocalStorageConsumerName: true,
				"external-consumer-1":            true,
			},
		},
		{
			name:          "NVMe-oF disabled removes SC from external consumer",
			nvmeofEnabled: false,
			consumers: []*ocsv1a1.StorageConsumer{
				{
					ObjectMeta: metav1.ObjectMeta{Name: defaults.LocalStorageConsumerName, Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "external-consumer-1", Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: nvmeofSCName}},
						},
					},
				},
			},
			wantNVMeOFInSpec: map[string]bool{
				// internal consumer is skipped by syncExternalConsumersStorageClasses
				defaults.LocalStorageConsumerName: false,
				"external-consumer-1":            false,
			},
		},
		{
			name:          "NVMe-oF enabled adds SC to multiple external consumers",
			nvmeofEnabled: true,
			consumers: []*ocsv1a1.StorageConsumer{
				{
					ObjectMeta: metav1.ObjectMeta{Name: defaults.LocalStorageConsumerName, Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ext-consumer-a", Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ext-consumer-b", Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-cephfs"}},
						},
					},
				},
			},
			wantNVMeOFInSpec: map[string]bool{
				defaults.LocalStorageConsumerName: false,
				"ext-consumer-a":                 true,
				"ext-consumer-b":                 true,
			},
		},
		{
			name:          "skips non-enabled external consumers",
			nvmeofEnabled: true,
			consumers: []*ocsv1a1.StorageConsumer{
				{
					ObjectMeta: metav1.ObjectMeta{Name: defaults.LocalStorageConsumerName, Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: true,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "not-enabled-consumer", Namespace: namespace},
					Spec: ocsv1a1.StorageConsumerSpec{
						Enable: false,
						StorageClasses: []ocsv1a1.StorageClassSpec{
							{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ocs-storagecluster-ceph-rbd"}},
						},
					},
				},
			},
			wantNVMeOFInSpec: map[string]bool{
				defaults.LocalStorageConsumerName: false,
				"not-enabled-consumer":           false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := storageCluster.DeepCopy()
			if tt.nvmeofEnabled {
				sc.Spec.NVMeOF = &api.NVMeOFSpec{
					Enable:           true,
					GatewayInstances: 2,
				}
			}

			var runtimeObjs []runtime.Object
			for _, c := range tt.consumers {
				runtimeObjs = append(runtimeObjs, c.DeepCopy())
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(runtimeObjs...).
				Build()

			r := &StorageClusterReconciler{
				Client: fakeClient,
				Scheme: scheme,
				Log:    logf.Log.WithName("test_sync_external_consumers"),
				ctx:    context.TODO(),
			}

			err := syncExternalConsumersStorageClasses(r, sc)
			assert.NoError(t, err)

			for consumerName, wantNVMeOF := range tt.wantNVMeOFInSpec {
				consumer := &ocsv1a1.StorageConsumer{}
				err := fakeClient.Get(context.TODO(), client.ObjectKey{
					Name:      consumerName,
					Namespace: namespace,
				}, consumer)
				assert.NoError(t, err, "failed to get consumer %s", consumerName)

				hasNVMeOF := false
				for _, sc := range consumer.Spec.StorageClasses {
					if sc.Name == nvmeofSCName {
						hasNVMeOF = true
						break
					}
				}
				assert.Equal(t, wantNVMeOF, hasNVMeOF,
					"consumer %s: expected NVMe-oF SC present=%v, got=%v, StorageClasses=%v",
					consumerName, wantNVMeOF, hasNVMeOF, consumer.Spec.StorageClasses)

				if hasNVMeOF {
					nameCount := 0
					for _, sc := range consumer.Spec.StorageClasses {
						if sc.Name == nvmeofSCName {
							nameCount++
						}
					}
					assert.Equal(t, 1, nameCount,
						"consumer %s: expected exactly 1 NVMe-oF SC entry, got %d",
						consumerName, nameCount)
				}
			}
		})
	}
}
