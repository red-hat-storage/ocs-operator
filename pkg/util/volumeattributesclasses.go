package util

import (
	"context"

	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func VolumeAttributesClassFromExisting(
	ctx context.Context,
	kubeClient client.Client,
	vacName string,
) (*storagev1.VolumeAttributesClass, error) {
	vac := &storagev1.VolumeAttributesClass{}
	vac.Name = vacName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(vac), vac); err != nil {
		return nil, err
	}

	if vac.DriverName != RbdDriverName {
		return nil, ErrUnsupportedDriver
	}

	return vac, nil
}
