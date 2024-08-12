package util

import (
	"reflect"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
)

func Test_getReadAffinityOptions(t *testing.T) {
	type args struct {
		sc *ocsv1.StorageCluster
	}
	tests := []struct {
		name string
		args args
		want rookCephv1.ReadAffinitySpec
	}{
		{
			name: "Internal ceph cluster: ReadAffinity enabled by default",
			args: args{
				sc: &ocsv1.StorageCluster{},
			},
			want: rookCephv1.ReadAffinitySpec{
				Enabled: true,
			},
		}, {
			name: "Internal ceph cluster: ReadAffinity enabled by user with crushlocationlabels",
			args: args{
				sc: &ocsv1.StorageCluster{
					Spec: ocsv1.StorageClusterSpec{
						CSI: &ocsv1.CSIDriverSpec{
							ReadAffinity: &rookCephv1.ReadAffinitySpec{
								Enabled:             true,
								CrushLocationLabels: []string{"topology.io/zone"},
							},
						},
					},
				},
			},
			want: rookCephv1.ReadAffinitySpec{
				Enabled:             true,
				CrushLocationLabels: []string{"topology.io/zone"},
			},
		},
		{
			name: "External ceph cluster: ReadAffinity disabled by default",
			args: args{
				sc: &ocsv1.StorageCluster{
					Spec: ocsv1.StorageClusterSpec{
						ExternalStorage: ocsv1.ExternalStorageClusterSpec{
							Enable: true,
						},
					},
				},
			},
			want: rookCephv1.ReadAffinitySpec{
				Enabled: false,
			},
		},
		{
			name: "External ceph cluster: ReadAffinity enabled by user with crushlocationlabels",
			args: args{
				sc: &ocsv1.StorageCluster{
					Spec: ocsv1.StorageClusterSpec{
						ExternalStorage: ocsv1.ExternalStorageClusterSpec{
							Enable: true,
						},
						CSI: &ocsv1.CSIDriverSpec{
							ReadAffinity: &rookCephv1.ReadAffinitySpec{
								Enabled:             true,
								CrushLocationLabels: []string{"topology.io/zone"},
							},
						},
					},
				},
			},
			want: rookCephv1.ReadAffinitySpec{
				Enabled:             true,
				CrushLocationLabels: []string{"topology.io/zone"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetReadAffinityOptions(tt.args.sc); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetReadAffinityOptions() = %v, want %v", got, tt.want)
			}
		})
	}
}
