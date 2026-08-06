package tlsprofile

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const testNamespace = "openshift-storage"

func TestLoad(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocstlsv1.AddToScheme(scheme))
	profile := &ocstlsv1.TLSProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "ocs-tls-profile", Namespace: testNamespace},
		Spec: ocstlsv1.TLSProfileSpec{Rules: []ocstlsv1.TLSProfileRules{{
			Selectors: []ocstlsv1.Selector{"ocs.openshift.io/metrics-exporter"},
			Config: ocstlsv1.TLSConfig{
				Version: ocstlsv1.VersionTLS1_3,
				Ciphers: []ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256"},
				Groups:  []ocstlsv1.TLSGroupName{"X25519MLKEM768"},
			},
		}}},
	}

	config, err := load(context.Background(), fake.NewClientBuilder().WithScheme(scheme).WithObjects(profile).Build(), testNamespace)
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS13), config.MinVersion)
	assert.Equal(t, uint16(tls.VersionTLS13), config.MaxVersion)
	assert.Equal(t, []tls.CurveID{tls.X25519MLKEM768}, config.CurvePreferences)
}

func TestLoadUsesDefaultsWithoutMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocstlsv1.AddToScheme(scheme))

	for _, profile := range []*ocstlsv1.TLSProfile{
		nil,
		{
			ObjectMeta: metav1.ObjectMeta{Name: "ocs-tls-profile", Namespace: testNamespace},
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
		config, err := load(context.Background(), builder.Build(), testNamespace)
		require.NoError(t, err)
		assert.Equal(t, uint16(tls.VersionTLS12), config.MinVersion)
		assert.Zero(t, config.MaxVersion)
	}
}

func TestLoadReturnsClientError(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocstlsv1.AddToScheme(scheme))
	want := errors.New("forbidden")
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return want
		},
	}).Build()

	_, err := load(context.Background(), kubeClient, testNamespace)
	assert.ErrorIs(t, err, want)
}
