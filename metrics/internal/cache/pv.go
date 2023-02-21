package cache

import (
	"context"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// Cache metadata for all PV with annotation - pv.kubernetes.io/provisioned-by: *.rbd.csi.ceph.com

var _ cache.Store = &PersistentVolumeStore{}

// PersistentVolumeStore implements the k8s.io/client-go/tools/cache.Store
// interface. It stores persistent volume CSI attributes
type PersistentVolumeStore struct {
	Mutex sync.RWMutex
	// Store is a map of PV UID to PersistentVolumeAttributes
	Store map[types.UID]PersistentVolumeAttributes
}

type PersistentVolumeAttributes struct {
	PersistentVolumeName           string
	PersistentVolumeClaimName      string
	PersistentVolumeClaimNamespace string
	ImageName                      string
	Pool                           string
}

func NewPersistentVolumeStore() *PersistentVolumeStore {
	return &PersistentVolumeStore{
		Store: map[types.UID]PersistentVolumeAttributes{},
	}
}

// Add inserts to the PersistentVolumeStore.
func (p *PersistentVolumeStore) Add(obj interface{}) error {
	pv, ok := obj.(*corev1.PersistentVolume)
	if !ok {
		return fmt.Errorf("unexpected object of type %T", obj)
	}

	provisioner := pv.Annotations["pv.kubernetes.io/provisioned-by"]
	if !strings.Contains(provisioner, ".rbd.csi.ceph.com") {
		klog.Infof("Skipping non Ceph CSI RBD volume %s", pv.Name)
		return nil
	}

	if pv.Spec.ClaimRef == nil {
		klog.Infof("Skipping unbound volume %s", pv.Name)
		return nil
	}

	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	p.Store[pv.GetUID()] = PersistentVolumeAttributes{
		PersistentVolumeName:           pv.Name,
		PersistentVolumeClaimName:      pv.Spec.ClaimRef.Name,
		PersistentVolumeClaimNamespace: pv.Spec.ClaimRef.Namespace,
		ImageName:                      pv.Spec.CSI.VolumeAttributes["imageName"],
		Pool:                           pv.Spec.CSI.VolumeAttributes["pool"],
	}

	return nil
}

// Update updates the existing entry in the PersistentVolumeStore.
func (p *PersistentVolumeStore) Update(obj interface{}) error {
	return p.Add(obj)
}

// Delete deletes an existing entry in the PersistentVolumeStore.
func (p *PersistentVolumeStore) Delete(obj interface{}) error {
	o, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	delete(p.Store, o.GetUID())

	return nil
}

// List implements the List method of the store interface.
func (p *PersistentVolumeStore) List() []interface{} {
	return nil
}

// ListKeys implements the ListKeys method of the store interface.
func (p *PersistentVolumeStore) ListKeys() []string {
	return nil
}

// Get implements the Get method of the store interface.
func (p *PersistentVolumeStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

// GetByKey implements the GetByKey method of the store interface.
func (p *PersistentVolumeStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

// Replace will delete the contents of the store, using instead the
// given list.
func (p *PersistentVolumeStore) Replace(list []interface{}, _ string) error {
	p.Mutex.Lock()
	p.Store = map[types.UID]PersistentVolumeAttributes{}
	p.Mutex.Unlock()

	for _, o := range list {
		err := p.Add(o)
		if err != nil {
			return err
		}
	}

	return nil
}

// Resync implements the Resync method of the store interface.
func (p *PersistentVolumeStore) Resync() error {
	return nil
}

func CreatePersistentVolumeListWatch(kubeClient clientset.Interface, fieldSelector string) cache.ListerWatcher {
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.FieldSelector = fieldSelector
			return kubeClient.CoreV1().PersistentVolumes().List(context.TODO(), opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = fieldSelector
			return kubeClient.CoreV1().PersistentVolumes().Watch(context.TODO(), opts)
		},
	}
}
