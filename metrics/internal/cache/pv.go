package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
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
	// RBDClientMap is a map of RBD client addresses to the names of the nodes whose images had this client as a watcher
	RBDClientMap      map[string][]string
	monitorConfig     cephMonitorConfig
	kubeClient        clientset.Interface
	allowedNamespaces []string
}

type Watcher struct {
	Address string      `json:"address,omitempty"`
	Client  int         `json:"client,omitempty"`
	Cookie  json.Number `json:"cookie,omitempty"`
}

type Clients struct {
	Watchers []Watcher `json:"watchers,omitempty"`
}

type PersistentVolumeAttributes struct {
	PersistentVolumeName           string
	PersistentVolumeClaimName      string
	PersistentVolumeClaimNamespace string
	ImageName                      string
	Pool                           string
}

func NewPersistentVolumeStore(opts *options.Options) *PersistentVolumeStore {
	return &PersistentVolumeStore{
		Store:             map[types.UID]PersistentVolumeAttributes{},
		RBDClientMap:      map[string][]string{},
		kubeClient:        clientset.NewForConfigOrDie(opts.Kubeconfig),
		monitorConfig:     cephMonitorConfig{},
		allowedNamespaces: opts.AllowedNamespaces,
	}
}

func runCephRBDStatus(config *cephMonitorConfig, pool, image string) (Clients, error) {
	var clients Clients

	if config.monitor == "" && config.id == "" && config.key == "" {
		return clients, errors.New("unable to get status data. monitor config missing")
	}
	imageSpec := fmt.Sprintf("%s/%s", pool, image)
	args := []string{"status", imageSpec, "--format", "json", "-m", config.monitor, "--id", config.id, "--key", config.key}
	cmd, err := execCommand("rbd", args, 30)
	if err != nil {
		return clients, fmt.Errorf("failed with output : %v, err: %v", string(cmd), err)
	}

	err = json.Unmarshal(cmd, &clients)
	return clients, err
}

func appendIfNotExists(slice []string, value string) []string {
	for _, existingValue := range slice {
		if existingValue == value {
			return slice
		}
	}
	return append(slice, value)
}

// Add inserts to the PersistentVolumeStore.
func (p *PersistentVolumeStore) Add(obj interface{}) error {
	var pv *corev1.PersistentVolume
	// obj can be of PV type or a pointer to it
	switch pvFromObj := obj.(type) {
	case *corev1.PersistentVolume:
		pv = pvFromObj
	case corev1.PersistentVolume:
		pv = &pvFromObj
	default:
		return fmt.Errorf("unexpected object of type %T", obj)
	}

	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	klog.Infof("PV store addition started at %v for PV %v", time.Now(), pv.Name)
	if err := p.add(pv); err != nil {
		return err
	}
	klog.Infof("PV store addition completed at %v", time.Now())

	return nil
}

// add is not thread-safe. So, it must to be called from a thread safe function only.
func (p *PersistentVolumeStore) add(pv *corev1.PersistentVolume) error {
	provisioner := pv.Annotations["pv.kubernetes.io/provisioned-by"]
	if !strings.Contains(provisioner, ".rbd.csi.ceph.com") {
		klog.Infof("Skipping non Ceph CSI RBD volume %s", pv.Name)
		return nil
	}

	if pv.Spec.ClaimRef == nil {
		klog.Infof("Skipping unbound volume %s", pv.Name)
		return nil
	}

	if (p.monitorConfig == cephMonitorConfig{}) {
		var err error
		p.monitorConfig, err = initCeph(p.kubeClient, p.allowedNamespaces)
		if err != nil {
			return fmt.Errorf("failed to initialize ceph: %v", err)
		}
	}

	p.Store[pv.GetUID()] = PersistentVolumeAttributes{
		PersistentVolumeName:           pv.Name,
		PersistentVolumeClaimName:      pv.Spec.ClaimRef.Name,
		PersistentVolumeClaimNamespace: pv.Spec.ClaimRef.Namespace,
		ImageName:                      pv.Spec.CSI.VolumeAttributes["imageName"],
		Pool:                           pv.Spec.CSI.VolumeAttributes["pool"],
	}

	clients, err := runCephRBDStatus(&p.monitorConfig, pv.Spec.CSI.VolumeAttributes["pool"], pv.Spec.CSI.VolumeAttributes["imageName"])
	if err != nil {
		return fmt.Errorf("failed to get image status %v", err)
	}

	nodeName, err := getNodeNameForPV(pv, p.kubeClient)
	if err != nil {
		return fmt.Errorf("failed to get node name for pod: %v", err)
	}

	for _, client := range clients.Watchers {
		p.RBDClientMap[client.Address] = appendIfNotExists(p.RBDClientMap[client.Address], nodeName)
	}

	return nil
}

func getNodeNameForPV(pv *corev1.PersistentVolume, kubeClient clientset.Interface) (string, error) {
	if pv.Spec.ClaimRef == nil {
		return "", fmt.Errorf("persistent volume %s is not bound to any claim", pv.Name)
	}

	pvc, err := kubeClient.CoreV1().PersistentVolumeClaims(pv.Spec.ClaimRef.Namespace).Get(context.Background(), pv.Spec.ClaimRef.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PVC %s/%s: %v", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name, err)
	}

	if pvc.Spec.VolumeName != pv.Name {
		return "", fmt.Errorf("persistent volume %s is not bound to claim %s/%s", pv.Name, pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name)
	}

	podList, err := kubeClient.CoreV1().Pods(pvc.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list pods in namespace %s: %v", pvc.Namespace, err)
	}

	for _, pod := range podList.Items {
		if pod.Spec.Volumes != nil {
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvc.Name {
					return pod.Spec.NodeName, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no pod is using PVC %s/%s", pvc.Namespace, pvc.Name)
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
func (p *PersistentVolumeStore) Get(_ interface{}) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

// GetByKey implements the GetByKey method of the store interface.
func (p *PersistentVolumeStore) GetByKey(_ string) (item interface{}, exists bool, err error) {
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
	klog.Infof("PV store Resync started at %v", time.Now())

	pvList, err := p.kubeClient.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list persistent volumes: %v", err)
	}

	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	for _, pv := range pvList.Items {
		err := p.add(&pv)
		if err != nil {
			return fmt.Errorf("failed to process PV: %s err: %v", pv.Name, err)
		}
	}

	klog.Infof("PV store Resync ended at %v", time.Now())
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
