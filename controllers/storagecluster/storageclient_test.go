package storagecluster

import (
	"testing"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
)

func Test_getCephNetworkAnnotationValue(t *testing.T) {
	makeMultusSpec := func(public, cluster string) *rookCephv1.NetworkSpec {
		s := &rookCephv1.NetworkSpec{
			Provider: rookCephv1.NetworkProviderMultus,
		}
		s.Selectors = map[rookCephv1.CephNetworkType]string{}
		if public != "" {
			s.Selectors[rookCephv1.CephNetworkPublic] = public
		}
		if cluster != "" {
			s.Selectors[rookCephv1.CephNetworkCluster] = cluster
		}
		return s
	}

	tests := []struct {
		name            string
		cephNetworkSpec *rookCephv1.NetworkSpec
		scNamespace     string
		want            string
		wantErr         bool
	}{
		{"nil network spec", nil, "ns", "", false},
		{"not multus", &rookCephv1.NetworkSpec{IPFamily: rookCephv1.IPv4}, "ns", "", false},
		{"multus no selectors", makeMultusSpec("", ""), "ns", "", true},
		{"multus public", makeMultusSpec("odf-public", ""), "ns", `[{"name":"odf-public","namespace":"ns"}]`, false},
		{"multus cluster", makeMultusSpec("", "odf-cluster"), "ns", "", false},
		{"multus public and cluster", makeMultusSpec("odf-public", "odf-cluster"), "other-ns", `[{"name":"odf-public","namespace":"other-ns"}]`, false},
		{"multus public NAD in default ns", makeMultusSpec("default/odf-public", ""), "ns", `[{"name":"odf-public","namespace":"default"}]`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getCephNetworkAnnotationValue(tt.cephNetworkSpec, tt.scNamespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCephNetworkAnnotationValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCephNetworkAnnotationValue() = %v, want %v", got, tt.want)
			}
		})
	}
}
