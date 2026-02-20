package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
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
	RBDClientMap         map[string][]string
	RBDChildrenMap       map[string]int
	monitorConfig     cephMonitorConfig
	monitorConfigInit bool
	kubeClient           clientset.Interface
	cephClusterNamespace string
	cephAuthNamespace    string
	CephFSSubvolumeCount int
	// Functions to make testing easier
	initCephFn                func(kubeclient clientset.Interface, cephClusterNamespace, cephAuthNamespace string) (cephMonitorConfig, error)
	runCephRBDStatusFn        func(config *cephMonitorConfig, pool, namespace, image string) (Clients, error)
	runCephRBDChildrenCountFn func(config *cephMonitorConfig, pool, namespace, image string) (int, error)
	// TODO: Use fake k8s client instead
	getNodeNameForPVFn        func(pv *corev1.PersistentVolume, kubeClient clientset.Interface) (string, error)
	runCephfsSubvolumeCountFn func(config *cephMonitorConfig) (int, error)
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
	RadosNameSpace                 string
	ImageName                      string
	Pool                           string
}

type pvPreparedData struct {
	uid           types.UID
	attrs         *PersistentVolumeAttributes
	clients       *Clients
	nodeName      string
	childrenCount int
	imageName     string
}

// Define the struct to match rbd children JSON output
type rbdChild struct {
	Pool          string `json:"pool"`
	PoolNamespace string `json:"pool_namespace"`
	Image         string `json:"image"`
}

func NewPersistentVolumeStore(opts *options.Options) *PersistentVolumeStore {
	return &PersistentVolumeStore{
		Store:                     map[types.UID]PersistentVolumeAttributes{},
		RBDClientMap:              map[string][]string{},
		RBDChildrenMap:            make(map[string]int),
		kubeClient:                clientset.NewForConfigOrDie(opts.Kubeconfig),
		monitorConfig:             cephMonitorConfig{},
		cephClusterNamespace:      opts.AllowedNamespaces[0],
		cephAuthNamespace:         opts.CephAuthNamespace,
		CephFSSubvolumeCount:      0,
		initCephFn:                initCeph,
		runCephRBDStatusFn:        runCephRBDStatus,
		runCephRBDChildrenCountFn: runCephRBDChildrenCount,
		getNodeNameForPVFn:        getNodeNameForPV,
		runCephfsSubvolumeCountFn: runCephFSSubvolumeCount,
	}
}

func runCephRBDStatus(config *cephMonitorConfig, pool, namespace, image string) (Clients, error) {
	var clients Clients

	if config.monitor == "" && config.id == "" && config.key == "" {
		return clients, errors.New("unable to get status data. monitor config missing")
	}
	imageSpec := fmt.Sprintf("%s/%s", pool, image)
	args := []string{"status", imageSpec, "--namespace", namespace, "--format", "json", "-m", config.monitor, "--id", config.id, "--key", config.key}
	cmd, err := execCommand("rbd", args, 30)
	if err != nil {
		return clients, fmt.Errorf("failed with output : %v, err: %v", string(cmd), err)
	}

	err = json.Unmarshal(cmd, &clients)
	return clients, err
}

func runCephRBDChildrenCount(config *cephMonitorConfig, pool, namespace, image string) (int, error) {

	if config.monitor == "" && config.id == "" && config.key == "" {
		return 0, errors.New("unable to get status data. monitor config missing")
	}
	imageSpec := fmt.Sprintf("%s/%s", pool, image)

	args := []string{
		"children", imageSpec,
		"--namespace", namespace,
		"--format", "json",
		"-m", config.monitor,
		"--id", config.id,
		"--key", config.key,
	}

	cmd, err := execCommand("rbd", args, 30)
	if err != nil {
		return 0, fmt.Errorf("failed to execute rbd command: %w", err)
	}
	if len(cmd) == 0 {
		return 0, nil // no children
	}

	// Unmarshal the json output
	// Here is how the output looks like
	// [{
	// "pool": "ocs-storagecluster-cephblockpool",
	// "pool_namespace": "",
	// "image": "csi-vol-1c886d74-e27d-4714-acb4-93a8b1ce97b8-temp"},
	//{"pool": "ocs-storagecluster-cephblockpool",
	//"pool_namespace": "",
	//"image": "csi-vol-4317cc5d-fa8c-490a-9e6d-907519788acf-temp"}]

	var children []rbdChild
	if err := json.Unmarshal([]byte(cmd), &children); err != nil {
		return 0, fmt.Errorf("failed to parse rbd children JSON output: %v, output: %q", err, string(cmd))
	}

	return len(children), nil
}

func appendIfNotExists(slice []string, value string) []string {
	for _, existingValue := range slice {
		if existingValue == value {
			return slice
		}
	}
	return append(slice, value)
}

func (p *PersistentVolumeStore) ensureMonitorConfig() error {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	if p.monitorConfigInit {
		return nil
	}

	config, err := p.initCephFn(p.kubeClient, p.cephClusterNamespace, p.cephAuthNamespace)
	if err != nil {
		return err
	}

	p.monitorConfig = config
	p.monitorConfigInit = true
	return nil
}

// Add inserts to the PersistentVolumeStore.
func (p *PersistentVolumeStore) Add(obj interface{}) error {
	var pv *corev1.PersistentVolume
	switch pvFromObj := obj.(type) {
	case *corev1.PersistentVolume:
		pv = pvFromObj
	case corev1.PersistentVolume:
		pv = &pvFromObj
	default:
		return fmt.Errorf("unexpected object of type %T", obj)
	}

	klog.Infof("PV store addition started at %v for PV %v", time.Now(), pv.Name)

	data, err := p.prepareData(pv)
	if err != nil {
		return err
	}

	p.Mutex.Lock()
	p.applyPreparedData(data)
	p.Mutex.Unlock()

	klog.Infof("PV store addition completed at %v for PV %v", time.Now(), pv.Name)
	return nil
}

func (p *PersistentVolumeStore) prepareData(pv *corev1.PersistentVolume) (*pvPreparedData, error) {
	provisioner := pv.Annotations["pv.kubernetes.io/provisioned-by"]
	if !strings.Contains(provisioner, ".rbd.csi.ceph.com") {
		klog.Infof("Skipping non Ceph CSI RBD volume %s", pv.Name)
		return nil, nil
	}

	if pv.Spec.ClaimRef == nil {
		klog.Infof("ClaimRef empty for pv %s", pv.Name)
		return nil, nil
	}

	if err := p.ensureMonitorConfig(); err != nil {
		return nil, fmt.Errorf("failed to initialize ceph: %v", err)
	}

	p.Mutex.RLock()
	config := p.monitorConfig
	p.Mutex.RUnlock()

	radosnamespace := util.ImplicitRbdRadosNamespaceName
	if pv.Spec.CSI.VolumeAttributes["radosNamespace"] != "" {
		radosnamespace = pv.Spec.CSI.VolumeAttributes["radosNamespace"]
	}

	pool := pv.Spec.CSI.VolumeAttributes["pool"]
	namespace := pv.Spec.CSI.VolumeAttributes["radosNamespace"]
	imageName := pv.Spec.CSI.VolumeAttributes["imageName"]

	attrs := &PersistentVolumeAttributes{
		PersistentVolumeName:           pv.Name,
		PersistentVolumeClaimName:      pv.Spec.ClaimRef.Name,
		PersistentVolumeClaimNamespace: pv.Spec.ClaimRef.Namespace,
		ImageName:                      imageName,
		Pool:                           pool,
		RadosNameSpace:                 radosnamespace,
	}

	clients, err := p.runCephRBDStatusFn(&config, pool, namespace, imageName)
	if err != nil {
		return nil, fmt.Errorf("failed to get image status %v", err)
	}

	nodeName, err := p.getNodeNameForPVFn(pv, p.kubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get node name for pod: %v", err)
	}

	childrenCount, err := p.runCephRBDChildrenCountFn(&config, pool, namespace, imageName)
	if err != nil {
		return nil, fmt.Errorf("failed to get children count for image %s/%s: %v", namespace, imageName, err)
	}

	return &pvPreparedData{
		uid:           pv.GetUID(),
		attrs:         attrs,
		clients:       &clients,
		nodeName:      nodeName,
		childrenCount: childrenCount,
		imageName:     imageName,
	}, nil
}

func (p *PersistentVolumeStore) applyPreparedData(data *pvPreparedData) {
	if data == nil || data.attrs == nil {
		return
	}

	p.Store[data.uid] = *data.attrs
	p.RBDChildrenMap[data.imageName] = data.childrenCount

	if data.nodeName == "" || data.clients == nil {
		return
	}

	for _, client := range data.clients.Watchers {
		p.RBDClientMap[client.Address] = appendIfNotExists(p.RBDClientMap[client.Address], data.nodeName)
	}
}

func getNodeNameForPV(pv *corev1.PersistentVolume, kubeClient clientset.Interface) (string, error) {
	if pv.Status.Phase != corev1.VolumeBound {
		return "", nil
	}

	nodeList, err := kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	uniqueVolumeName := fmt.Sprintf("kubernetes.io/csi/%s^%s", pv.Spec.CSI.Driver, pv.Spec.CSI.VolumeHandle)
	for _, node := range nodeList.Items {
		for _, volumeInUse := range node.Status.VolumesInUse {
			if volumeInUse == corev1.UniqueVolumeName(uniqueVolumeName) {
				return node.Name, nil
			}
		}
	}

	return "", nil
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
	p.RBDClientMap = map[string][]string{}
	p.RBDChildrenMap = make(map[string]int)
	p.Mutex.Unlock()

	for _, o := range list {
		err := p.Add(o)
		if err != nil {
			klog.Errorf("failed to add PV during Replace: %v", err)
			continue
		}
	}

	return nil
}

// Resync implements the Resync method of the store interface.
func (p *PersistentVolumeStore) Resync() error {
	klog.Infof("PV store Resync started at %v", time.Now())

	if err := p.ensureMonitorConfig(); err != nil {
		return fmt.Errorf("failed to initialize ceph: %v", err)
	}

	pvList, err := p.kubeClient.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list persistent volumes: %v", err)
	}

	var preparedData []*pvPreparedData
	for idx := range pvList.Items {
		pv := &pvList.Items[idx]

		data, err := p.prepareData(pv)
		if err != nil {
			klog.Errorf("failed to process PV %s: %v", pv.Name, err)
			continue
		}
		if data != nil {
			preparedData = append(preparedData, data)
		}
	}

	p.Mutex.Lock()
	for _, data := range preparedData {
		p.applyPreparedData(data)
	}
	p.Mutex.Unlock()

	p.Mutex.RLock()
	config := p.monitorConfig
	p.Mutex.RUnlock()

	klog.Infof("Caching CephFS subvolume count")
	subvolCount, err := p.runCephfsSubvolumeCountFn(&config)
	if err != nil {
		klog.Errorf("failed to get CephFS subvolume count: %v", err)
	} else {
		p.Mutex.Lock()
		p.CephFSSubvolumeCount = subvolCount
		p.Mutex.Unlock()
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

func runCephFSSubvolumeCount(config *cephMonitorConfig) (int, error) {
	if config.monitor == "" && config.id == "" && config.key == "" {
		return 0, fmt.Errorf("Unable to get subvolume count as the monitor config is missing")
	}

	// considering the constants for the internal filesystem
	cephfilesystem := "ocs-storagecluster-cephfilesystem"
	subvolumeGroup := "csi"
	args := []string{
		"fs", "subvolume", "ls", cephfilesystem,
		"--group_name", subvolumeGroup,
		"--format", "json",
		"-m", config.monitor,
		"--id", config.id,
		"--key", config.key,
	}

	cmd, err := execCommand("ceph", args, 30)
	if err != nil {
		return 0, fmt.Errorf("failed to execute subvolume count command: %w", err)
	}

	if len(cmd) == 0 {
		return 0, nil
	}

	decoder := json.NewDecoder(strings.NewReader(string(cmd)))

	token, err := decoder.Token()
	if err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return 0, fmt.Errorf("expected JSON array, got %v", token)
	}

	count := 0
	for decoder.More() {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return 0, fmt.Errorf("failed to decode array element: %w", err)
		}
		count++
	}
	return count, nil
}
