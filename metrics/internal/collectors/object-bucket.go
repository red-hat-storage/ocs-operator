package collectors

import (
	"context"
	"fmt"

	rgwadmin "github.com/ceph/go-ceph/rgw/admin"
	libbucket "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	bktclient "github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/clientset/versioned"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	bucketProvisionerName = "ceph.rook.io-bucket"
	rgwServiceName        = "rook-ceph-rgw"
	svcDNSSuffix          = "svc"
	prometheusUserName    = "prometheus-user"
	accessKey             = "AccessKey"
	secretKey             = "SecretKey"
	cephUser              = "cephUser"
)

var _ prometheus.Collector = &ObjectBucketCollector{}

// ObjectBucketCollector is a custom collector for CephObjectStore Custom Resource
type ObjectBucketCollector struct {
	OBSizeTotal           *prometheus.Desc
	OBSizeMax             *prometheus.Desc
	OBObjectCountTotal    *prometheus.Desc
	OBObjectCountMax      *prometheus.Desc
	ObjectBucketClaimInfo *prometheus.Desc
	ObjectBucketCount     *prometheus.Desc
	Informer              cache.SharedIndexInformer
	AllowedNamespaces     []string
	bktclient             bktclient.Interface
	rookclient            rookclient.Interface
	k8sclient             kubernetes.Interface
}

// NewObjectBucketCollector constructs a collector
func NewObjectBucketCollector(opts *options.Options) *ObjectBucketCollector {
	sharedIndexInformer := CephObjectStoreInformer(opts)

	return &ObjectBucketCollector{
		OBSizeTotal: prometheus.NewDesc(
			"ocs_objectbucket_used_bytes",
			"The size of the objectbucket consumed in bytes",
			[]string{"objectbucket", "object_store"},
			nil,
		),
		OBSizeMax: prometheus.NewDesc(
			"ocs_objectbucket_max_bytes",
			"Maximum allowed size of the object bucket in bytes",
			[]string{"objectbucket", "object_store"},
			nil,
		),
		OBObjectCountTotal: prometheus.NewDesc(
			"ocs_objectbucket_objects_total",
			"The total number of objects in the object bucket",
			[]string{"objectbucket", "object_store"},
			nil,
		),
		OBObjectCountMax: prometheus.NewDesc(
			"ocs_objectbucket_max_objects",
			"Maximum number of objects allowed in the object bucket",
			[]string{"objectbucket", "object_store"},
			nil,
		),
		ObjectBucketClaimInfo: prometheus.NewDesc(
			"ocs_objectbucketclaim_info",
			"Information about the ObjectBucketClaim",
			[]string{"objectbucketclaim", "objectbucket", "storageclass"},
			nil,
		),

		ObjectBucketCount: prometheus.NewDesc(
			"ocs_objectbucket_count_total",
			"The total number of objectbuckets",
			[]string{"object_store"},
			nil,
		),

		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
		bktclient:         bktclient.NewForConfigOrDie(opts.Kubeconfig),
		rookclient:        rookclient.NewForConfigOrDie(opts.Kubeconfig),
		k8sclient:         kubernetes.NewForConfigOrDie(opts.Kubeconfig),
	}
}

// Run starts CephObjectStore informer
func (c *ObjectBucketCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *ObjectBucketCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.OBSizeTotal,
		c.OBSizeMax,
		c.OBObjectCountTotal,
		c.OBObjectCountMax,
		c.ObjectBucketClaimInfo,
		c.ObjectBucketCount,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *ObjectBucketCollector) Collect(ch chan<- prometheus.Metric) {
	cephObjectStoreLister := cephv1listers.NewCephObjectStoreLister(c.Informer.GetIndexer())
	cephObjectStores := getAllObjectStores(cephObjectStoreLister, c.AllowedNamespaces)
	if len(cephObjectStores) > 0 {
		for _, cephObjectStore := range cephObjectStores {
			adminAPI := c.getAdminOpsClient(*cephObjectStore)
			if adminAPI != nil {
				c.collectObjectBucketMetrics(*cephObjectStore, adminAPI, ch)
			} else {
				klog.Warningf("CephObjectStore %q in namespace %q was skipped", cephObjectStore.Name, cephObjectStore.Namespace)
			}
		}
	}
}

func getEndpoint(cephObjectStore cephv1.CephObjectStore) (host string, port int) {
	return fmt.Sprintf("%s-%s.%s.%s", rgwServiceName, cephObjectStore.Name, cephObjectStore.Namespace, svcDNSSuffix), int(cephObjectStore.Spec.Gateway.Port)
}

func (c *ObjectBucketCollector) getAllObjectBuckets(cephObjectStore cephv1.CephObjectStore) (objectBuckets []libbucket.ObjectBucket) {
	selector := fmt.Sprintf("bucket-provisioner=%s.%s", cephObjectStore.Namespace, bucketProvisionerName)
	listOpts := metav1.ListOptions{
		LabelSelector: selector,
	}

	// collect the OBC list
	objectBucketClaimList, err := c.bktclient.ObjectbucketV1alpha1().
		ObjectBucketClaims(cephObjectStore.Namespace).List(context.TODO(), listOpts)
	// any error, don't panic
	if err != nil {
		// make a non-nil OBC List with Items 'nil'
		objectBucketClaimList = &libbucket.ObjectBucketClaimList{Items: nil}
	}
	objectBucketsList, err := c.bktclient.ObjectbucketV1alpha1().ObjectBuckets().List(context.TODO(), listOpts)
	if err != nil {
		klog.Errorf("Couldn't list ObjectBuckets. %v", err)
		return
	}
	objectBuckets = make([]libbucket.ObjectBucket, 0)

	//filter based on ceph-object store
	bucketHost, _ := getEndpoint(cephObjectStore)
	for _, ob := range objectBucketsList.Items {
		// if this ob's claimref object is not set (to an OBC), try to set it
		if ob.Spec.ClaimRef == nil || ob.Spec.ClaimRef.Name == "" {
			// this for loop will be safely skipped if the 'Items' object is 'nil'
			for _, obc := range objectBucketClaimList.Items {
				// add the OBC details into the matching OB's spec.claimref
				if obc.Spec.ObjectBucketName == ob.Name {
					ob.Spec.ClaimRef = &corev1.ObjectReference{Name: obc.Name, Namespace: obc.Namespace}
					// found the obc associated with this ob
					break
				}
			}
		}
		if ob.Spec.Endpoint.BucketHost == bucketHost {
			objectBuckets = append(objectBuckets, ob)
		}
	}
	return
}

func (c *ObjectBucketCollector) getAdminOpsClient(cephObjectStore cephv1.CephObjectStore) *rgwadmin.API {
	ctx := context.TODO()
	//TODO: SSL endpoint
	if (cephObjectStore.Spec.Gateway.Port == 0) && (cephObjectStore.Spec.Gateway.SecurePort > 0) {
		klog.Warningf("Secure port is not supported")
		return nil
	}
	prometheusSecretName := fmt.Sprintf("rook-ceph-object-user-%s-%s", cephObjectStore.Name, prometheusUserName)

	secret, err := c.k8sclient.CoreV1().Secrets(cephObjectStore.Namespace).Get(ctx, prometheusSecretName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Secret for %s not found. %v", prometheusUserName, err)
		return nil
	}

	if secret != nil {
		host, port := getEndpoint(cephObjectStore)
		endPoint := fmt.Sprintf("http://%s:%d", host, port)
		adminAPI, err := rgwadmin.New(endPoint, string(secret.Data[accessKey]), string(secret.Data[secretKey]), nil)
		if err != nil {
			klog.Errorf("Connection to RGW failed. %v", err)
			return nil
		}
		return adminAPI
	}

	return nil
}

func (c *ObjectBucketCollector) collectObjectBucketMetrics(cephObjectStore cephv1.CephObjectStore, adminAPI *rgwadmin.API, ch chan<- prometheus.Metric) {
	generatestat := true
	ctx := context.TODO()
	objectBucketList := c.getAllObjectBuckets(cephObjectStore)
	if len(objectBucketList) > 0 {
		for _, ob := range objectBucketList {
			userinfo, err := adminAPI.GetUser(ctx, rgwadmin.User{ID: ob.Spec.AdditionalState[cephUser], GenerateStat: &generatestat})
			if err != nil {
				klog.Errorf("Failed to get user for object bucket %q. %v", err, ob.Name)
				return
			}
			ch <- prometheus.MustNewConstMetric(c.OBSizeTotal,
				prometheus.GaugeValue, float64(*userinfo.Stat.Size),
				ob.Name,
				cephObjectStore.Name)
			ch <- prometheus.MustNewConstMetric(c.OBSizeMax,
				prometheus.GaugeValue,
				float64(*userinfo.UserQuota.MaxSize),
				ob.Name,
				cephObjectStore.Name)
			ch <- prometheus.MustNewConstMetric(c.OBObjectCountTotal,
				prometheus.GaugeValue,
				float64(*userinfo.Stat.NumObjects),
				ob.Name,
				cephObjectStore.Name)
			ch <- prometheus.MustNewConstMetric(c.OBObjectCountMax,
				prometheus.GaugeValue,
				float64(*userinfo.UserQuota.MaxObjects),
				ob.Name,
				cephObjectStore.Name)
			ch <- prometheus.MustNewConstMetric(c.ObjectBucketClaimInfo,
				prometheus.GaugeValue,
				1,
				ob.Spec.ClaimRef.Name,
				ob.Name,
				ob.Spec.StorageClassName)
		}
		ch <- prometheus.MustNewConstMetric(c.ObjectBucketCount,
			prometheus.GaugeValue,
			float64(len(objectBucketList)),
			cephObjectStore.Name)

	} else {
		klog.Infof("No ObjectBuckets present in the object store %s", cephObjectStore.Name)
	}
}
