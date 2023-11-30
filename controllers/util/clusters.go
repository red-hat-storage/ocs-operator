package util

import (
	"context"
	"sort"

	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Clusters struct {
	internalStorageClusters []ocsv1.StorageCluster
	externalStorageClusters []ocsv1.StorageCluster

	names           []string
	namespaces      []string
	namespacedNames []string
}

func (c *Clusters) GetInternalStorageClusters() []ocsv1.StorageCluster {
	return c.internalStorageClusters
}

func (c *Clusters) GetExternalStorageClusters() []ocsv1.StorageCluster {
	return c.externalStorageClusters
}

func (c *Clusters) GetStorageClusters() []ocsv1.StorageCluster {
	storageClusters := make([]ocsv1.StorageCluster, 0)
	storageClusters = append(storageClusters, c.internalStorageClusters...)
	storageClusters = append(storageClusters, c.externalStorageClusters...)
	return storageClusters
}

func (c *Clusters) GetNames() []string {
	return c.names
}

func (c *Clusters) GetNamespaces() []string {
	return c.namespaces
}

func (c *Clusters) GetNamespacedNames() []string {
	return c.namespacedNames
}

func (c *Clusters) IsInternalStorageClusterExist() bool {
	return len(c.internalStorageClusters) > 0
}

func (c *Clusters) IsExternalStorageClusterExist() bool {
	return len(c.externalStorageClusters) > 0
}

func (c *Clusters) IsInternalAndExternalStorageClustersExist() bool {
	return len(c.internalStorageClusters) > 0 && len(c.externalStorageClusters) > 0
}

func GetClusters(ctx context.Context, cli client.Client) (*Clusters, error) {

	var storageClusters ocsv1.StorageClusterList
	if err := cli.List(ctx, &storageClusters); err != nil {
		return nil, err
	}

	internalStorageClusters := make([]ocsv1.StorageCluster, 0)
	externalStorageClusters := make([]ocsv1.StorageCluster, 0)
	names := make([]string, 0)
	namespaces := make([]string, 0)
	namespacedNames := make([]string, 0)

	for _, storageCluster := range storageClusters.Items {
		names = append(names, storageCluster.Name)
		namespaces = append(namespaces, storageCluster.Namespace)
		namespacedNames = append(namespacedNames, storageCluster.Namespace+"/"+storageCluster.Name)

		if !storageCluster.Spec.ExternalStorage.Enable {
			internalStorageClusters = append(internalStorageClusters, storageCluster)
		} else {
			externalStorageClusters = append(externalStorageClusters, storageCluster)
		}
	}

	sort.Strings(names)
	sort.Strings(namespaces)
	sort.Strings(namespacedNames)

	// sort internal storage clusters by namespace and name
	sort.Slice(internalStorageClusters, func(i, j int) bool {
		if internalStorageClusters[i].Namespace < internalStorageClusters[j].Namespace {
			return true
		} else if internalStorageClusters[i].Namespace > internalStorageClusters[j].Namespace {
			return false
		}
		return internalStorageClusters[i].Name < internalStorageClusters[j].Name
	})

	// sort external storage clusters by namespace and name
	sort.Slice(externalStorageClusters, func(i, j int) bool {
		if externalStorageClusters[i].Namespace < externalStorageClusters[j].Namespace {
			return true
		} else if externalStorageClusters[i].Namespace > externalStorageClusters[j].Namespace {
			return false
		}
		return externalStorageClusters[i].Name < externalStorageClusters[j].Name
	})

	return &Clusters{
		internalStorageClusters: internalStorageClusters,
		externalStorageClusters: externalStorageClusters,
		names:                   names,
		namespaces:              namespaces,
		namespacedNames:         namespacedNames,
	}, nil
}

// AreOtherStorageClustersReady checks if all other storage clusters (internal and external) are ready.
func (c *Clusters) AreOtherStorageClustersReady(instance *ocsv1.StorageCluster) bool {

	for _, sc := range append(c.internalStorageClusters, c.externalStorageClusters...) {
		// ignore the current recociling storage cluster as its status will set in the current reconcile
		if sc.Name != instance.Name && sc.Namespace != instance.Namespace {
			if sc.Status.Phase != PhaseReady {
				return false
			}
		}
	}

	return true
}
