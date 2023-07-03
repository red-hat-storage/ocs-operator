package collectors

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ceph/go-ceph/rgw/admin"
	libbucket "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	bktclient "github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/clientset/versioned/fake"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

/*
const (
getUserJSON = )
*/
var (
	mockObjStore = "mock-store"
	nameSpace    = "openshift-storage"
	port         = 1234
	host         = fmt.Sprintf("rook-ceph-rgw-%s.%s.svc", mockObjStore, nameSpace)
	bucketName   = "mockbkt"
	endpoint     = fmt.Sprintf("http://%s:%d", host, port)

	mockEndpointInfo = map[string]string{
		"endpoint": endpoint}

	conn = &libbucket.Connection{
		Endpoint: &libbucket.Endpoint{
			BucketHost: host,
			BucketPort: int(port),
			BucketName: bucketName,
		},
		AdditionalState: map[string]string{
			"cephUser": "mock-ceph-user"},
	}
	mockCephObjectStore = cephv1.CephObjectStore{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ceph.rook.io/v1",
			Kind:       "CephObjectStore",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      mockObjStore,
			Namespace: nameSpace,
		},
		Spec: cephv1.ObjectStoreSpec{},
		Status: &cephv1.ObjectStoreStatus{
			Phase: cephv1.ConditionConnected,
			Info:  mockEndpointInfo,
		},
	}
	mockObjectBucket = libbucket.ObjectBucket{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "objectbucket.io/v1alpha1",
			Kind:       "ObjectBucket",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "obc-mockOB",
			Labels: map[string]string{
				"bucket-provisioner": fmt.Sprintf("%s.%s", nameSpace, bucketProvisionerName),
			},
		},
		Spec: libbucket.ObjectBucketSpec{
			StorageClassName: "mock-sc",
			Connection:       conn,
			ClaimRef: &corev1.ObjectReference{
				Name: "mockOB",
			},
		},
	}
)

func getMockObjectBucketCollector(t *testing.T, mockOpts *options.Options) (mockOBCollector *ObjectBucketCollector) {
	setKubeConfig(t)
	mockOBCollector = NewObjectBucketCollector(mockOpts)
	assert.NotNil(t, mockOBCollector)
	return
}

func TestNewObjectBucketCollector(t *testing.T) {
	tests := Tests{
		{
			name: "Test NewObjectBucketCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockObjectBucketCollector(t, tt.args.opts)
		assert.NotNil(t, got.AllowedNamespaces)
		assert.NotNil(t, got.Informer)
	}
}

func TestGetAllObjectBuckets(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)
	obCollector := getMockObjectBucketCollector(t, mockOpts)
	obCollector.bktclient = bktclient.NewSimpleClientset()

	// Creating different samples for objectbuckets in the setup
	cephOB := mockObjectBucket.DeepCopy()
	cephOB.Name = cephOB.Name + "ceph"
	cephOBdiffEndpoint := mockObjectBucket.DeepCopy()
	cephOBdiffEndpoint.Name = cephOB.Name + "different"
	cephOBdiffEndpoint.Spec.Connection.Endpoint.BucketHost = "different-endpoint"
	noobaaOB := mockObjectBucket.DeepCopy()
	noobaaOB.Name = noobaaOB.Name + "noobaa"
	noobaaOB.Labels["bucket-provisioner"] = "noobaa.io-bucket"

	tests := Tests{
		{
			name:         "No object buckets",
			inputObjects: []runtime.Object{},
			wantObjects:  []runtime.Object(nil),
		},
		{
			name: "One valid object buckets",
			inputObjects: []runtime.Object{
				cephOB,
				cephOBdiffEndpoint,
				noobaaOB,
			},
			wantObjects: []runtime.Object{
				cephOB,
			},
		},
	}
	for _, tt := range tests {
		createOBs(t, tt.inputObjects, obCollector.bktclient)
		gotObjectBuckets := obCollector.getAllObjectBuckets(mockCephObjectStore)
		assert.Len(t, gotObjectBuckets, len(tt.wantObjects))
		for _, obj := range gotObjectBuckets {
			assert.Contains(t, tt.wantObjects, &obj)
		}
		deleteOBs(t, tt.inputObjects, obCollector.bktclient)
	}
}

func TestCollectObjectBucketMetrics(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	obCollector := getMockObjectBucketCollector(t, mockOpts)
	obCollector.bktclient = bktclient.NewSimpleClientset()

	mockClient := &MockClient{
		MockDo: func(req *http.Request) (*http.Response, error) {
			userJSONFile, err := os.Open("testuserdata.json")
			if err != nil {
				return nil, err
			}
			defer userJSONFile.Close()
			byteValue, err := io.ReadAll(userJSONFile)
			if err != nil {
				return nil, err
			}
			if req.URL.RawQuery == "format=json&stats=true&uid=mock-ceph-user" && req.Method == http.MethodGet && req.URL.Path == "/admin/user" {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(byteValue)),
				}, nil
			}
			return nil, fmt.Errorf("unexpected request: %q. method %q. path %q", req.URL.RawQuery, req.Method, req.URL.Path)
		},
	}

	adminClient, err := admin.New(endpoint, "53S6B9S809NUP19IJ2K3", "1bXPegzsGClvoGAiJdHQD1uOW2sQBLAZM9j9VtXR", mockClient)
	assert.NoError(t, err)

	tests := Tests{
		{
			name: "Collect objectbucket usage metrics",
			args: args{
				objects: []runtime.Object{
					&mockObjectBucket,
				},
			},
		},
	}
	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			createOBs(t, tt.args.objects, obCollector.bktclient)
			obCollector.collectObjectBucketMetrics(mockCephObjectStore, adminClient, ch)
			close(ch)
		}()
		for m := range ch {
			metric.Reset()
			err := m.Write(&metric)
			assert.Nil(t, err)
			labels := metric.GetLabel()
			for _, label := range labels {
				if *label.Name == "objectbucket" {
					assert.Equal(t, *label.Value, mockObjectBucket.Name)
				} else if *label.Name == "objectbucketclaim" {
					assert.Equal(t, *label.Value, mockObjectBucket.Spec.ClaimRef.Name)
				} else if *label.Name == "storageclass" {
					assert.Equal(t, *label.Value, mockObjectBucket.Spec.StorageClassName)
				} else if *label.Name == "object_store" {
					assert.Equal(t, *label.Value, mockCephObjectStore.Name)
				}
			}

			if strings.Contains(m.Desc().String(), "ocs_objectbucket_used_bytes") {
				assert.Equal(t, *metric.Gauge.Value, float64(0))
			} else if strings.Contains(m.Desc().String(), "ocs_objectbucket_max_bytes") {
				assert.Equal(t, *metric.Gauge.Value, float64(1000000))
			} else if strings.Contains(m.Desc().String(), "ocs_objectbucket_objects_total") {
				assert.Equal(t, *metric.Gauge.Value, float64(0))
			} else if strings.Contains(m.Desc().String(), "ocs_objectbucket_max_objects") {
				assert.Equal(t, *metric.Gauge.Value, float64(1000))
			} else if strings.Contains(m.Desc().String(), "ocs_objectbucketclaim_info") {
				assert.Equal(t, *metric.Gauge.Value, float64(1))
			}
		}
		deleteOBs(t, tt.args.objects, obCollector.bktclient)
	}
}
