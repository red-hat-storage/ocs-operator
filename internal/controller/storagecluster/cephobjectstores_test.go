package storagecluster

import (
	"context"
	"encoding/base64"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/platform"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCephObjectStores(t *testing.T) {
	var cases = []struct {
		label                string
		createRuntimeObjects bool
		platform             configv1.PlatformType
	}{
		{
			label:                "Create CephObjectStore on non Cloud platform",
			createRuntimeObjects: false,
			platform:             configv1.BareMetalPlatformType,
		},
		{
			label:                "Do not create CephObjectStore on Cloud platform",
			createRuntimeObjects: false,
			platform:             configv1.AWSPlatformType,
		},
	}

	for _, c := range cases {
		platform.SetFakePlatformInstanceForTesting(true, c.platform)
		var objects []client.Object
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, objects, nil)
		if c.createRuntimeObjects {
			_ = createUpdateRuntimeObjects(t)
		}
		assertCephObjectStores(t, reconciler, cr, request)
		platform.UnsetFakePlatformInstanceForTesting()
	}
}

func assertCephObjectStores(t *testing.T, reconciler *StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	expectedCos, err := reconciler.newCephObjectStoreInstances(cr, nil, nil)
	assert.NoError(t, err)

	actualCos := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstore",
		},
	}
	request.Name = "ocsinit-cephobjectstore"
	err = reconciler.Get(context.TODO(), request.NamespacedName, actualCos)
	// for any cloud platform, 'cephobjectstore' should not be created
	// 'Get' should have thrown an error
	skip, skipErr := platform.PlatformsShouldSkipObjectStore()
	assert.NoError(t, skipErr)
	if skip {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		assert.Equal(t, expectedCos[0].Name, actualCos.Name)
		assert.Equal(t, expectedCos[0].Spec, actualCos.Spec)
		assert.Condition(
			t, func() bool { return expectedCos[0].Spec.Gateway.Instances == 1 },
			"there should be one 'Spec.Gateway.Instances' by default")
		assert.Equal(
			t, expectedCos[0].Spec.Gateway.Placement, GetPlacement(cr, "rgw"))
	}

	assert.Equal(t, len(expectedCos[0].OwnerReferences), 1)

	cr.Spec.ManagedResources.CephObjectStores.GatewayInstances = 2
	expectedCos, _ = reconciler.newCephObjectStoreInstances(cr, nil, nil)
	assert.Equal(t, expectedCos[0].Spec.Gateway.Instances, int32(2))
}

func TestCephObjectStoreSSES3WithVaultAgent(t *testing.T) {
	platform.SetFakePlatformInstanceForTesting(true, configv1.BareMetalPlatformType)
	defer platform.UnsetFakePlatformInstanceForTesting()

	var objects []client.Object
	t, reconciler, cr, _ := initStorageClusterResourceCreateUpdateTest(t, objects, nil)

	t.Run("SSE-S3 is configured when VAULT_RGW_AUTH_METHOD is agent", func(t *testing.T) {
		reconciler.images.VaultAgent = "vault-agent:test"
		kmsConfigMap := &corev1.ConfigMap{
			Data: map[string]string{
				"KMS_PROVIDER":          "vault",
				"VAULT_ADDR":            "https://vault.example.com:8200",
				"VAULT_AUTH_METHOD":     "token",
				"VAULT_RGW_AUTH_METHOD": "agent",
			},
		}
		cephObjectStores, err := reconciler.newCephObjectStoreInstances(cr, kmsConfigMap, nil)
		assert.NoError(t, err)
		assert.NotNil(t, cephObjectStores[0].Spec.Security)
		s3 := cephObjectStores[0].Spec.Security.ServerSideEncryptionS3
		assert.Equal(t, "vault", s3.ConnectionDetails["KMS_PROVIDER"])
		// VAULT_ADDR should point to the ODF-managed Vault Agent service
		assert.Equal(t, VaultAgentServiceURL(cr.Namespace), s3.ConnectionDetails["VAULT_ADDR"])
		assert.Equal(t, "agent", s3.ConnectionDetails["VAULT_AUTH_METHOD"])
		assert.Equal(t, "transit", s3.ConnectionDetails["VAULT_SECRET_ENGINE"])
		assert.Empty(t, s3.TokenSecretName)
	})

	t.Run("SSE-S3 is skipped when VAULT_AGENT_IMAGE is empty", func(t *testing.T) {
		reconciler.images.VaultAgent = ""
		kmsConfigMap := &corev1.ConfigMap{
			Data: map[string]string{
				"KMS_PROVIDER":          "vault",
				"VAULT_ADDR":            "https://vault.example.com:8200",
				"VAULT_RGW_AUTH_METHOD": "agent",
			},
		}
		cephObjectStores, err := reconciler.newCephObjectStoreInstances(cr, kmsConfigMap, nil)
		assert.NoError(t, err)
		assert.Nil(t, cephObjectStores[0].Spec.Security)
	})

	t.Run("SSE-KMS is configured when VAULT_RGW_AUTH_METHOD is token", func(t *testing.T) {
		kmsConfigMap := &corev1.ConfigMap{
			Data: map[string]string{
				"KMS_PROVIDER":          "vault",
				"VAULT_ADDR":            "https://vault.example.com:8200",
				"VAULT_RGW_AUTH_METHOD": "token",
			},
		}
		cephObjectStores, err := reconciler.newCephObjectStoreInstances(cr, kmsConfigMap, nil)
		assert.NoError(t, err)
		assert.NotNil(t, cephObjectStores[0].Spec.Security)
		// SSE-KMS should be configured
		kms := cephObjectStores[0].Spec.Security.KeyManagementService
		assert.Equal(t, "https://vault.example.com:8200", kms.ConnectionDetails["VAULT_ADDR"])
		assert.Equal(t, KMSTokenSecretName, kms.TokenSecretName)
		// SSE-S3 should not be configured
		assert.Empty(t, cephObjectStores[0].Spec.Security.ServerSideEncryptionS3.ConnectionDetails)
	})

	t.Run("No RGW encryption when VAULT_RGW_AUTH_METHOD is absent", func(t *testing.T) {
		kmsConfigMap := &corev1.ConfigMap{
			Data: map[string]string{
				"KMS_PROVIDER": "vault",
				"VAULT_ADDR":   "https://vault.example.com:8200",
			},
		}
		cephObjectStores, err := reconciler.newCephObjectStoreInstances(cr, kmsConfigMap, nil)
		assert.NoError(t, err)
		// Security is always set (for DEFAULT TLS groups), but KMS fields must be empty
		assert.NotNil(t, cephObjectStores[0].Spec.Security)
		assert.Empty(t, cephObjectStores[0].Spec.Security.KeyManagementService.ConnectionDetails)
		assert.Empty(t, cephObjectStores[0].Spec.Security.ServerSideEncryptionS3.ConnectionDetails)
	})

	t.Run("Error when VAULT_RGW_AUTH_METHOD is invalid", func(t *testing.T) {
		kmsConfigMap := &corev1.ConfigMap{
			Data: map[string]string{
				"KMS_PROVIDER":          "vault",
				"VAULT_ADDR":            "https://vault.example.com:8200",
				"VAULT_RGW_AUTH_METHOD": "kubernetes",
			},
		}
		_, err := reconciler.newCephObjectStoreInstances(cr, kmsConfigMap, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "kubernetes")
	})

	t.Run("No RGW encryption when KMS ConfigMap is nil", func(t *testing.T) {
		cephObjectStores, err := reconciler.newCephObjectStoreInstances(cr, nil, nil)
		assert.NoError(t, err)
		// Security is always set (for DEFAULT TLS groups), but KMS fields must be empty
		assert.NotNil(t, cephObjectStores[0].Spec.Security)
		assert.Empty(t, cephObjectStores[0].Spec.Security.KeyManagementService.ConnectionDetails)
		assert.Empty(t, cephObjectStores[0].Spec.Security.ServerSideEncryptionS3.ConnectionDetails)
	})

}

func TestGetCephObjectStoreGatewayInstances(t *testing.T) {
	var cases = []struct {
		label                                   string
		sc                                      *api.StorageCluster
		expectedCephObjectStoreGatewayInstances int
	}{
		{
			label:                                   "Default case",
			sc:                                      &api.StorageCluster{},
			expectedCephObjectStoreGatewayInstances: defaults.CephObjectStoreGatewayInstances,
		},
		{
			label: "CephObjectStoreGatewayInstances is set on the StorageCluster CR Spec",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephObjectStores: api.ManageCephObjectStores{
							GatewayInstances: 2,
						},
					},
				},
			},
			expectedCephObjectStoreGatewayInstances: 2,
		},
		{
			label: "Arbiter Mode",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					Arbiter: api.ArbiterSpec{
						Enable: true,
					},
				},
			},
			expectedCephObjectStoreGatewayInstances: defaults.ArbiterCephObjectStoreGatewayInstances,
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		actualCephObjectStoreGatewayInstances := getCephObjectStoreGatewayInstances(c.sc)
		assert.Equal(t, c.expectedCephObjectStoreGatewayInstances, actualCephObjectStoreGatewayInstances)
	}
}

func TestSetSTSOptions(t *testing.T) {
	testCases := []struct {
		name            string
		enableSTS       bool
		expectRgwConfig bool
		expectSecret    bool
		expectSecretRef bool
	}{
		{
			name:            "STS enabled - should configure all options",
			enableSTS:       true,
			expectRgwConfig: true,
			expectSecret:    true,
			expectSecretRef: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test environment
			var objects []runtime.Object
			sc := &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-storagecluster",
					Namespace: "test-namespace",
				},
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephObjectStores: api.ManageCephObjectStores{
							EnableSTS: tc.enableSTS,
						},
					},
				},
			}
			objects = append(objects, sc)

			reconciler := createFakeStorageClusterReconciler(t, objects...)

			// Create a CephObjectStore instance
			cos := &cephv1.CephObjectStore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-objectstore",
					Namespace: sc.Namespace,
				},
				Spec: cephv1.ObjectStoreSpec{
					Gateway: cephv1.GatewaySpec{},
				},
			}

			if tc.enableSTS {
				// Call setSTSOptions
				err := reconciler.setSTSOptions(cos, sc)
				assert.NoError(t, err)

				// Verify rgwConfig is set
				if tc.expectRgwConfig {
					assert.NotNil(t, cos.Spec.Gateway.RgwConfig)
					assert.Equal(t, "true", cos.Spec.Gateway.RgwConfig["rgw_s3_auth_use_sts"])
				}

				// Verify secret was created
				if tc.expectSecret {
					secretName := "sts-key-test-objectstore"
					secret := &corev1.Secret{}
					err := reconciler.Get(context.TODO(), types.NamespacedName{
						Name:      secretName,
						Namespace: sc.Namespace,
					}, secret)
					assert.NoError(t, err)
					assert.NotNil(t, secret)
					assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)

					// Verify secret contains the STS key
					stsKey, exists := secret.Data["rgw_sts_key"]
					assert.True(t, exists)
					assert.NotEmpty(t, stsKey)

					// Verify the key is valid base64
					_, err = base64.StdEncoding.DecodeString(string(stsKey))
					assert.NoError(t, err)

					// Verify owner reference is set
					assert.Equal(t, 1, len(secret.OwnerReferences))
					assert.Equal(t, sc.Name, secret.OwnerReferences[0].Name)
				}

				// Verify RgwConfigFromSecret is set
				if tc.expectSecretRef {
					assert.NotNil(t, cos.Spec.Gateway.RgwConfigFromSecret)
					secretSelector, exists := cos.Spec.Gateway.RgwConfigFromSecret["rgw_sts_key"]
					assert.True(t, exists)
					assert.Equal(t, "sts-key-test-objectstore", secretSelector.Name)
					assert.Equal(t, "rgw_sts_key", secretSelector.Key)
				}
			}
		})
	}
}

func TestSetSTSOptionsIdempotency(t *testing.T) {
	// Setup test environment
	var objects []runtime.Object
	sc := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storagecluster",
			Namespace: "test-namespace",
		},
		Spec: api.StorageClusterSpec{
			ManagedResources: api.ManagedResourcesSpec{
				CephObjectStores: api.ManageCephObjectStores{
					EnableSTS: true,
				},
			},
		},
	}
	objects = append(objects, sc)

	reconciler := createFakeStorageClusterReconciler(t, objects...)

	cos := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-objectstore",
			Namespace: sc.Namespace,
		},
		Spec: cephv1.ObjectStoreSpec{
			Gateway: cephv1.GatewaySpec{},
		},
	}

	// Call setSTSOptions first time
	err := reconciler.setSTSOptions(cos, sc)
	assert.NoError(t, err)

	// Get the secret created
	secretName := "sts-key-test-objectstore"
	secret1 := &corev1.Secret{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{
		Name:      secretName,
		Namespace: sc.Namespace,
	}, secret1)
	assert.NoError(t, err)
	originalKey := string(secret1.Data["rgw_sts_key"])

	// Call setSTSOptions second time (should be idempotent)
	err = reconciler.setSTSOptions(cos, sc)
	assert.NoError(t, err)

	// Verify secret still exists and key hasn't changed
	secret2 := &corev1.Secret{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{
		Name:      secretName,
		Namespace: sc.Namespace,
	}, secret2)
	assert.NoError(t, err)
	currentKey := string(secret2.Data["rgw_sts_key"])

	// The key should remain the same (idempotent behavior)
	assert.Equal(t, originalKey, currentKey, "Secret key should not change on subsequent calls")
}

func TestNewCephObjectStoreInstancesWithSTS(t *testing.T) {
	platform.SetFakePlatformInstanceForTesting(true, configv1.BareMetalPlatformType)
	defer platform.UnsetFakePlatformInstanceForTesting()

	var objects []runtime.Object
	sc := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storagecluster",
			Namespace: "test-namespace",
		},
		Spec: api.StorageClusterSpec{
			ManagedResources: api.ManagedResourcesSpec{
				CephObjectStores: api.ManageCephObjectStores{
					EnableSTS: true,
				},
			},
		},
	}
	objects = append(objects, sc)

	reconciler := createFakeStorageClusterReconciler(t, objects...)

	// Create CephObjectStore instances
	cephObjectStores, err := reconciler.newCephObjectStoreInstances(sc, nil, nil)
	assert.NoError(t, err)
	assert.NotEmpty(t, cephObjectStores)

	// Verify STS configuration is applied
	cos := cephObjectStores[0]
	assert.NotNil(t, cos.Spec.Gateway.RgwConfig)
	assert.Equal(t, "true", cos.Spec.Gateway.RgwConfig["rgw_s3_auth_use_sts"])

	assert.NotNil(t, cos.Spec.Gateway.RgwConfigFromSecret)
	secretSelector, exists := cos.Spec.Gateway.RgwConfigFromSecret["rgw_sts_key"]
	assert.True(t, exists)
	assert.Contains(t, secretSelector.Name, "sts-key-")
	assert.Equal(t, "rgw_sts_key", secretSelector.Key)

	// Verify the secret was created
	secretName := secretSelector.Name
	secret := &corev1.Secret{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{
		Name:      secretName,
		Namespace: sc.Namespace,
	}, secret)
	assert.NoError(t, err)
	assert.NotEmpty(t, secret.Data["rgw_sts_key"])
}

func makeTLSProfile(selector ocstlsv1.Selector, version ocstlsv1.TLSProtocolVersion, ciphers []ocstlsv1.TLSCipherSuite, groups []ocstlsv1.TLSGroupName) *ocstlsv1.TLSProfile {
	return &ocstlsv1.TLSProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.TLSProfileName,
			Namespace: "test-ns",
		},
		Spec: ocstlsv1.TLSProfileSpec{
			Rules: []ocstlsv1.TLSProfileRules{
				{
					Selectors: []ocstlsv1.Selector{selector},
					Config: ocstlsv1.TLSConfig{
						Version: version,
						Ciphers: ciphers,
						Groups:  groups,
					},
				},
			},
		},
	}
}

func TestRGWTLSConfig(t *testing.T) {
	platform.SetFakePlatformInstanceForTesting(true, configv1.BareMetalPlatformType)
	defer platform.UnsetFakePlatformInstanceForTesting()

	reconciler := createFakeStorageClusterReconciler(t)
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "ocsinit", Namespace: "test-ns"},
	}

	t.Run("TLS 1.2 profile with rook.io selector sets ciphers and groups", func(t *testing.T) {
		profile := makeTLSProfile(
			"rook.io",
			ocstlsv1.VersionTLS1_2,
			[]ocstlsv1.TLSCipherSuite{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"},
			[]ocstlsv1.TLSGroupName{"secp256r1", "secp384r1"},
		)
		stores, err := reconciler.newCephObjectStoreInstances(cr, nil, profile)
		assert.NoError(t, err)
		assert.NotNil(t, stores[0].Spec.Security)
		assert.Contains(t, stores[0].Spec.Security.Ciphers, "ECDHE-RSA-AES128-GCM-SHA256")
		assert.Contains(t, stores[0].Spec.Security.Ciphers, "ECDHE-RSA-AES256-GCM-SHA384")
		assert.Contains(t, stores[0].Spec.Security.TlsGroups, "prime256v1")
		assert.Contains(t, stores[0].Spec.Security.TlsGroups, "secp384r1")
		// TLS 1.2 must not set SSLv2 option
		assert.Nil(t, stores[0].Spec.Security.SslOptions)
	})

	t.Run("TLS 1.3 profile with rook.io selector disables SSLv2", func(t *testing.T) {
		profile := makeTLSProfile(
			"rook.io",
			ocstlsv1.VersionTLS1_3,
			[]ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
			[]ocstlsv1.TLSGroupName{"X25519"},
		)
		stores, err := reconciler.newCephObjectStoreInstances(cr, nil, profile)
		assert.NoError(t, err)
		assert.NotNil(t, stores[0].Spec.Security)
		assert.Contains(t, stores[0].Spec.Security.TlsGroups, "x25519")
		assert.NotNil(t, stores[0].Spec.Security.SslOptions)
		assert.Equal(t, ptr.To(false), stores[0].Spec.Security.SslOptions.SSLv2)
	})

	t.Run("Profile with non-matching selector falls back to DEFAULT groups", func(t *testing.T) {
		profile := makeTLSProfile(
			"noobaa.io",
			ocstlsv1.VersionTLS1_2,
			[]ocstlsv1.TLSCipherSuite{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
			[]ocstlsv1.TLSGroupName{"secp256r1"},
		)
		stores, err := reconciler.newCephObjectStoreInstances(cr, nil, profile)
		assert.NoError(t, err)
		assert.NotNil(t, stores[0].Spec.Security)
		assert.Equal(t, []string{"DEFAULT"}, stores[0].Spec.Security.TlsGroups)
		assert.Nil(t, stores[0].Spec.Security.SslOptions)
	})

	t.Run("Nil TLS profile sets DEFAULT groups and clears ciphers", func(t *testing.T) {
		stores, err := reconciler.newCephObjectStoreInstances(cr, nil, nil)
		assert.NoError(t, err)
		assert.NotNil(t, stores[0].Spec.Security)
		assert.Nil(t, stores[0].Spec.Security.Ciphers)
		assert.Equal(t, []string{"DEFAULT"}, stores[0].Spec.Security.TlsGroups)
		assert.Nil(t, stores[0].Spec.Security.SslOptions)
	})

	t.Run("Wildcard selector configures RGW", func(t *testing.T) {
		// "*" has lowest specificity but still matches rook.io
		profile := makeTLSProfile(
			"*",
			ocstlsv1.VersionTLS1_2,
			[]ocstlsv1.TLSCipherSuite{"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"},
			[]ocstlsv1.TLSGroupName{"secp384r1"},
		)
		stores, err := reconciler.newCephObjectStoreInstances(cr, nil, profile)
		assert.NoError(t, err)
		assert.NotNil(t, stores[0].Spec.Security)
		assert.Contains(t, stores[0].Spec.Security.Ciphers, "ECDHE-RSA-AES256-GCM-SHA384")
		assert.Contains(t, stores[0].Spec.Security.TlsGroups, "secp384r1")
	})
}
