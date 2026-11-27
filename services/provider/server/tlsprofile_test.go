package server

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetTLSConfigAppliesMatchingProfile(t *testing.T) {
	scheme, err := newScheme()
	require.NoError(t, err)
	profile := &ocstlsv1.TLSProfile{
		ObjectMeta: metav1.ObjectMeta{Name: defaults.TLSProfileName, Namespace: testNamespace},
		Spec: ocstlsv1.TLSProfileSpec{Rules: []ocstlsv1.TLSProfileRules{{
			Selectors: []ocstlsv1.Selector{"ocs.openshift.io/provider-server"},
			Config: ocstlsv1.TLSConfig{
				Version: ocstlsv1.VersionTLS1_3,
				Ciphers: []ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256"},
				Groups:  []ocstlsv1.TLSGroupName{"X25519MLKEM768"},
			},
		}}},
	}
	s := &OCSProviderServer{
		client:    fake.NewClientBuilder().WithScheme(scheme).WithObjects(profile).Build(),
		namespace: testNamespace,
	}

	config := s.getTLSConfig(context.Background())
	assert.Equal(t, uint16(tls.VersionTLS13), config.MinVersion)
	assert.Equal(t, uint16(tls.VersionTLS13), config.MaxVersion)
	assert.Equal(t, []tls.CurveID{tls.X25519MLKEM768}, config.CurvePreferences)
}

func TestGetTLSConfigUsesDefaultsWithoutMatch(t *testing.T) {
	scheme, err := newScheme()
	require.NoError(t, err)

	for _, profile := range []*ocstlsv1.TLSProfile{
		nil,
		{
			ObjectMeta: metav1.ObjectMeta{Name: defaults.TLSProfileName, Namespace: testNamespace},
			Spec: ocstlsv1.TLSProfileSpec{Rules: []ocstlsv1.TLSProfileRules{{
				Selectors: []ocstlsv1.Selector{"other.io"},
				Config:    ocstlsv1.TLSConfig{Version: ocstlsv1.VersionTLS1_3},
			}}},
		},
	} {
		builder := fake.NewClientBuilder().WithScheme(scheme)
		if profile != nil {
			builder.WithObjects(profile)
		}
		s := &OCSProviderServer{client: builder.Build(), namespace: testNamespace}

		config := s.getTLSConfig(context.Background())
		assert.Equal(t, uint16(tls.VersionTLS12), config.MinVersion)
		assert.Zero(t, config.MaxVersion)
	}
}
