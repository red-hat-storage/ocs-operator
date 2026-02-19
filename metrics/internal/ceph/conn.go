package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/ceph/go-ceph/rados"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// Conn wraps a go-ceph rados connection with lazy initialization and
// reconnection. It reads credentials from K8s secrets and monitor
// addresses from the rook-ceph-csi-config configmap at connect time.
type Conn struct {
	mu       sync.Mutex
	conn     *rados.Conn
	client   clientset.Interface
	ns       string // ceph cluster namespace (e.g. openshift-storage)
	authNs   string // namespace where ceph auth configmap lives
	userID   string
	userKey  string
	monitors string
}

type csiClusterConfig struct {
	ClusterID string   `json:"clusterID"`
	Monitors  []string `json:"monitors"`
	Namespace string   `json:"namespace"`
}

const (
	cephAuthSecretName = "ocs-metrics-exporter-ceph-auth"
	csiConfigMapName   = "rook-ceph-csi-config"
)

func NewConn(opts *options.Options) *Conn {
	return &Conn{
		client: clientset.NewForConfigOrDie(opts.Kubeconfig),
		ns:     opts.AllowedNamespaces[0],
		authNs: opts.CephAuthNamespace,
	}
}

// Get returns a connected rados.Conn, creating or reconnecting as needed.
func (c *Conn) Get() (*rados.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn, nil
	}

	if err := c.fetchCredentials(); err != nil {
		return nil, fmt.Errorf("failed to fetch ceph credentials: %w", err)
	}

	conn, err := rados.NewConnWithUser(c.userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create rados connection: %w", err)
	}

	for _, opt := range []struct{ key, val string }{
		{"rados_osd_op_timeout", "30"},
		{"rados_mon_op_timeout", "30"},
		{"client_mount_timeout", "30"},
	} {
		if err := conn.SetConfigOption(opt.key, opt.val); err != nil {
			conn.Shutdown()
			return nil, fmt.Errorf("failed to set %s: %w", opt.key, err)
		}
	}

	if err := conn.ReadDefaultConfigFile(); err != nil {
		conn.Shutdown()
		return nil, fmt.Errorf("failed to read ceph config: %w", err)
	}

	if err := conn.SetConfigOption("mon_host", c.monitors); err != nil {
		conn.Shutdown()
		return nil, fmt.Errorf("failed to set mon_host: %w", err)
	}

	if err := conn.SetConfigOption("key", c.userKey); err != nil {
		conn.Shutdown()
		return nil, fmt.Errorf("failed to set key: %w", err)
	}

	if err := conn.Connect(); err != nil {
		conn.Shutdown()
		return nil, fmt.Errorf("failed to connect to ceph: %w", err)
	}

	klog.Info("connected to ceph cluster")
	c.conn = conn
	return c.conn, nil
}

// Close shuts down the connection.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Shutdown()
		c.conn = nil
	}
}

// Reconnect forces a new connection on the next Get call.
func (c *Conn) Reconnect() {
	c.Close()
}

func (c *Conn) fetchCredentials() error {
	secret, err := c.client.CoreV1().Secrets(c.ns).Get(
		context.TODO(), cephAuthSecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", c.ns, cephAuthSecretName, err)
	}

	id, ok := secret.Data["userID"]
	if !ok {
		return fmt.Errorf("userID not found in secret %s/%s", c.ns, cephAuthSecretName)
	}
	key, ok := secret.Data["userKey"]
	if !ok {
		return fmt.Errorf("userKey not found in secret %s/%s", c.ns, cephAuthSecretName)
	}
	c.userID = string(id)
	c.userKey = string(key)

	configmap, err := c.client.CoreV1().ConfigMaps(c.authNs).Get(
		context.TODO(), csiConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configmap %s/%s: %w", c.authNs, csiConfigMapName, err)
	}

	data, ok := configmap.Data["csi-cluster-config-json"]
	if !ok {
		return fmt.Errorf("csi-cluster-config-json not found in configmap %s/%s", c.authNs, csiConfigMapName)
	}

	var configs []csiClusterConfig
	if err := json.Unmarshal([]byte(data), &configs); err != nil {
		return fmt.Errorf("failed to parse csi cluster config: %w", err)
	}

	for _, cfg := range configs {
		if cfg.Namespace == c.ns && len(cfg.Monitors) > 0 {
			c.monitors = strings.Join(cfg.Monitors, ",")
			return nil
		}
	}

	return fmt.Errorf("no monitor config found for namespace %s", c.ns)
}
