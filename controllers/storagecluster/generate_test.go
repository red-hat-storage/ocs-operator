package storagecluster

import (
	"reflect"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCephFileSystemProviderParameters(t *testing.T) {
	testTable := []struct {
		label               string
		storageCluster      *api.StorageCluster
		expectedConfigValue map[string]string
		expectError         bool
	}{
		{
			label: "Case #1 AllowRemoteStorageConsumers is enabled",
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-test",
					Namespace: "test",
				},
				Spec: api.StorageClusterSpec{
					AllowRemoteStorageConsumers: true,
					StorageDeviceSets: []api.StorageDeviceSet{
						{
							Name:  "default",
							Count: 1,
						},
					},
				},
			},
			expectedConfigValue: map[string]string{
				"pg_autoscale_mode": "off",
				"pg_num":            "256",
				"pgp_num":           "256",
			},
		},
		{
			label: "Case #2 Diferrent device set Name",
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-test",
					Namespace: "test",
				},
				Spec: api.StorageClusterSpec{
					AllowRemoteStorageConsumers: true,
					StorageDeviceSets: []api.StorageDeviceSet{
						{
							Name:  "non-default",
							Count: 1,
						},
					},
				},
			},
			expectedConfigValue: nil,
			expectError:         true,
		},
		{
			label: "Case #3 Device Count is 2",
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-test",
					Namespace: "test",
				},
				Spec: api.StorageClusterSpec{
					AllowRemoteStorageConsumers: true,
					StorageDeviceSets: []api.StorageDeviceSet{
						{
							Name:  "default",
							Count: 2,
						},
					},
				},
			},
			expectedConfigValue: map[string]string{
				"pg_autoscale_mode": "off",
				"pg_num":            "512",
				"pgp_num":           "512",
			},
		},
		{
			label: "Case #4 Device Count is 5",
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-test",
					Namespace: "test",
				},
				Spec: api.StorageClusterSpec{
					AllowRemoteStorageConsumers: true,
					StorageDeviceSets: []api.StorageDeviceSet{
						{
							Name:  "default",
							Count: 5,
						},
					},
				},
			},
			expectedConfigValue: map[string]string{
				"pg_autoscale_mode": "off",
				"pg_num":            "512",
				"pgp_num":           "512",
			},
		},
	}

	for i, testCase := range testTable {
		t.Logf("Case #%+v", i+1)
		got, err := generateCephFSProviderParameters(testCase.storageCluster)
		if err != nil && !testCase.expectError {
			t.Errorf(err.Error())
		}
		eq := reflect.DeepEqual(got, testCase.expectedConfigValue)
		if !eq {
			t.Errorf("Wrong config values. Got %v want %v", got, testCase.expectedConfigValue)
		}
	}

}
