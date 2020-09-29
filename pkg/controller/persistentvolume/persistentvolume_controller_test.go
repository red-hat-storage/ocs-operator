package persistentvolume

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestReconcilePV(t *testing.T) {
	cases := []struct {
		label               string
		csi                 *corev1.CSIPersistentVolumeSource
		invalidStorageClass bool
		reconcile           bool
	}{
		{
			label:     "nil CSI",
			csi:       nil,
			reconcile: false,
		},
		{
			label: "invalid driver",
			csi: &corev1.CSIPersistentVolumeSource{
				Driver: "",
			},
			reconcile: false,
		},
		{
			label: "RBD driver, invalid StorageClass",
			csi: &corev1.CSIPersistentVolumeSource{
				Driver: csiRBDDriverSuffix,
			},
			invalidStorageClass: true,
			reconcile:           false,
		},
		{
			label: "RBD driver, valid StorageClass, invalid SecretRef",
			csi: &corev1.CSIPersistentVolumeSource{
				ControllerExpandSecretRef: &corev1.SecretReference{
					Name: "foo",
				},
				Driver: csiRBDDriverSuffix,
			},
			reconcile: false,
		},
		{
			label: "valid RBD driver, nil SecretRef",
			csi: &corev1.CSIPersistentVolumeSource{
				Driver: csiRBDDriverSuffix,
			},
			reconcile: true,
		},
		{
			label: "valid RBD driver, empty SecretRef",
			csi: &corev1.CSIPersistentVolumeSource{
				Driver: csiRBDDriverSuffix,
				ControllerExpandSecretRef: &corev1.SecretReference{
					Name: "",
				},
			},
			reconcile: true,
		},
		{
			label: "valid CephFS driver",
			csi: &corev1.CSIPersistentVolumeSource{
				Driver: csiCephFSDriverSuffix,
			},
			reconcile: true,
		},
	}

	for i, c := range cases {
		t.Logf("Case %d: %s\n", i+1, c.label)

		storageClassName := ""
		if !c.invalidStorageClass {
			storageClassName = "foo"
		}

		pv := &corev1.PersistentVolume{
			Spec: corev1.PersistentVolumeSpec{
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					CSI: c.csi,
				},
				StorageClassName: storageClassName,
			},
		}

		assert.Equal(t, c.reconcile, reconcilePV(pv))
	}
}
