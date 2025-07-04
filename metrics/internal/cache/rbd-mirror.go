package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type RBDMirrorPoolStatusVerbose struct {
	PoolName      string
	PoolNamespace string
	MirrorStatus  RBDMirrorStatusVerbose
}
type RBDMirrorStatusVerbose struct {
	Summary RBDMirrorPoolStatusSummary `json:"summary"`
	Daemons []RBDMirrorDaemonStatus    `json:"daemons"`
	Images  []RBDMirrorImageStatus     `json:"images"`
}

type RBDMirrorPoolStatusSummary struct {
	Health       string                     `json:"health"`
	DaemonHealth string                     `json:"daemon_health"`
	ImageHealth  string                     `json:"image_health"`
	States       RBDMirrorImageStatusStates `json:"states"`
}

type RBDMirrorImageStatusStates struct {
	Unknown        int `json:"unknown"`
	Error          int `json:"error"`
	Syncing        int `json:"syncing"`
	StartingReplay int `json:"starting_replay"`
	Replaying      int `json:"replaying"`
	StoppingReplay int `json:"stopping_replay"`
	Stopped        int `json:"stopped"`
}

type RBDMirrorDaemonStatus struct {
	ServiceID   string `json:"service_id"`
	InstanceID  string `json:"instance_id"`
	ClientID    string `json:"client_id"`
	Hostname    string `json:"hostname"`
	CephVersion string `json:"ceph_version"`
	Leader      bool   `json:"leader"`
	Health      string `json:"health"`
}

type RBDMirrorImageStatus struct {
	Name          string                 `json:"name"`
	GlobalID      string                 `json:"global_id"`
	State         string                 `json:"state"`
	Description   string                 `json:"description"`
	DaemonService RBDMirrorDaemonService `json:"daemon_service"`
	LastUpdate    string                 `json:"last_update"`
	PeerSites     []RBDMirrorPeerSite    `json:"peer_sites"`
}

type RBDMirrorDaemonService struct {
	ServiceID  string `json:"service_id"`
	InstanceID string `json:"instance_id"`
	DaemonID   string `json:"daemon_id"`
	Hostname   string `json:"hostname"`
}

type RBDMirrorPeerSite struct {
	SiteName    string `json:"site_name"`
	MirrorUuids string `json:"mirror_uuids"`
	State       string `json:"state"`
	Description string `json:"description"`
	LastUpdate  string `json:"last_update"`
}

type csiClusterConfig struct {
	ClusterID string   `json:"clusterID"`
	Monitors  []string `json:"monitors"`
	Namespace string   `json:"namespace"`
}

// Cache mirror data for all CephBlockPools with mirroring enabled

var _ cache.Store = &RBDMirrorStore{}

// RBDMirrorStore implements the k8s.io/client-go/tools/cache.Store
// interface. It stores rbd mirror data.
type RBDMirrorStore struct {
	Mutex sync.RWMutex
	// Store is a map of Pool UID to RBDMirrorPoolStatusVerbose
	Store map[types.UID]RBDMirrorPoolStatusVerbose
	// rbdCommandInput is a struct that contains the input for the rbd command
	// for each AllowdNamespaces
	rbdCommandInput      map[string]*cephMonitorConfig
	kubeclient           clientset.Interface
	cephClusterNamespace string
	cephAuthNamespace    string
	// Functions to make testing easier
	initCephFn       func(kubeclient clientset.Interface, cephClusterNamespace, cephAuthNamespace string) (cephMonitorConfig, error)
	rbdImageStatusFn func(config *cephMonitorConfig, poolName string) (RBDMirrorStatusVerbose, error)
}

func NewRBDMirrorStore(opts *options.Options) *RBDMirrorStore {
	return &RBDMirrorStore{
		Store:                map[types.UID]RBDMirrorPoolStatusVerbose{},
		rbdCommandInput:      map[string]*cephMonitorConfig{},
		kubeclient:           clientset.NewForConfigOrDie(opts.Kubeconfig),
		cephClusterNamespace: opts.AllowedNamespaces[0],
		cephAuthNamespace:    opts.CephAuthNamespace,
		initCephFn:           initCeph,
		rbdImageStatusFn:     rbdImageStatus,
	}
}

func (s *RBDMirrorStore) WithRBDCommandInput(namespace string) error {
	input, err := s.initCephFn(s.kubeclient, namespace, s.cephAuthNamespace)
	if err != nil {
		return err
	}

	s.rbdCommandInput[namespace] = &input

	return nil
}

func initCeph(kubeclient clientset.Interface, cephClusterNamespace, cephAuthNamespace string) (cephMonitorConfig, error) {
	var err error
	var namespace string
	var secret *corev1.Secret

	secret, err = kubeclient.CoreV1().Secrets(cephClusterNamespace).Get(context.TODO(), util.OcsMetricsExporterCephClientName, metav1.GetOptions{})
	if err != nil && !apierror.IsNotFound(err) {
		return cephMonitorConfig{}, err
	}

	if apierror.IsNotFound(err) {
		secret, err = kubeclient.CoreV1().Secrets(cephClusterNamespace).Get(context.TODO(), "rook-csi-rbd-provisioner", metav1.GetOptions{})
		if err != nil {
			return cephMonitorConfig{}, err
		}
	}

	id, ok := secret.Data["userID"]
	if !ok {
		return cephMonitorConfig{}, fmt.Errorf("failed to get ceph user id in namespace %q", namespace)
	}

	key, ok := secret.Data["userKey"]
	if !ok {
		return cephMonitorConfig{}, fmt.Errorf("failed to get ceph user key in namespace %q", namespace)
	}

	configmap, err := kubeclient.CoreV1().ConfigMaps(cephAuthNamespace).Get(context.TODO(), "rook-ceph-csi-config", metav1.GetOptions{})
	if err != nil {
		return cephMonitorConfig{}, fmt.Errorf("failed to get configmap in namespace %q: %v", namespace, err)
	}

	data, ok := configmap.Data["csi-cluster-config-json"]
	if !ok {
		return cephMonitorConfig{}, fmt.Errorf("failed to get CSI cluster config from configmap in namespace %q", namespace)
	}

	var clusterConfigs []csiClusterConfig
	err = json.Unmarshal([]byte(data), &clusterConfigs)
	if err != nil {
		return cephMonitorConfig{}, fmt.Errorf("failed to unmarshal csi-cluster-config-json in namespace %q: %v", namespace, err)
	}

	if len(clusterConfigs) == 0 {
		return cephMonitorConfig{}, fmt.Errorf("expected 1 or more CSI cluster config but found 0 from configmap in namespace %q", namespace)
	}

	var clusterConfig csiClusterConfig
	for idx := range clusterConfigs {
		if clusterConfigs[idx].Namespace == cephClusterNamespace {
			clusterConfig = clusterConfigs[idx]
			break
		}
	}

	if len(clusterConfig.Monitors) == 0 {
		return cephMonitorConfig{}, fmt.Errorf("expected 1 or more monitors but found 0 from configmap in namespace %q", namespace)
	}

	input := cephMonitorConfig{}
	input.clusterID = clusterConfig.ClusterID
	input.cephClusterNamespace = clusterConfig.Namespace
	input.monitor = clusterConfig.Monitors[0]
	input.id = string(id)
	input.key = string(key)

	return input, nil
}

func (s *RBDMirrorStore) Add(obj interface{}) error {
	o, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	pool, ok := obj.(*cephv1.CephBlockPool)
	if !ok {
		return fmt.Errorf("unexpected object of type %T", obj)
	}

	if !pool.Spec.Mirroring.Enabled {
		klog.Infof("skipping rbd mirror status update for pool %s/%s because mirroring is disabled", pool.Namespace, pool.Name)
		return nil
	}

	if _, ok := s.rbdCommandInput[pool.Namespace]; !ok {
		err := s.WithRBDCommandInput(pool.Namespace)
		if err != nil {
			klog.Errorf("Failed to initialize rbd command input for pool %s/%s: %v", pool.Namespace, pool.Name, err)
			return fmt.Errorf("rbd command error for pool %s/%s : %v", pool.Namespace, pool.Name, err)
		}
	}

	mirrorStatus, err := s.rbdImageStatusFn(s.rbdCommandInput[pool.Namespace], pool.Name)
	if err != nil {
		return fmt.Errorf("rbd command error: %v", err)
	}

	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	s.Store[o.GetUID()] = RBDMirrorPoolStatusVerbose{
		PoolName:      pool.Name,
		PoolNamespace: pool.Namespace,
		MirrorStatus:  mirrorStatus,
	}

	return nil
}

func (s *RBDMirrorStore) Update(obj interface{}) error {
	return s.Add(obj)
}

func (s *RBDMirrorStore) Delete(obj interface{}) error {
	o, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	delete(s.Store, o.GetUID())

	return nil
}

func (s *RBDMirrorStore) List() []interface{} {
	return nil
}

func (s *RBDMirrorStore) ListKeys() []string {
	return nil
}

func (s *RBDMirrorStore) Get(_ interface{}) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

func (s *RBDMirrorStore) GetByKey(_ string) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

func (s *RBDMirrorStore) Replace(list []interface{}, _ string) error {
	s.Mutex.Lock()
	s.Store = map[types.UID]RBDMirrorPoolStatusVerbose{}
	s.Mutex.Unlock()

	for _, o := range list {
		err := s.Add(o)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *RBDMirrorStore) Resync() error {
	klog.Infof("RBD mirror store resync started at %v", time.Now())
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	for poolUUID, poolStatusVerbose := range s.Store {
		if _, ok := s.rbdCommandInput[poolStatusVerbose.PoolNamespace]; !ok {
			err := s.WithRBDCommandInput(poolStatusVerbose.PoolNamespace)
			if err != nil {
				klog.Errorf("Failed to initialize rbd command input for pool %s/%s: %v", poolStatusVerbose.PoolNamespace, poolStatusVerbose.PoolName, err)
				continue
			}
		}

		mirrorStatus, err := s.rbdImageStatusFn(s.rbdCommandInput[poolStatusVerbose.PoolNamespace], poolStatusVerbose.PoolName)
		if err != nil {
			klog.Errorf("rbd command error: %v", err)
			continue
		}

		s.Store[poolUUID] = RBDMirrorPoolStatusVerbose{
			PoolName:      poolStatusVerbose.PoolName,
			PoolNamespace: poolStatusVerbose.PoolNamespace,
			MirrorStatus:  mirrorStatus,
		}
	}
	klog.Infof("RBD mirror store resync ended at %v", time.Now())
	return nil
}

func CreateCephBlockPoolListWatch(cephClient rookclient.Interface, namespace, fieldSelector string) cache.ListerWatcher {
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.FieldSelector = fieldSelector
			return cephClient.CephV1().CephBlockPools(namespace).List(context.TODO(), opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = fieldSelector
			return cephClient.CephV1().CephBlockPools(namespace).Watch(context.TODO(), opts)
		},
	}
}

/* RBD CLI Commands */

type cephMonitorConfig struct {
	clusterID, cephClusterNamespace, monitor, id, key string
}

func rbdImageStatus(config *cephMonitorConfig, poolName string) (RBDMirrorStatusVerbose, error) {
	var cmd []byte
	var rbdMirrorStatusVerbose RBDMirrorStatusVerbose

	if config.monitor == "" && config.id == "" && config.key == "" {
		return rbdMirrorStatusVerbose, errors.New("unable to get RBD mirror data. RBD command input not specified")
	}

	args := []string{"mirror", "pool", "status", poolName, "--verbose", "--format", "json", "-m", config.monitor, "--id", config.id, "--key", config.key, "--debug-rbd", "0"}
	cmd, err := execCommand("rbd", args, 30)
	if err != nil {
		return rbdMirrorStatusVerbose, err
	}

	err = json.Unmarshal(cmd, &rbdMirrorStatusVerbose)

	return rbdMirrorStatusVerbose, err
}

func execCommand(command string, args []string, timeout int) ([]byte, error) {
	var cancel context.CancelFunc
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)

	output, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		klog.Errorf("command %v timedout in %d seconds", command, timeout)
		return output, ctx.Err()

	}
	return output, err
}
