package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type CephBlocklistLs struct {
	IP    string    `json:"ip"`
	Port  int       `json:"port"`
	Nonce int       `json:"nonce"`
	Until time.Time `json:"until"`
}

func (c *CephBlocklistLs) UnmarshalJSON(data []byte) error {
	var blocklistData struct {
		Addr  string `json:"addr"`
		Until string `json:"until"`
	}
	if err := json.Unmarshal(data, &blocklistData); err != nil {
		return fmt.Errorf("failed to unmarshal CephBlocklistLs: %v", err)
	}
	c.Until, _ = time.Parse(time.RFC3339Nano, blocklistData.Until)
	re := regexp.MustCompile(`^(\d+\.\d+\.\d+\.\d+):(\d+)/(\d+)$`)
	match := re.FindStringSubmatch(blocklistData.Addr)
	if len(match) != 4 {
		return fmt.Errorf("failed to extract IP, port, and nonce from address %s, expected format <IP>:<port>/<nonce>", blocklistData.Addr)
	}
	c.IP = match[1]
	port, err := strconv.Atoi(match[2])
	if err != nil {
		return fmt.Errorf("failed to convert port to integer: %v", err)
	}
	c.Port = port
	nonce, err := strconv.Atoi(match[3])
	if err != nil {
		return fmt.Errorf("failed to convert nonce to integer: %v", err)
	}
	c.Nonce = nonce
	return nil
}

type CephBlocklistStore struct {
	Mutex                sync.RWMutex
	Store                []CephBlocklistLs
	monitorConfig        cephMonitorConfig
	kubeClient           clientset.Interface
	cephClusterNamespace string
	cephAuthNamespace    string
}

var _ cache.Store = &CephBlocklistStore{}

func NewCephBlocklistStore(opts *options.Options) *CephBlocklistStore {
	return &CephBlocklistStore{
		Store:                []CephBlocklistLs{},
		kubeClient:           clientset.NewForConfigOrDie(opts.Kubeconfig),
		monitorConfig:        cephMonitorConfig{},
		cephClusterNamespace: opts.AllowedNamespaces[0],
		cephAuthNamespace:    opts.CephAuthNamespace,
	}
}

func (c *CephBlocklistStore) Add(_ interface{}) error {
	return c.Resync()
}

func (c *CephBlocklistStore) Get(_ interface{}) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

func (c *CephBlocklistStore) List() []interface{} {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()

	list := make([]interface{}, len(c.Store))
	for i, blocklist := range c.Store {
		list[i] = blocklist
	}

	return list
}

func (c *CephBlocklistStore) ListKeys() []string {
	return nil
}

func (c *CephBlocklistStore) Replace(_ []interface{}, _ string) error {
	return nil
}

func (c *CephBlocklistStore) GetByKey(_ string) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

func (c *CephBlocklistStore) Resync() error {
	klog.Infof("Blocklist store sync started %v", time.Now())

	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	if (c.monitorConfig == cephMonitorConfig{}) {
		var err error
		c.monitorConfig, err = initCeph(c.kubeClient, c.cephClusterNamespace, c.cephAuthNamespace)
		if err != nil {
			return fmt.Errorf("failed to initialize ceph: %v", err)
		}
	}

	blockList, err := runCephOSDBlocklist(&c.monitorConfig)
	if err != nil {
		klog.Errorf("failed to get blocklist data: %v", err)
		if err == context.DeadlineExceeded {
			klog.Errorf("re-creating ceph client, command timedout")
			c.monitorConfig, err = initCeph(c.kubeClient, c.cephClusterNamespace, c.cephAuthNamespace)
			if err != nil {
				return fmt.Errorf("failed to recreate ceph client, %v", err)
			}
		}
		// returning an error here will break everything
		return nil
	}

	c.Store = blockList
	klog.Infof("Blocklist store sync completed at %v", time.Now())
	return nil
}

func (c *CephBlocklistStore) Delete(_ interface{}) error {
	return nil
}

func (c *CephBlocklistStore) Update(obj interface{}) error {
	return c.Add(obj)
}

func (c *CephBlocklistStore) IsBlocked(ip string, port, nonce int) bool {
	// Check if the RBD client for this node is blocklisted
	for _, blocklist := range c.Store {
		if blocklist.IP == ip && blocklist.Port == port && (blocklist.Nonce == 0 || blocklist.Nonce == nonce) {
			return true
		}
	}
	return false
}

func runCephOSDBlocklist(config *cephMonitorConfig) ([]CephBlocklistLs, error) {
	var blocklistSlice []CephBlocklistLs
	if config.monitor == "" && config.id == "" && config.key == "" {
		return blocklistSlice, errors.New("unable to get blocklist data. monitor config missing")
	}

	args := []string{"osd", "blocklist", "ls", "--format", "json", "-m", config.monitor, "--id", config.id, "--key", config.key}
	cmd, err := execCommand("ceph", args, 30)
	if err != nil {
		return blocklistSlice, err
	}

	re := regexp.MustCompile(`\[[^\[\]]+\]`)
	match := re.Find(cmd)
	if err != nil {
		return blocklistSlice, fmt.Errorf("failed to extract JSON from input: %v", err)
	}

	if len(match) == 0 {
		return blocklistSlice, nil
	}

	err = json.Unmarshal(match, &blocklistSlice)
	if err != nil {
		return blocklistSlice, fmt.Errorf("failed to extract JSON from command output: %v", err)
	}
	return blocklistSlice, nil
}
