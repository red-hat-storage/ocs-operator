package util

import (
	"context"
	"errors"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add storagev1 to scheme: %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

func TestVolumeAttributesClassFromExisting(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		vacName     string
		objs        []client.Object
		wantErr     bool
		expectedErr error
	}{
		{
			name:    "returns VAC with supported RBD driver",
			vacName: "rbd-qos-high",
			objs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos-high"},
					DriverName: RbdDriverName,
					Parameters: map[string]string{
						"maxReadIops":  "2000",
						"maxWriteIops": "2000",
					},
				},
			},
		},
		{
			name:        "rejects VAC with CephFS driver",
			vacName:     "cephfs-qos-high",
			expectedErr: ErrUnsupportedDriver,
			objs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "cephfs-qos-high"},
					DriverName: CephFSDriverName,
					Parameters: map[string]string{
						"maxReadBps": "209715200",
					},
				},
			},
		},
		{
			name:        "rejects VAC with NFS driver",
			vacName:     "nfs-qos",
			expectedErr: ErrUnsupportedDriver,
			objs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "nfs-qos"},
					DriverName: NfsDriverName,
					Parameters: map[string]string{
						"maxReadIops": "500",
					},
				},
			},
		},
		{
			name:        "rejects VAC with unsupported driver",
			vacName:     "ebs-qos",
			expectedErr: ErrUnsupportedDriver,
			objs: []client.Object{
				&storagev1.VolumeAttributesClass{
					ObjectMeta: metav1.ObjectMeta{Name: "ebs-qos"},
					DriverName: "ebs.csi.aws.com",
					Parameters: map[string]string{
						"maxReadIops": "1000",
					},
				},
			},
		},
		{
			name:    "returns error when VAC not found",
			vacName: "non-existent",
			objs:    []client.Object{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := newFakeClient(t, tt.objs...)

			vac, err := VolumeAttributesClassFromExisting(ctx, kubeClient, tt.vacName)
			if tt.expectedErr != nil {
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("expected error %v, got %v", tt.expectedErr, err)
				}
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if vac.Name != tt.vacName {
				t.Fatalf("expected name %s, got %s", tt.vacName, vac.Name)
			}
		})
	}
}
