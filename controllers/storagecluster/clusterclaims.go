package storagecluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clusterclientv1alpha1 "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	RookCephMonSecretName = "rook-ceph-mon"
	FsidKey               = "fsid"
	OdfOperatorNamePrefix = "odf-operator"
	ClusterClaimCRDName   = "clusterclaims.cluster.open-cluster-management.io"
)

var (
	ClusterClaimGroup         = "odf"
	OdfVersion                = fmt.Sprintf("version.%s.openshift.io", ClusterClaimGroup)
	StorageSystemName         = fmt.Sprintf("storagesystemname.%s.openshift.io", ClusterClaimGroup)
	StorageClusterName        = fmt.Sprintf("storageclustername.%s.openshift.io", ClusterClaimGroup)
	StorageClusterCount       = fmt.Sprintf("count.storageclusters.%s.openshift.io", ClusterClaimGroup)
	StorageClusterDROptimized = fmt.Sprintf("droptimized.%s.openshift.io", ClusterClaimGroup)
	CephFsid                  = fmt.Sprintf("cephfsid.%s.openshift.io", ClusterClaimGroup)
)

type ocsClusterClaim struct{}

type ClusterClaimCreator struct {
	Context             context.Context
	Logger              logr.Logger
	Client              client.Client
	Values              map[string]string
	StorageCluster      *ocsv1.StorageCluster
	StorageClusterCount int
}

func doesClusterClaimCrdExist(ctx context.Context, client client.Client) (bool, error) {
	crd := extensionsv1.CustomResourceDefinition{}
	err := client.Get(ctx, types.NamespacedName{Name: ClusterClaimCRDName}, &crd)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (obj *ocsClusterClaim) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	ctx := context.TODO()
	if crdExists, err := doesClusterClaimCrdExist(ctx, r.Client); !crdExists {
		if err != nil {
			r.Log.Error(err, "An error has occurred while fetching customresourcedefinition", "CustomResourceDefinition", ClusterClaimCRDName)
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	creator := ClusterClaimCreator{
		Logger:         r.Log,
		Context:        ctx,
		Client:         r.Client,
		Values:         make(map[string]string),
		StorageCluster: instance,
	}

	odfVersion, err := creator.getOdfVersion()
	if err != nil {
		r.Log.Error(err, "failed to get odf version for operator. retrying again")
		return reconcile.Result{}, err
	}

	storageClusterCount := len(r.clusters.GetStorageClusters())

	cephFsid, err := creator.getCephFsid()
	if err != nil {
		r.Log.Error(err, "failed to get ceph fsid from secret. retrying again")
		return reconcile.Result{}, err
	}

	storageSystemName, err := creator.getStorageSystemName()
	if err != nil {
		r.Log.Error(err, "failed to get storagesystem name. retrying again")
		return reconcile.Result{}, err
	}

	var isDROptimized = "false"
	// Set isDROptmized to "false" in case of external clusters as we currently don't have to way to determine
	// if external cluster OSDs are using bluestore-rdr
	if !instance.Spec.ExternalStorage.Enable {
		isDROptimized, err = creator.getIsDROptimized()
		if err != nil {
			r.Log.Error(err, "failed to get cephcluster status. retrying again")
			return reconcile.Result{}, err
		}
	}

	err = creator.setStorageClusterCount(strconv.Itoa(storageClusterCount)).
		setStorageSystemName(storageSystemName).
		setStorageClusterName(instance.Name).
		setOdfVersion(odfVersion).
		setCephFsid(cephFsid).
		setDROptimized(isDROptimized).
		create()

	return reconcile.Result{}, err
}

func (c *ClusterClaimCreator) create() error {
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		c.Logger.Error(err, "Failed to get kubeconfig for ClusterClaim client.")
		return err
	}

	client, err := clusterclientv1alpha1.NewForConfig(kubeconfig)
	if err != nil {
		c.Logger.Error(err, "Failed to create ClusterClaim client.")
		return err
	}

	for name, value := range c.Values {
		cc := &clusterv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: clusterv1alpha1.ClusterClaimSpec{
				Value: value,
			},
		}

		existingClaim, err := client.ClusterV1alpha1().ClusterClaims().Get(c.Context, name, metav1.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				c.Logger.Error(err, "failed to get clusterclaim", "ClusterClaim", name)
				return err
			}
			_, err = client.ClusterV1alpha1().ClusterClaims().Create(c.Context, cc, metav1.CreateOptions{})
			if err != nil {
				c.Logger.Error(err, "failed to create clusterclaim", "ClusterClaim", name)
				return err
			}
			c.Logger.Info("created clusterclaim", "ClusterClaim", cc.Name)
			continue
		}

		if equality.Semantic.DeepEqual(existingClaim.Spec, cc.Spec) {
			c.Logger.Info("clusterclaim unchanged", "ClusterClaim", name)
			continue
		}

		existingClaim.Spec = cc.Spec
		_, err = client.ClusterV1alpha1().ClusterClaims().Update(c.Context, existingClaim, metav1.UpdateOptions{})
		if err != nil {
			c.Logger.Error(err, "failed to update clusterclaim", "ClusterClaim", name)
			return err
		}
		c.Logger.Info("updated clusterclaim", "ClusterClaim", cc.Name)
	}

	return nil
}
func (c *ClusterClaimCreator) getOdfVersion() (string, error) {
	var csvs operatorsv1alpha1.ClusterServiceVersionList
	err := c.Client.List(c.Context, &csvs, &client.ListOptions{Namespace: c.StorageCluster.Namespace})
	if err != nil {
		return "", err
	}

	for _, csv := range csvs.Items {
		if strings.HasPrefix(csv.Name, OdfOperatorNamePrefix) {
			return csv.Spec.Version.String(), nil
		}
	}

	return "", fmt.Errorf("failed to find csv with prefix %q", OdfOperatorNamePrefix)
}

func (c *ClusterClaimCreator) getCephFsid() (string, error) {
	var rookCephMonSecret corev1.Secret
	err := c.Client.Get(c.Context, types.NamespacedName{Name: RookCephMonSecretName, Namespace: c.StorageCluster.Namespace}, &rookCephMonSecret)
	if err != nil {
		return "", err
	}
	if val, ok := rookCephMonSecret.Data[FsidKey]; ok {
		return string(val), nil
	}

	return "", fmt.Errorf("failed to fetch ceph fsid from %q secret", RookCephMonSecretName)
}

func (c *ClusterClaimCreator) getIsDROptimized() (string, error) {
	var cephCluster rookCephv1.CephCluster
	err := c.Client.Get(c.Context, types.NamespacedName{Name: generateNameForCephClusterFromString(c.StorageCluster.Name), Namespace: c.StorageCluster.Namespace}, &cephCluster)
	if err != nil {
		return "false", err
	}
	if cephCluster.Status.CephStorage == nil || cephCluster.Status.CephStorage.OSD.StoreType == nil {
		return "false", fmt.Errorf("cephcluster status does not have OSD store information")
	}
	bluestorerdr, ok := cephCluster.Status.CephStorage.OSD.StoreType["bluestore-rdr"]
	if !ok {
		return "false", nil
	}
	total := getOsdCount(c.StorageCluster)
	if bluestorerdr < total {
		return "false", nil
	}
	return "true", nil
}

func (c *ClusterClaimCreator) setStorageClusterCount(count string) *ClusterClaimCreator {
	c.Values[StorageClusterCount] = count
	return c
}

func (c *ClusterClaimCreator) setStorageSystemName(name string) *ClusterClaimCreator {
	c.Values[StorageSystemName] = fmt.Sprintf("%s/%s", name, c.StorageCluster.GetNamespace())
	return c
}

func (c *ClusterClaimCreator) setOdfVersion(version string) *ClusterClaimCreator {
	c.Values[OdfVersion] = version
	return c
}

func (c *ClusterClaimCreator) setStorageClusterName(name string) *ClusterClaimCreator {
	c.Values[StorageClusterName] = fmt.Sprintf("%s/%s", name, c.StorageCluster.GetNamespace())
	return c
}

func (c *ClusterClaimCreator) setCephFsid(fsid string) *ClusterClaimCreator {
	c.Values[CephFsid] = fsid
	return c
}

func (c *ClusterClaimCreator) setDROptimized(optimized string) *ClusterClaimCreator {
	c.Values[StorageClusterDROptimized] = optimized
	return c
}

func (c *ClusterClaimCreator) getStorageSystemName() (string, error) {
	for _, ref := range c.StorageCluster.OwnerReferences {
		if ref.Kind == "StorageSystem" {
			return ref.Name, nil
		}
	}

	return "", fmt.Errorf("failed to find parent StorageSystem's name in StorageCluster %q ownerreferences", c.StorageCluster.Name)
}

func (obj *ocsClusterClaim) ensureDeleted(r *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	r.Log.Info("deleting ClusterClaim resources")
	ctx := context.TODO()
	if crdExists, err := doesClusterClaimCrdExist(ctx, r.Client); !crdExists {
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}
	names := []string{OdfVersion, StorageSystemName, StorageClusterName, CephFsid}
	for _, name := range names {
		cc := clusterv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		err := r.Client.Delete(context.TODO(), &cc)
		if errors.IsNotFound(err) {
			continue
		} else if err != nil {
			r.Log.Error(err, "failed to delete ClusterClaim", "ClusterClaim", cc.Name)
			return reconcile.Result{}, fmt.Errorf("failed to delete %v: %v", cc.Name, err)
		}
	}

	return reconcile.Result{}, nil
}
