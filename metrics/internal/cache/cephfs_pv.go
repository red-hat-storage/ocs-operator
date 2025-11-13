package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// Cache metadata for all PV with annotation - pv.kubernetes.io/provisioned-by: *.cephfs.csi.ceph.com

var _ cache.Store = &CephFSPVStore{}

type CephFSPVAttributes struct {
	PersistentVolumeName string
}

type CephFSPVStore struct {
	Mutex sync.RWMutex
	Store map[types.UID]CephFSPVAttributes
	// CephFSSubvolumeCountMap is a map of "subvolumegroup" to subvolume count
	CephFSSubvolumeCountMap   map[string]int
	monitorConfig             cephMonitorConfig
	kubeClient                clientset.Interface
	rookClient                rookclient.Interface
	cephClusterNamespace      string
	cephAuthNamespace         string
	runCephfsSubvolumeCountFn func(config cephMonitorConfig, rookClient rookclient.Interface, cephClusterNamespace string) (map[string]int, error)
}

func NewCephFSPVStore(opts *options.Options) *CephFSPVStore {
	return &CephFSPVStore{
		Store:                     map[types.UID]CephFSPVAttributes{},
		CephFSSubvolumeCountMap:   make(map[string]int),
		kubeClient:                clientset.NewForConfigOrDie(opts.Kubeconfig),
		rookClient:                rookclient.NewForConfigOrDie(opts.Kubeconfig),
		cephClusterNamespace:      opts.AllowedNamespaces[0],
		cephAuthNamespace:         opts.CephAuthNamespace,
		monitorConfig:             cephMonitorConfig{},
		runCephfsSubvolumeCountFn: runCephFSSubvolumeCount,
	}
}

// Add inserts to the PersistentVolumeStore.
func (p *CephFSPVStore) Add(obj interface{}) error {
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
	klog.Infof("PV store addition completed at %v for PV %v", time.Now(), pv.Name)

	return nil
}

// add is not thread-safe. So, it must to be called from a thread safe function only.
func (p *CephFSPVStore) add(pv *corev1.PersistentVolume) error {
	provisioner, exists := pv.Annotations["pv.kubernetes.io/provisioned-by"]

	if !exists || !strings.Contains(provisioner, ".cephfs.csi.ceph.com") {
		klog.Infof("Skipping non Ceph CSI CephFS volume %s", pv.Name)
		return nil
	}

	p.Store[pv.GetUID()] = CephFSPVAttributes{
		PersistentVolumeName: pv.Name,
	}

	return nil
}

// Resync refreshes the cached filesystem name and subvolume count from Ceph
func (c *CephFSPVStore) Resync() error {
	klog.Infof("PV store Resync started at %v", time.Now())

	pvList, err := c.kubeClient.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list persistent volumes: %v", err)
	}

	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	for _, pv := range pvList.Items {
		err := c.add(&pv)
		if err != nil {
			return fmt.Errorf("failed to process PV: %s err: %v", pv.Name, err)
		}
	}
	klog.Infof("PV store Resync ended at %v", time.Now())

	subvolGroupCounts, err := c.runCephfsSubvolumeCountFn(c.monitorConfig, c.rookClient, c.cephClusterNamespace)
	if err != nil {
		klog.Errorf("failed to get CephFS subvolume counts: %v", err)
	} else {
		klog.Infof("CephFS subvolumegroup counts: %v", subvolGroupCounts)
		c.CephFSSubvolumeCountMap = subvolGroupCounts
	}

	return nil
}

// Update updates the existing entry in the PersistentVolumeStore.
func (c *CephFSPVStore) Update(obj interface{}) error {
	return c.Add(obj)
}

func (c *CephFSPVStore) Delete(obj interface{}) error {
	o, err := meta.Accessor(obj)
	if err != nil {
		return err
	}
	c.Mutex.Lock()
	delete(c.Store, o.GetUID()) // ADD THIS
	c.Mutex.Unlock()
	return nil
}

func (c *CephFSPVStore) List() []interface{} {
	return nil
}

func (c *CephFSPVStore) ListKeys() []string {
	return nil
}

func (c *CephFSPVStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

func (c *CephFSPVStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

func (c *CephFSPVStore) Replace(list []interface{}, resourceVersion string) error {
	c.Mutex.Lock()
	c.Store = map[types.UID]CephFSPVAttributes{}
	c.Mutex.Unlock()

	for _, o := range list {
		err := c.Add(o)
		if err != nil {
			return err
		}
	}

	subvolGroupCounts, err := c.runCephfsSubvolumeCountFn(c.monitorConfig, c.rookClient, c.cephClusterNamespace)
	if err != nil {
		klog.Errorf("failed to get CephFS subvolume counts during Replace: %v", err)
	} else {
		klog.Infof("CephFS subvolumegroup counts during Replace: %v", subvolGroupCounts)
		c.CephFSSubvolumeCountMap = subvolGroupCounts
	}

	return nil
}

func runCephFSSubvolumeCount(config cephMonitorConfig, rookClient rookclient.Interface, cephClusterNamespace string) (map[string]int, error) {
	// 1. Retrieve the filesystem
	cephFilesystems, err := rookClient.CephV1().CephFilesystems(cephClusterNamespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list CephFilesystem CRs: %w", err)
	}

	if len(cephFilesystems.Items) == 0 {
		return nil, fmt.Errorf("no CephFilesystem CRs found")
	}

	fsName := cephFilesystems.Items[0].Name

	// 2. Initialize Ceph monitor config if needed
	if (config == cephMonitorConfig{}) {
		return nil, fmt.Errorf("ceph monitor config is required")
	}

	// 3. Retrieve all subvolumegroups for this filesystem
	groups, err := runCephFSSubvolumeGroups(config, fsName)
	if err != nil {
		return nil, fmt.Errorf("failed to get subvolumegroups: %w", err)
	}

	// 4. Retrieve the number of subvolumes per subvolumegroup
	result := make(map[string]int)
	for _, groupName := range groups {
		count, err := runCephFSSubvolumeCountPerGroup(config, fsName, groupName)
		if err != nil {
			klog.Errorf("Failed to get subvolume count for %s/%s: %v", fsName, groupName, err)
			continue
		}
		result[groupName] = count
	}

	return result, nil
}

// runCephFSSubvolumeGroups gets the list of subvolumegroup names for a filesystem
func runCephFSSubvolumeGroups(config cephMonitorConfig, fsName string) ([]string, error) {
	if config.monitor == "" && config.id == "" && config.key == "" {
		return nil, fmt.Errorf("unable to get subvolumegroup list. monitor config missing")
	}

	args := []string{
		"fs", "subvolumegroup", "ls", fsName,
		"--format", "json",
		"-m", config.monitor,
		"--id", config.id,
		"--key", config.key,
	}

	cmd, err := execCommand("ceph", args, 30)
	if err != nil {
		return nil, fmt.Errorf("failed to execute ceph fs subvolumegroup ls: %w", err)
	}

	if len(cmd) == 0 {
		return []string{}, nil
	}

	var groups []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(cmd, &groups); err != nil {
		return nil, fmt.Errorf("failed to parse subvolumegroup list JSON: %v, output: %q", err, string(cmd))
	}

	groupNames := make([]string, 0, len(groups))
	for _, group := range groups {
		groupNames = append(groupNames, group.Name)
	}

	return groupNames, nil
}

// runCephFSSubvolumeCountSingle gets the count of subvolumes in a specific subvolumegroup
func runCephFSSubvolumeCountPerGroup(config cephMonitorConfig, fsName, groupName string) (int, error) {
	if config.monitor == "" && config.id == "" && config.key == "" {
		return 0, fmt.Errorf("unable to get subvolume count. monitor config missing")
	}

	args := []string{
		"fs", "subvolume", "ls", fsName,
		"--group_name", groupName,
		"--format", "json",
		"-m", config.monitor,
		"--id", config.id,
		"--key", config.key,
	}

	cmd, err := execCommand("ceph", args, 30)
	if err != nil {
		return 0, fmt.Errorf("failed to execute ceph fs subvolume ls: %w", err)
	}

	if len(cmd) == 0 {
		return 0, nil
	}

	var subvolumes []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(cmd, &subvolumes); err != nil {
		return 0, fmt.Errorf("failed to parse subvolume list JSON: %v, output: %q", err, string(cmd))
	}

	return len(subvolumes), nil
}
