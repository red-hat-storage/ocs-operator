package ceph

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ceph/go-ceph/rados"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	monEndpointConfigMap = "rook-ceph-mon-endpoints"
)

// k8sReader is the subset of the Kubernetes API needed to fetch credentials.
type k8sReader interface {
	getSecret(ctx context.Context, ns, name string) (*corev1.Secret, error)
	getConfigMap(ctx context.Context, ns, name string) (*corev1.ConfigMap, error)
}

// clientsetReader adapts a real clientset.Interface.
type clientsetReader struct {
	client clientset.Interface
}

func (r *clientsetReader) getSecret(ctx context.Context, ns, name string) (*corev1.Secret, error) {
	return r.client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
}

func (r *clientsetReader) getConfigMap(ctx context.Context, ns, name string) (*corev1.ConfigMap, error) {
	return r.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

type cephCredentials struct {
	userID   string
	userKey  string
	monitors string
}

type Conn struct {
	mu   sync.Mutex
	conn *rados.Conn
	k8s  k8sReader
	ns   string
}

func NewConn(opts *options.Options) (*Conn, error) {
	if len(opts.AllowedNamespaces) == 0 {
		return nil, fmt.Errorf("no allowed namespaces configured")
	}
	client, err := clientset.NewForConfig(opts.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return &Conn{
		k8s: &clientsetReader{client: client},
		ns:  opts.AllowedNamespaces[0],
	}, nil
}

// Get returns the current rados connection, connecting lazily on first call.
func (c *Conn) Get() (*rados.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn, nil
	}
	return c.connect()
}

// connect creates a new rados connection. Caller must hold mu.
func (c *Conn) connect() (*rados.Conn, error) {
	creds, err := c.fetchCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ceph credentials: %w", err)
	}

	conn, err := rados.NewConnWithUser(creds.userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create rados connection: %w", err)
	}

	ok := false
	defer func() {
		if !ok {
			conn.Shutdown()
		}
	}()

	if err := c.configureConn(conn, creds); err != nil {
		return nil, err
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to ceph: %w", err)
	}

	ok = true
	klog.Info("connected to ceph cluster")
	c.conn = conn
	return conn, nil
}

// configureConn sets timeouts, reads config, and applies credentials.
func (c *Conn) configureConn(conn *rados.Conn, creds *cephCredentials) error {
	// Read default config first so programmatic overrides take precedence.
	if err := conn.ReadDefaultConfigFile(); err != nil {
		klog.Warningf("failed to read default ceph config (continuing): %v", err)
	}

	for _, opt := range []struct{ key, val string }{
		{"rados_osd_op_timeout", "30"},
		{"rados_mon_op_timeout", "30"},
		{"client_mount_timeout", "30"},
		{"mon_host", creds.monitors},
		{"key", creds.userKey},
	} {
		if err := conn.SetConfigOption(opt.key, opt.val); err != nil {
			return fmt.Errorf("failed to set %s: %w", opt.key, err)
		}
	}

	return nil
}

// Close shuts down the current connection.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Shutdown()
		c.conn = nil
	}
}

// Reconnect tears down the current connection so the next Get rebuilds it.
// Concurrent users of the old connection will see errors on in-flight
// operations, which is acceptable because Reconnect is only called when
// the connection is already failing.
func (c *Conn) Reconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Shutdown()
		c.conn = nil
		klog.Info("ceph connection reset, will reconnect on next use")
	}
}

// IOContext opens an IO context for the given pool and optional namespace.
// The caller must call Destroy on the returned IOContext when done.
func (c *Conn) IOContext(pool, namespace string) (*rados.IOContext, error) {
	conn, err := c.Get()
	if err != nil {
		return nil, fmt.Errorf("pool %s: %w", pool, err)
	}

	ioctx, err := conn.OpenIOContext(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to open IO context for pool %s: %w", pool, err)
	}

	if namespace != "" {
		ioctx.SetNamespace(namespace)
	}
	return ioctx, nil
}

// fetchCredentials reads auth credentials from the ceph-auth secret and
// monitor addresses from the rook-ceph-mon-endpoints ConfigMap.
func (c *Conn) fetchCredentials() (*cephCredentials, error) {
	secret, err := c.k8s.getSecret(context.Background(), c.ns, util.OcsMetricsExporterCephClientName)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", c.ns, util.OcsMetricsExporterCephClientName, err)
	}

	id, ok := secret.Data["userID"]
	if !ok || len(id) == 0 {
		return nil, fmt.Errorf("userID not found in secret %s/%s", c.ns, util.OcsMetricsExporterCephClientName)
	}
	key, ok := secret.Data["userKey"]
	if !ok || len(key) == 0 {
		return nil, fmt.Errorf("userKey not found in secret %s/%s", c.ns, util.OcsMetricsExporterCephClientName)
	}

	configmap, err := c.k8s.getConfigMap(context.Background(), c.ns, monEndpointConfigMap)
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap %s/%s: %w", c.ns, monEndpointConfigMap, err)
	}

	data, ok := configmap.Data["data"]
	if !ok || data == "" {
		return nil, fmt.Errorf("data not found in configmap %s/%s", c.ns, monEndpointConfigMap)
	}

	monitors := parseMonEndpoints(data)
	if monitors == "" {
		return nil, fmt.Errorf("no monitor addresses in configmap %s/%s", c.ns, monEndpointConfigMap)
	}

	return &cephCredentials{
		userID:   string(id),
		userKey:  string(key),
		monitors: monitors,
	}, nil
}

// parseMonEndpoints extracts IP:port pairs from the rook-ceph-mon-endpoints
// data field. Format: "a=10.0.0.1:6789,b=10.0.0.2:6789"
func parseMonEndpoints(data string) string {
	entries := strings.Split(data, ",")
	addrs := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if idx := strings.Index(entry, "="); idx >= 0 {
			addrs = append(addrs, entry[idx+1:])
		}
	}
	return strings.Join(addrs, ",")
}
