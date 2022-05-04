package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
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

type RBDMirrorPeerSiteDescription struct {
	BytesPerSecond          float64 `json:"bytes_per_second"`
	BytesPerSnapshot        float64 `json:"bytes_per_snapshot"`
	LocalSnapshotTimestamp  int64   `json:"local_snapshot_timestamp"`
	RemoteSnapshotTimestamp int64   `json:"remote_snapshot_timestamp"`
	ReplayState             string  `json:"replay_state"`
}

// Cache mirror data for all CephBlockPools with mirroring enabled

var _ cache.Store = &RBDMirrorStore{}

// RBDMirrorStore implements the k8s.io/client-go/tools/cache.Store
// interface. It stores rbd mirror data.
type RBDMirrorStore struct {
	Mutex sync.RWMutex
	// Store is a map of Pool UID to RBDMirrorPoolStatusVerbose
	Store           map[types.UID]RBDMirrorPoolStatusVerbose
	rbdCommandInput rbdCommandInput
}

func NewRBDMirrorStore() *RBDMirrorStore {
	return &RBDMirrorStore{
		Store:           map[types.UID]RBDMirrorPoolStatusVerbose{},
		rbdCommandInput: rbdCommandInput{},
	}
}

func (s *RBDMirrorStore) WithRBDCommandInput(monitor, id, key string) {
	s.rbdCommandInput.monitor = monitor
	s.rbdCommandInput.id = id
	s.rbdCommandInput.key = key
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

	mirrorStatus, err := s.rbdCommandInput.rbdImageStatus(pool.Name)
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

func (s *RBDMirrorStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

func (s *RBDMirrorStore) GetByKey(key string) (item interface{}, exists bool, err error) {
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
		mirrorStatus, err := s.rbdCommandInput.rbdImageStatus(poolStatusVerbose.PoolName)
		if err != nil {
			klog.Errorf("unable to collect RBD mirror data for CephBlockPool %q in %q namespace", poolStatusVerbose.PoolName, poolStatusVerbose.PoolNamespace)
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

type rbdCommandInput struct {
	monitor, id, key string
}

func (in *rbdCommandInput) rbdImageStatus(poolName string) (RBDMirrorStatusVerbose, error) {
	var cmd []byte
	var rbdMirrorStatusVerbose RBDMirrorStatusVerbose

	if in.monitor == "" && in.id == "" && in.key == "" {
		return rbdMirrorStatusVerbose, errors.New("unable to get RBD mirror data. RBD command input not specified")
	}

	args := []string{"mirror", "pool", "status", poolName, "--verbose", "--format", "json", "-m", in.monitor, "--id", in.id, "--key", in.key}
	cmd, err := execCommand("rbd", args)
	if err != nil {
		return rbdMirrorStatusVerbose, err
	}

	err = json.Unmarshal(cmd, &rbdMirrorStatusVerbose)

	return rbdMirrorStatusVerbose, err
}

func execCommand(command string, args []string) ([]byte, error) {
	cmd := exec.Command(command, args...)
	return cmd.CombinedOutput()
}
