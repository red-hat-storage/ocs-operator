package storagecluster

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	ini "gopkg.in/ini.v1"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestDualStack(t *testing.T) {

	testTable := []struct {
		label string
		// the networkspec in the storagecluster
		rookCephNetworkSpec rookCephv1.NetworkSpec
		// status of the configv1.Network object for the cluster
		networkStatus configv1.NetworkStatus
		// whether to expect public network in the configmap
		expectPublicNetwork        bool
		expectedPublicNetworkValue string
	}{
		{
			label: "Case #1: DualStack Enabled with both IPV4 CIDR and No network provider",
			rookCephNetworkSpec: rookCephv1.NetworkSpec{
				DualStack: true,
			},
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "191118.12.3.4/16"},
					{CIDR: "123.45676.12.1/12"},
				},
			},
			expectPublicNetwork:        true,
			expectedPublicNetworkValue: "191118.12.3.4/16,123.45676.12.1/12",
		},
		{
			label: "Case #2: DualStack Enabled with IPV4 and IPV6 CIDR and No network provider",
			rookCephNetworkSpec: rookCephv1.NetworkSpec{
				DualStack: true,
			},
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "191118.12.3.4/17"},
					{CIDR: "fd01::/48"},
				},
			},
			expectPublicNetwork:        true,
			expectedPublicNetworkValue: "191118.12.3.4/17,fd01::/48",
		},
		{
			label: "Case #3: DualStack Disabled and No network provider ",
			rookCephNetworkSpec: rookCephv1.NetworkSpec{
				DualStack: false,
			},
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "198.1v2.3.4/16"},
					{CIDR: "123.45676.12.1/12"},
				},
			},
			expectPublicNetwork: false,
		},
		{
			label: "Case #4: DualStack Enabled and network provider is multus",
			rookCephNetworkSpec: rookCephv1.NetworkSpec{
				DualStack: true,
				Provider:  "multus",
			},
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "198.1v2.3.4/16"},
					{CIDR: "123.45676.12.1/12"},
				},
			},
			expectPublicNetwork: false,
		},
		{
			label: "Case #5: DualStack Enabled with some other network provider",
			rookCephNetworkSpec: rookCephv1.NetworkSpec{
				DualStack: true,
				Provider:  "xyz",
			},
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "198.1v2.3.4/16"},
					{CIDR: "123.45676.12.1/12"},
				},
			},
			expectPublicNetwork: false,
		},
		{
			label: "Case #6: DualStack Disabled and network provider is multus",
			rookCephNetworkSpec: rookCephv1.NetworkSpec{
				DualStack: false,
				Provider:  "multus",
			},
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "198.1v2.3.4/16"},
					{CIDR: "123.45676.12.1/12"},
				},
			},
			expectPublicNetwork: false,
		},
	}

	for i, testCase := range testTable {
		t.Logf("Case #%+v", i+1)
		// setup the mocks

		networkConfig := &configv1.Network{

			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster",
				Namespace: "",
			},
			Status: testCase.networkStatus,
		}
		r := createFakeStorageClusterReconciler(t, networkConfig)
		configMap := &corev1.ConfigMap{}

		sc := &api.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
			Spec:       api.StorageClusterSpec{Network: &testCase.rookCephNetworkSpec},
		}
		cephConfigReconciler := &ocsCephConfig{}
		_, err := cephConfigReconciler.ensureCreated(&r, sc)
		if err != nil {
			assert.NilError(t, err, "ensure created failed")
		}
		// get the output
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: rookConfigMapName, Namespace: "test"}, configMap)
		assert.NilError(t, err, "expected to find configmap %q: %+v", rookConfigMapName, err)

		// compare with the expected results
		cfg, err := ini.Load([]byte(configMap.Data["config"]))
		assert.NilError(t, err, "expected ini string to load")

		sect, err := cfg.GetSection(globalSectionKey)
		assert.NilError(t, err, "expected section to exist")

		// check if "public_network" key exists
		keyFound := sect.HasKey(publicNetworkKey)
		assert.Equal(t, keyFound, testCase.expectPublicNetwork)

		// check that the keys are the same
		if testCase.expectPublicNetwork && keyFound {
			assert.Equal(t, sect.Key(publicNetworkKey).Value(), testCase.expectedPublicNetworkValue, "expected public network value to match")
		}

	}

}
