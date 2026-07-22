package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	ifaces "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/interfaces"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReplaceMsgr1PortWithMsgr2(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no msgr1 port",
			input:    []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
			expected: []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
		},
		{
			name:     "all msgr1 ports",
			input:    []string{"192.168.1.1:6789", "192.168.1.2:6789", "192.168.1.3:6789"},
			expected: []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
		},
		{
			name:     "mixed ports",
			input:    []string{"192.168.1.1:6789", "192.168.1.2:3300", "192.168.1.3:6789"},
			expected: []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "no port in IP",
			input:    []string{"192.168.1.1", "192.168.1.2:6789", "192.168.1.2:6789"},
			expected: []string{"192.168.1.1", "192.168.1.2:3300", "192.168.1.2:3300"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of the input slice to avoid modifying the original
			inputCopy := make([]string, len(tt.input))
			copy(inputCopy, tt.input)
			replaceMsgr1PortWithMsgr2(inputCopy)
			if !reflect.DeepEqual(inputCopy, tt.expected) {
				t.Errorf("replaceMsgr1PortWithMsgr2() = %v, expected %v", inputCopy, tt.expected)
			}
		})
	}
}

func TestGetKubeResourcesForClass(t *testing.T) {
	srcClassName := "class-a"

	srcSc := &storagev1.StorageClass{}
	srcSc.Name = srcClassName
	srcSc.Parameters = map[string]string{
		"key1": "val1",
		"keyn": "valn",
	}
	srcSc.Provisioner = "whoami"
	srcSc.MountOptions = []string{"mount", "secretly"}

	consumer := &ocsv1a1.StorageConsumer{}
	classItem := ocsv1a1.StorageClassSpec{}
	classItem.Name = srcClassName
	classItem.Aliases = append(classItem.Aliases, "class-1", "class-2")
	consumer.Spec.StorageClasses = append(
		consumer.Spec.StorageClasses,
		classItem,
	)
	genClassFn := func(srcName string) (client.Object, error) {
		return srcSc, nil
	}

	objs := getKubeResourcesForClass(
		klog.Background(),
		consumer.Spec.StorageClasses,
		"StorageClass",
		genClassFn,
	)

	// class-a, class-1 and class-2
	wantObjs := 3
	if gotObjs := len(objs); gotObjs != wantObjs {
		t.Fatalf("expected %d objects, got %d", wantObjs, gotObjs)
	}

	objIdxByName := make(map[string]int, len(objs))
	for idx, obj := range objs {
		objIdxByName[obj.kubeObject.GetName()] = idx
	}

	for _, expName := range []string{"class-1", "class-2"} {
		t.Run(expName, func(t *testing.T) {
			wantObj := srcSc.DeepCopy()
			wantObj.Name = expName
			idx, exist := objIdxByName[wantObj.Name]
			if !exist {
				t.Fatalf("expected storageclass with name %s to exist", wantObj.Name)
			}
			gotObj := objs[idx]
			// except the name the whole object should be deep equal
			wantObj.Name = expName
			if !equality.Semantic.DeepEqual(gotObj.kubeObject, wantObj) {
				t.Fatalf("expected %v to be deep equal to %v", gotObj.kubeObject, wantObj)
			}
		})
	}
}

func newTestProviderServer(t *testing.T, objs ...client.Object) *OCSProviderServer {
	t.Helper()
	scheme, err := newScheme()
	if err != nil {
		t.Fatalf("newScheme() error = %v", err)
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&ocsv1a1.StorageConsumer{}).
		WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
		Build()
	return &OCSProviderServer{
		client:          fakeClient,
		scheme:          scheme,
		consumerManager: createTestConsumerManager(fakeClient),
		namespace:       testNamespace,
	}
}

func TestAppendVolumeAttributesClassKubeResources(t *testing.T) {
	ctx := context.Background()
	scheme, err := newScheme()
	if err != nil {
		t.Fatalf("newScheme() error = %v", err)
	}

	rbdVAC := &storagev1.VolumeAttributesClass{
		ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos-high"},
		DriverName: util.RbdDriverName,
		Parameters: map[string]string{
			"maxReadIops":  "2000",
			"maxWriteIops": "2000",
		},
	}

	rbdVACLow := &storagev1.VolumeAttributesClass{
		ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos-low"},
		DriverName: util.RbdDriverName,
		Parameters: map[string]string{
			"maxReadIops": "500",
		},
	}

	cephfsVAC := &storagev1.VolumeAttributesClass{
		ObjectMeta: metav1.ObjectMeta{Name: "cephfs-qos-low"},
		DriverName: util.CephFSDriverName,
		Parameters: map[string]string{
			"maxReadBps": "104857600",
		},
	}

	unsupportedVAC := &storagev1.VolumeAttributesClass{
		ObjectMeta: metav1.ObjectMeta{Name: "ebs-qos"},
		DriverName: "ebs.csi.aws.com",
		Parameters: map[string]string{
			"maxReadIops": "1000",
		},
	}

	tests := []struct {
		name          string
		clusterObjs   []client.Object
		consumerVACs  []ocsv1a1.VolumeAttributesClassesSpec
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "no VACs in consumer spec",
			clusterObjs:   []client.Object{rbdVAC},
			consumerVACs:  nil,
			expectedCount: 0,
		},
		{
			name:        "single VAC distributed",
			clusterObjs: []client.Object{rbdVAC},
			consumerVACs: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "rbd-qos-high"}},
			},
			expectedCount: 1,
			expectedNames: []string{"rbd-qos-high"},
		},
		{
			name:        "multiple RBD VACs distributed",
			clusterObjs: []client.Object{rbdVAC, rbdVACLow},
			consumerVACs: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "rbd-qos-high"}},
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "rbd-qos-low"}},
			},
			expectedCount: 2,
			expectedNames: []string{"rbd-qos-high", "rbd-qos-low"},
		},
		{
			name:        "CephFS driver VAC skipped",
			clusterObjs: []client.Object{cephfsVAC},
			consumerVACs: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "cephfs-qos-low"}},
			},
			expectedCount: 0,
		},
		{
			name:        "VAC with rename",
			clusterObjs: []client.Object{rbdVAC},
			consumerVACs: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "rbd-qos-high", Rename: "custom-qos-name"}},
			},
			expectedCount: 1,
			expectedNames: []string{"custom-qos-name"},
		},
		{
			name:        "VAC with aliases",
			clusterObjs: []client.Object{rbdVAC},
			consumerVACs: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{
					Name:    "rbd-qos-high",
					Aliases: []string{"alias-1", "alias-2"},
				}},
			},
			expectedCount: 3,
			expectedNames: []string{"rbd-qos-high", "alias-1", "alias-2"},
		},
		{
			name:        "unsupported driver VAC skipped",
			clusterObjs: []client.Object{unsupportedVAC},
			consumerVACs: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "ebs-qos"}},
			},
			expectedCount: 0,
		},
		{
			name:        "non-existent VAC skipped",
			clusterObjs: []client.Object{},
			consumerVACs: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "does-not-exist"}},
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.clusterObjs...).
				Build()

			server := &OCSProviderServer{
				client: fakeClient,
				scheme: scheme,
			}

			consumer := &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{Name: "test-consumer", Namespace: "test-ns"},
			}
			consumer.Spec.VolumeAttributesClasses = tt.consumerVACs

			records, err := server.appendVolumeAttributesClassKubeResources(
				ctx,
				klog.Background(),
				nil,
				consumer,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(records) != tt.expectedCount {
				t.Fatalf("expected %d records, got %d", tt.expectedCount, len(records))
			}

			if tt.expectedNames != nil {
				gotNames := map[string]bool{}
				for _, r := range records {
					gotNames[r.kubeObject.GetName()] = true
				}
				for _, expectedName := range tt.expectedNames {
					if !gotNames[expectedName] {
						t.Errorf("expected record with name %q, got names %v", expectedName, gotNames)
					}
				}
			}
		})
	}
}

func TestGetDesiredClientState(t *testing.T) {
	ctx := context.Background()
	consumerUID := "test-consumer-uid-123"

	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer",
			Namespace: testNamespace,
			UID:       types.UID(consumerUID),
		},
		Status: ocsv1a1.StorageConsumerStatus{
			Client: &ocsv1a1.ClientStatus{
				OperatorVersion:   "4.22.0",
				PlatformVersion:   "4.22.0",
				OperatorNamespace: "client-ns",
				Name:              "test-client",
				ClusterID:         "test-cluster-id",
				ID:                "test-client-id",
			},
		},
	}

	tests := []struct {
		name              string
		setupConsumer     func() *ocsv1a1.StorageConsumer
		expectedErrorCode codes.Code
	}{
		{
			name: "consumer not found",
			setupConsumer: func() *ocsv1a1.StorageConsumer {
				return nil
			},
			expectedErrorCode: codes.Internal,
		},
		{
			name: "consumer not enabled",
			setupConsumer: func() *ocsv1a1.StorageConsumer {
				c := consumer.DeepCopy()
				c.Status.State = ocsv1a1.StorageConsumerStateNotEnabled
				return c
			},
			expectedErrorCode: codes.FailedPrecondition,
		},
		{
			name: "consumer failed",
			setupConsumer: func() *ocsv1a1.StorageConsumer {
				c := consumer.DeepCopy()
				c.Status.State = ocsv1a1.StorageConsumerStateFailed
				return c
			},
			expectedErrorCode: codes.Internal,
		},
		{
			name: "consumer configuring",
			setupConsumer: func() *ocsv1a1.StorageConsumer {
				c := consumer.DeepCopy()
				c.Status.State = ocsv1a1.StorageConsumerStateConfiguring
				return c
			},
			expectedErrorCode: codes.Unavailable,
		},
		{
			name: "consumer deleting",
			setupConsumer: func() *ocsv1a1.StorageConsumer {
				c := consumer.DeepCopy()
				c.Status.State = ocsv1a1.StorageConsumerStateDeleting
				return c
			},
			expectedErrorCode: codes.NotFound,
		},
		{
			name: "consumer status not set",
			setupConsumer: func() *ocsv1a1.StorageConsumer {
				c := consumer.DeepCopy()
				c.Status.State = ""
				return c
			},
			expectedErrorCode: codes.Unavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.setupConsumer()
			var srv *OCSProviderServer
			if c != nil {
				srv = newTestProviderServer(t, c)
			} else {
				srv = newTestProviderServer(t)
			}

			req := &pb.GetDesiredClientStateRequest{
				StorageConsumerUUID: consumerUID,
			}
			_, err := srv.GetDesiredClientState(ctx, req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := status.Code(err); got != tt.expectedErrorCode {
				t.Fatalf("expected error code %v, got %v: %v", tt.expectedErrorCode, got, err)
			}
		})
	}
}

func TestGetDesiredClientStateReady(t *testing.T) {
	ctx := context.Background()
	consumerUID := types.UID("ready-consumer-uid-999")

	rbdProvisionerSecretName := util.GenerateCsiRbdProvisionerCephClientName(1, consumerUID)
	rbdNodeSecretName := util.GenerateCsiRbdNodeCephClientName(1, consumerUID)

	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ready-consumer",
			Namespace: testNamespace,
			UID:       consumerUID,
		},
		Spec: ocsv1a1.StorageConsumerSpec{
			VolumeAttributesClasses: []ocsv1a1.VolumeAttributesClassesSpec{
				{CommonClassSpec: ocsv1a1.CommonClassSpec{Name: "rbd-qos-high"}},
			},
		},
		Status: ocsv1a1.StorageConsumerStatus{
			State: ocsv1a1.StorageConsumerStateReady,
			Client: &ocsv1a1.ClientStatus{
				OperatorVersion:   "4.22.0",
				PlatformVersion:   "4.22.0",
				OperatorNamespace: "client-ns",
				Name:              "test-client",
				ClusterID:         "test-cluster-id",
				ID:                "test-client-id",
			},
			ResourceNameMappingConfigMap: corev1.LocalObjectReference{
				Name: "consumer-config",
			},
		},
	}

	consumerConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-config",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"rbd-rados-ns":                   "test-rados-ns",
			"cephfs-subvolumegroup":          "test-svg",
			"cephfs-subvolumegroup-rados-ns": "test-svg-rados-ns",
			"csiop-rbd-client-profile":       "rbd-profile",
			"csi-rbd-provisioner-ceph-user":  "rbd-provisioner-secret",
			"csi-rbd-node-ceph-user":         "rbd-node-secret",
		},
	}

	storageCluster := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storagecluster",
			Namespace: testNamespace,
		},
	}

	cephCluster := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cephcluster",
			Namespace: testNamespace,
		},
		Status: rookCephv1.ClusterStatus{
			CephStatus: &rookCephv1.CephStatus{
				FSID: "b88c2d78-9de9-4227-9313-a63f62f78743",
			},
		},
	}

	monConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-mon-endpoints",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"data": "a=10.0.0.1:6789",
		},
	}

	rbdProvisionerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rbdProvisionerSecretName,
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"userID":  []byte("rbd-provisioner"),
			"userKey": []byte("test-key"),
		},
	}

	rbdNodeSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rbdNodeSecretName,
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"userID":  []byte("rbd-node"),
			"userKey": []byte("test-key"),
		},
	}

	rbdVAC := &storagev1.VolumeAttributesClass{
		ObjectMeta: metav1.ObjectMeta{Name: "rbd-qos-high"},
		DriverName: util.RbdDriverName,
		Parameters: map[string]string{
			"maxReadIops":  "2000",
			"maxWriteIops": "2000",
		},
	}

	srv := newTestProviderServer(t,
		consumer,
		consumerConfigMap,
		storageCluster,
		cephCluster,
		monConfigMap,
		rbdProvisionerSecret,
		rbdNodeSecret,
		rbdVAC,
	)

	resp, err := srv.GetDesiredClientState(ctx, &pb.GetDesiredClientStateRequest{
		StorageConsumerUUID: string(consumerUID),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.KubeObjects) == 0 {
		t.Fatal("expected KubeObjects in response")
	}
	if resp.DesiredStateHash == "" {
		t.Fatal("expected DesiredStateHash to be set")
	}

	foundVAC := false
	for _, obj := range resp.KubeObjects {
		if obj.Bytes == nil {
			continue
		}
		raw := map[string]interface{}{}
		if err := json.Unmarshal(obj.Bytes, &raw); err != nil {
			continue
		}
		kind, _ := raw["kind"].(string)
		metadata, _ := raw["metadata"].(map[string]interface{})
		name, _ := metadata["name"].(string)
		if kind == "VolumeAttributesClass" && name == "rbd-qos-high" {
			foundVAC = true
			params, _ := raw["parameters"].(map[string]interface{})
			if params["maxReadIops"] != "2000" {
				t.Errorf("expected maxReadIops=2000, got %v", params["maxReadIops"])
			}
			if params["maxWriteIops"] != "2000" {
				t.Errorf("expected maxWriteIops=2000, got %v", params["maxWriteIops"])
			}
		}
	}
	if !foundVAC {
		t.Error("VolumeAttributesClass 'rbd-qos-high' not found in response KubeObjects")
	}
}

func TestOffboardConsumer(t *testing.T) {
	ctx := context.Background()
	consumerUID := "offboard-uid-456"

	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "offboard-consumer",
			Namespace: testNamespace,
			UID:       types.UID(consumerUID),
		},
		Status: ocsv1a1.StorageConsumerStatus{
			Client: &ocsv1a1.ClientStatus{
				OperatorVersion:   "4.22.0",
				OperatorNamespace: "client-ns",
				Name:              "test-client",
				ID:                "test-client-id",
			},
		},
	}

	tests := []struct {
		name        string
		consumerUID string
		seedObjs    []client.Object
		wantErr     bool
		validate    func(t *testing.T, srv *OCSProviderServer)
	}{
		{
			name:        "successful offboard clears client info",
			consumerUID: consumerUID,
			seedObjs:    []client.Object{consumer},
			validate: func(t *testing.T, srv *OCSProviderServer) {
				c, err := srv.consumerManager.Get(ctx, consumerUID)
				if err != nil {
					t.Fatalf("failed to get consumer after offboard: %v", err)
				}
				if c.Status.Client != nil {
					t.Fatal("expected client status to be nil after offboard")
				}
			},
		},
		{
			name:        "offboard non-existent consumer fails",
			consumerUID: "non-existent-uid",
			seedObjs:    []client.Object{consumer},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestProviderServer(t, tt.seedObjs...)

			resp, err := srv.OffboardConsumer(ctx, &pb.OffboardConsumerRequest{
				StorageConsumerUUID: tt.consumerUID,
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if tt.validate != nil {
				tt.validate(t, srv)
			}
		})
	}
}

func TestReportStatus(t *testing.T) {
	ctx := context.Background()
	consumerUID := "report-uid-789"

	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "report-consumer",
			Namespace: testNamespace,
			UID:       types.UID(consumerUID),
		},
	}

	tests := []struct {
		name              string
		req               *pb.ReportStatusRequest
		seedObjs          []client.Object
		expectedErrorCode codes.Code
	}{
		{
			name: "malformed client operator version",
			req: &pb.ReportStatusRequest{
				StorageConsumerUUID:   consumerUID,
				ClientOperatorVersion: "not-a-semver",
			},
			seedObjs:          []client.Object{consumer},
			expectedErrorCode: codes.InvalidArgument,
		},
		{
			name: "malformed client platform version",
			req: &pb.ReportStatusRequest{
				StorageConsumerUUID:   consumerUID,
				ClientOperatorVersion: "4.22.0",
				ClientPlatformVersion: "bad-version",
			},
			seedObjs:          []client.Object{consumer},
			expectedErrorCode: codes.InvalidArgument,
		},
		{
			name: "consumer not found",
			req: &pb.ReportStatusRequest{
				StorageConsumerUUID:   "non-existent-uid",
				ClientOperatorVersion: "4.22.0",
				ClientPlatformVersion: "4.22.0",
			},
			seedObjs:          []client.Object{consumer},
			expectedErrorCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestProviderServer(t, tt.seedObjs...)

			_, err := srv.ReportStatus(ctx, tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := status.Code(err); got != tt.expectedErrorCode {
				t.Fatalf("expected error code %v, got %v: %v", tt.expectedErrorCode, got, err)
			}
		})
	}
}

func TestRequestMaintenanceMode(t *testing.T) {
	ctx := context.Background()
	consumerUID := "maint-uid-101"

	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "maint-consumer",
			Namespace: testNamespace,
			UID:       types.UID(consumerUID),
		},
	}

	tests := []struct {
		name     string
		enable   bool
		seedObjs []client.Object
		wantErr  bool
		validate func(t *testing.T, srv *OCSProviderServer)
	}{
		{
			name:     "enable maintenance mode",
			enable:   true,
			seedObjs: []client.Object{consumer},
			validate: func(t *testing.T, srv *OCSProviderServer) {
				c, err := srv.consumerManager.Get(ctx, consumerUID)
				if err != nil {
					t.Fatalf("failed to get consumer: %v", err)
				}
				if _, ok := c.GetAnnotations()[util.RequestMaintenanceModeAnnotation]; !ok {
					t.Fatal("expected maintenance mode annotation")
				}
			},
		},
		{
			name:   "disable maintenance mode",
			enable: false,
			seedObjs: []client.Object{
				func() *ocsv1a1.StorageConsumer {
					c := consumer.DeepCopy()
					c.Annotations = map[string]string{util.RequestMaintenanceModeAnnotation: ""}
					return c
				}(),
			},
			validate: func(t *testing.T, srv *OCSProviderServer) {
				c, err := srv.consumerManager.Get(ctx, consumerUID)
				if err != nil {
					t.Fatalf("failed to get consumer: %v", err)
				}
				if _, ok := c.GetAnnotations()[util.RequestMaintenanceModeAnnotation]; ok {
					t.Fatal("expected maintenance mode annotation to be removed")
				}
			},
		},
		{
			name:     "enable on non-existent consumer fails",
			enable:   true,
			seedObjs: []client.Object{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestProviderServer(t, tt.seedObjs...)

			resp, err := srv.RequestMaintenanceMode(ctx, &pb.RequestMaintenanceModeRequest{
				StorageConsumerUUID: consumerUID,
				Enable:              tt.enable,
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if tt.validate != nil {
				tt.validate(t, srv)
			}
		})
	}
}

func newNotifyTestServer(t *testing.T, objs ...client.Object) *OCSProviderServer {
	t.Helper()
	return newTestProviderServer(t, objs...)
}

func TestNotify(t *testing.T) {
	ctx := context.Background()
	storageConsumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storage-consumer",
			Namespace: testNamespace,
			UID:       "storage-consumer-123",
		},
	}
	obcName := "test-obc"
	obcNamespace := "app-namespace"
	obcCreatePayload := nbv1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obcName,
			Namespace: obcNamespace,
		},
		Spec: nbv1.ObjectBucketClaimSpec{
			StorageClassName:   "openshift-storage.noobaa.io",
			GenerateBucketName: "test-obc-0402",
		},
	}
	obcCreatePayloadBytes, err := json.Marshal(obcCreatePayload)
	if err != nil {
		t.Fatalf("failed to marshal OBC payload: %v", err)
	}
	obcDeletePayload := types.NamespacedName{
		Name:      obcName,
		Namespace: obcNamespace,
	}
	obcDeletepayloadBytes, err := json.Marshal(obcDeletePayload)
	if err != nil {
		t.Fatalf("failed to marshal OBC payload: %v", err)
	}

	tests := []struct {
		name              string
		setupServer       func(t *testing.T) *OCSProviderServer
		req               *pb.NotifyRequest
		ExpectedErrorCode codes.Code
		validate          func(t *testing.T, srv *OCSProviderServer)
	}{
		{
			name: "notify failed - unknown reason",
			setupServer: func(t *testing.T) *OCSProviderServer {
				return newNotifyTestServer(t, storageConsumer)
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              uint32(ifaces.NotifyReasonUnknown),
			},
			ExpectedErrorCode: codes.InvalidArgument,
		},
		{
			name: "notify failed - storage consumer not found",
			setupServer: func(t *testing.T) *OCSProviderServer {
				return newNotifyTestServer(t)
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: "non-existent-uuid",
				Reason:              uint32(ifaces.NotifyReasonObcCreated),
				Payload:             obcCreatePayloadBytes,
			},
			ExpectedErrorCode: codes.Internal,
		},
		{
			name: "notify succeeded - obc created",
			setupServer: func(t *testing.T) *OCSProviderServer {
				return newNotifyTestServer(t, storageConsumer)
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              uint32(ifaces.NotifyReasonObcCreated),
				Payload:             obcCreatePayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
			validate: func(t *testing.T, srv *OCSProviderServer) {
				expectedName := getObcHashedName(types.NamespacedName{
					Name:      storageConsumer.Name,
					Namespace: storageConsumer.Namespace,
				}, obcName, obcNamespace)
				obc := &nbv1.ObjectBucketClaim{}
				if err := srv.client.Get(ctx, types.NamespacedName{
					Name:      expectedName,
					Namespace: testNamespace,
				}, obc); err != nil {
					t.Fatalf("expected OBC to be created: %v", err)
				}
				if obc.Labels[remoteObcNameLabelKey] != obcName {
					t.Fatalf("expected label %s=test-obc, got %v", remoteObcNameLabelKey, obc.Labels)
				}
				if obc.Labels[remoteObcNamespaceLabelKey] != obcNamespace {
					t.Fatalf("expected label %s=app-namespace, got %v", remoteObcNamespaceLabelKey, obc.Labels)
				}
				if obc.Labels[storageConsumerNameLabelKey] != storageConsumer.Name {
					t.Fatalf("expected label %s=%s, got %v", storageConsumerNameLabelKey, storageConsumer.Name, obc.Labels)
				}
				if obc.Labels[storageConsumerUUIDLabelKey] != string(storageConsumer.UID) {
					t.Fatalf("expected label %s=%s, got %v", storageConsumerUUIDLabelKey, storageConsumer.UID, obc.Labels)
				}
				if obc.Annotations[remoteObcCreationAnnotationKey] != "true" {
					t.Fatalf("expected annotation %s=true, got %v", remoteObcCreationAnnotationKey, obc.Annotations)
				}
				if len(obc.OwnerReferences) == 0 {
					t.Fatalf("expected ownerReferences to be set")
				}
				foundOwner := false
				for _, ownerRef := range obc.OwnerReferences {
					if ownerRef.Kind == "StorageConsumer" &&
						ownerRef.Name == storageConsumer.Name &&
						ownerRef.UID == storageConsumer.UID &&
						ownerRef.APIVersion == ocsv1a1.GroupVersion.String() {
						foundOwner = true
						break
					}
				}
				if !foundOwner {
					t.Fatalf("expected StorageConsumer owner reference, got %v", obc.OwnerReferences)
				}
			},
		},
		{
			name: "notify failed - obc created - invalid JSON payload",
			setupServer: func(t *testing.T) *OCSProviderServer {
				return newNotifyTestServer(t, storageConsumer)
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              uint32(ifaces.NotifyReasonObcCreated),
				Payload:             []byte("not valid json"),
			},
			ExpectedErrorCode: codes.InvalidArgument,
		},
		{
			name: "notify succeeded - obc created - OBC already exists",
			setupServer: func(t *testing.T) *OCSProviderServer {
				obcHashedName := getObcHashedName(types.NamespacedName{
					Name:      storageConsumer.Name,
					Namespace: storageConsumer.Namespace,
				}, obcName, obcNamespace)
				existingObc := &nbv1.ObjectBucketClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      obcHashedName,
						Namespace: testNamespace,
					},
				}
				return newNotifyTestServer(t, storageConsumer, existingObc)
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              uint32(ifaces.NotifyReasonObcCreated),
				Payload:             obcCreatePayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
		},
		{
			name: "notify succeeded - obc deleted",
			setupServer: func(t *testing.T) *OCSProviderServer {
				obcHashedName := getObcHashedName(types.NamespacedName{
					Name:      storageConsumer.Name,
					Namespace: storageConsumer.Namespace,
				}, obcName, obcNamespace)
				obc := &nbv1.ObjectBucketClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      obcHashedName,
						Namespace: testNamespace,
						Labels: map[string]string{
							remoteObcNameLabelKey:       obcName,
							remoteObcNamespaceLabelKey:  obcNamespace,
							storageConsumerNameLabelKey: storageConsumer.Name,
						},
					},
				}
				return newNotifyTestServer(t, storageConsumer, obc)
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              uint32(ifaces.NotifyReasonObcDeleted),
				Payload:             obcDeletepayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
			validate: func(t *testing.T, srv *OCSProviderServer) {
				expectedName := getObcHashedName(types.NamespacedName{
					Name:      storageConsumer.Name,
					Namespace: storageConsumer.Namespace,
				}, obcName, obcNamespace)
				obc := &nbv1.ObjectBucketClaim{}
				err := srv.client.Get(ctx, types.NamespacedName{
					Name:      expectedName,
					Namespace: testNamespace,
				}, obc)
				if err == nil {
					t.Fatalf("expected OBC to be deleted, but it still exists")
				}
				if !apierrors.IsNotFound(err) {
					t.Fatalf("expected not found error, got %v", err)
				}
			},
		},
		{
			name: "notify succeeded - obc deleted - obc does not exist",
			setupServer: func(t *testing.T) *OCSProviderServer {
				return newNotifyTestServer(t, storageConsumer)
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              uint32(ifaces.NotifyReasonObcDeleted),
				Payload:             obcDeletepayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			resp, err := srv.Notify(ctx, tt.req)

			if tt.ExpectedErrorCode == codes.OK {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if resp == nil {
					t.Fatalf("expected non-nil response")
				}
			} else {
				if resp != nil {
					t.Fatalf("expected nil response, got %#v", resp)
				}
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if status.Code(err) != tt.ExpectedErrorCode {
					t.Fatalf("expected %v error, got %v", tt.ExpectedErrorCode, status.Code(err))
				}
			}

			if tt.validate != nil {
				tt.validate(t, srv)
			}
		})
	}
}

func TestBuildConsumerMap(t *testing.T) {
	alertsResp := &prometheusAlertsResponse{
		Status: "success",
		Data: struct {
			Alerts []prometheusAlert `json:"alerts"`
		}{
			Alerts: []prometheusAlert{
				{Labels: map[string]string{"alertname": "FiringMatch", storageConsumerNameLabel: "test-consumer"}, State: "firing", Value: "85.5"},
				{Labels: map[string]string{"alertname": "WrongConsumer", storageConsumerNameLabel: "other-consumer"}, State: "firing", Value: "95"},
				{Labels: map[string]string{"alertname": "Pending", storageConsumerNameLabel: "test-consumer"}, State: "pending", Value: "1"},
				{Labels: map[string]string{"alertname": "NoLabel"}, State: "firing", Value: "90"},
				{Labels: map[string]string{"alertname": "FiringMatch2", storageConsumerNameLabel: "test-consumer"}, State: "firing", Value: "3"},
			},
		},
	}

	got := buildConsumerMap(alertsResp)

	// test-consumer should have 2 firing alerts
	testAlerts := got["test-consumer"]
	if len(testAlerts) != 2 {
		t.Fatalf("expected 2 alerts for test-consumer, got %d", len(testAlerts))
	}
	if testAlerts[0].AlertName != "FiringMatch" {
		t.Errorf("alert[0].AlertName = %q, want %q", testAlerts[0].AlertName, "FiringMatch")
	}
	if testAlerts[0].Value != 85.5 {
		t.Errorf("alert[0].Value = %v, want 85.5", testAlerts[0].Value)
	}
	if testAlerts[1].AlertName != "FiringMatch2" {
		t.Errorf("alert[1].AlertName = %q, want %q", testAlerts[1].AlertName, "FiringMatch2")
	}

	// other-consumer should have 1 alert
	otherAlerts := got["other-consumer"]
	if len(otherAlerts) != 1 {
		t.Fatalf("expected 1 alert for other-consumer, got %d", len(otherAlerts))
	}

	// No alerts without consumer label
	if _, ok := got[""]; ok {
		t.Error("expected no alerts for empty consumer name")
	}
}

func TestGetClientAlerts(t *testing.T) {
	ctx := context.Background()
	storageConsumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer",
			Namespace: testNamespace,
			UID:       "consumer-uid-123",
		},
	}
	srv := newNotifyTestServer(t, storageConsumer)

	// Inject a pre-populated alert cache (no HTTP server needed)
	srv.alertStore = &alertStore{
		pollInterval: time.Minute,
		alertsByConsumer: map[string][]*pb.AlertInfo{
			"test-consumer": {
				{
					AlertName: "CephOSDNearFull",
					Labels: map[string]string{
						"alertname":              "CephOSDNearFull",
						storageConsumerNameLabel: "test-consumer",
					},
					Value: 85,
				},
			},
		},
		lastUpdateTime: time.Now(),
	}

	resp, err := srv.GetClientAlerts(ctx, &pb.GetClientAlertsRequest{
		StorageConsumerUUID: string(storageConsumer.UID),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(resp.Alerts))
	}
	if resp.Alerts[0].AlertName != "CephOSDNearFull" {
		t.Errorf("expected alert name %q, got %q", "CephOSDNearFull", resp.Alerts[0].AlertName)
	}
	if resp.Alerts[0].Value != 85 {
		t.Errorf("expected alert value 85, got %v", resp.Alerts[0].Value)
	}
}

func TestAlertCachePolling(t *testing.T) {
	var queryCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCount.Add(1)
		response := prometheusAlertsResponse{
			Status: "success",
			Data: struct {
				Alerts []prometheusAlert `json:"alerts"`
			}{
				Alerts: []prometheusAlert{
					{
						Labels: map[string]string{
							"alertname":              "DiskPressure",
							storageConsumerNameLabel: "test-consumer",
						},
						State: "firing",
						Value: "85.5",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	cache := newAlertStore(ts.URL, 100*time.Millisecond, ts.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go cache.startPolling(ctx)

	// Wait for at least 2 polls
	time.Sleep(350 * time.Millisecond)

	count := queryCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 Prometheus queries, got %d", count)
	}

	alerts, err := cache.getAlertsForConsumer("test-consumer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].AlertName != "DiskPressure" {
		t.Errorf("expected alert name %q, got %q", "DiskPressure", alerts[0].AlertName)
	}
	if alerts[0].Value != 85.5 {
		t.Errorf("expected alert value 85.5, got %v", alerts[0].Value)
	}
}

func TestAlertCacheConcurrentReads(t *testing.T) {
	cache := &alertStore{
		pollInterval: time.Minute,
		alertsByConsumer: map[string][]*pb.AlertInfo{
			"consumer-1": {{AlertName: "Alert1"}},
			"consumer-2": {{AlertName: "Alert2"}},
		},
		lastUpdateTime: time.Now(),
	}

	const numReaders = 100
	var wg sync.WaitGroup
	errors := make([]error, numReaders)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			consumerName := fmt.Sprintf("consumer-%d", (idx%2)+1)
			_, errors[idx] = cache.getAlertsForConsumer(consumerName)
		}(i)
	}

	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("read %d failed: %v", i, err)
		}
	}
}

func newRGWTestObjects() (
	*ocsv1.StorageCluster,
	*ocsv1a1.StorageConsumer,
	*rookCephv1.CephObjectStoreAccount,
	*corev1.ConfigMap,
	*corev1.Secret,
	*routev1.Route,
) {
	storageCluster := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: testNamespace,
			Annotations: map[string]string{
				util.EnableAdvancedRGWFeaturesAnnotation: "true",
			},
		},
	}

	consumerConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer-config",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"rgw-account-name":            "test-rgw-account",
			"rgw-credentials-secret-name": "spoke-account-iam-credentials",
		},
	}

	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer",
			Namespace: testNamespace,
			UID:       "consumer-uid-123",
			Annotations: map[string]string{
				util.EnableRGWAccountAnnotation: "true",
			},
		},
		Status: ocsv1a1.StorageConsumerStatus{
			Client: &ocsv1a1.ClientStatus{
				ID:                "client-id-abc",
				OperatorNamespace: "spoke-ns",
			},
			ResourceNameMappingConfigMap: corev1.LocalObjectReference{
				Name: consumerConfigMap.Name,
			},
		},
	}

	account := &rookCephv1.CephObjectStoreAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rgw-account",
			Namespace: testNamespace,
		},
		Status: &rookCephv1.ObjectStoreAccountStatus{
			Phase:                 "Ready",
			AccountID:             "RGW12345678901234567",
			RootAccountSecretName: "rook-ceph-root-user-test-secret",
		},
	}

	rookSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "rook-ceph-root-user-test-secret",
			Namespace:       testNamespace,
			ResourceVersion: "12345",
		},
		Data: map[string][]byte{
			"AccessKey": []byte("TESTACCESSKEY123"),
			"SecretKey": []byte("TESTSecretKey456"),
		},
	}

	rgwRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenerateNameForCephObjectStoreSecureRoute(storageCluster),
			Namespace: testNamespace,
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{
					Host: "rgw.apps.example.com",
					Conditions: []routev1.RouteIngressCondition{
						{
							Type:   routev1.RouteAdmitted,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
	}

	return storageCluster, consumer, account, consumerConfigMap, rookSecret, rgwRoute
}

func TestGetRGWRootUserCredentialsSecret(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		objs         func() []client.Object
		expectSecret bool
		expectError  bool
	}{
		{
			name:        "Account not found",
			expectError: true,
			objs: func() []client.Object {
				_, consumer, _, consumerCM, rookSecret, _ := newRGWTestObjects()
				return []client.Object{consumer, consumerCM, rookSecret}
			},
		},
		{
			name: "Account not Ready",
			objs: func() []client.Object {
				_, consumer, account, consumerCM, rookSecret, _ := newRGWTestObjects()
				account.Status.Phase = "Pending"
				return []client.Object{consumer, consumerCM, account, rookSecret}
			},
		},
		{
			name: "Account missing rootAccountSecretName",
			objs: func() []client.Object {
				_, consumer, account, consumerCM, rookSecret, _ := newRGWTestObjects()
				account.Status.RootAccountSecretName = ""
				return []client.Object{consumer, consumerCM, account, rookSecret}
			},
		},
		{
			name:        "Root user Secret not found",
			expectError: true,
			objs: func() []client.Object {
				_, consumer, account, consumerCM, _, _ := newRGWTestObjects()
				return []client.Object{consumer, consumerCM, account}
			},
		},
		{
			name:         "success",
			expectSecret: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, consumer, account, consumerCM, rookSecret, _ := newRGWTestObjects()

			consumerConfig := util.WrapStorageConsumerResourceMap(consumerCM.Data)

			var objs []client.Object
			if tt.objs != nil {
				objs = tt.objs()
			} else {
				objs = []client.Object{consumer, consumerCM, account, rookSecret}
			}

			srv := newNotifyTestServer(t, objs...)
			secret, err := srv.getRGWRootUserCredentialsSecret(ctx, consumer, consumerConfig)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, secret)
			} else {
				assert.NoError(t, err)
			}
			if tt.expectSecret {
				assert.NotNil(t, secret)
				assert.Equal(t, "TESTACCESSKEY123", string(secret.Data["AccessKey"]))
				assert.Equal(t, "12345", secret.ResourceVersion)
			}
		})
	}
}

func TestAppendRGWAccountCredentialsKubeResources(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		objs        func() []client.Object
		expectAdded bool
		expectError bool
	}{
		{
			name:        "Account CR not found",
			expectError: true,
			objs: func() []client.Object {
				_, consumer, _, consumerCM, rookSecret, rgwRoute := newRGWTestObjects()
				return []client.Object{consumer, consumerCM, rookSecret, rgwRoute}
			},
		},
		{
			name: "Account CR not Ready",
			objs: func() []client.Object {
				_, consumer, account, consumerCM, rookSecret, rgwRoute := newRGWTestObjects()
				account.Status.Phase = "Pending"
				return []client.Object{consumer, account, consumerCM, rookSecret, rgwRoute}
			},
		},
		{
			name:        "Route not found",
			expectError: true,
			objs: func() []client.Object {
				_, consumer, account, consumerCM, rookSecret, _ := newRGWTestObjects()
				return []client.Object{consumer, account, consumerCM, rookSecret}
			},
		},
		{
			name:        "Route has no admitted host",
			expectError: true,
			objs: func() []client.Object {
				_, consumer, account, consumerCM, rookSecret, rgwRoute := newRGWTestObjects()
				rgwRoute.Status.Ingress = nil
				return []client.Object{consumer, account, consumerCM, rookSecret, rgwRoute}
			},
		},
		{
			name:        "success",
			expectAdded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, consumer, account, consumerCM, rookSecret, rgwRoute := newRGWTestObjects()

			var objs []client.Object
			if tt.objs != nil {
				objs = tt.objs()
			} else {
				objs = []client.Object{consumer, account, consumerCM, rookSecret, rgwRoute}
			}

			srv := newNotifyTestServer(t, objs...)
			consumerConfig := util.WrapStorageConsumerResourceMap(consumerCM.Data)

			records, err := srv.appendRGWAccountCredentialsKubeResources(ctx, nil, consumer, consumerConfig, sc)

			if tt.expectError {
				assert.Error(t, err)
				assert.Empty(t, records)
				return
			}
			assert.NoError(t, err)

			if !tt.expectAdded {
				assert.Empty(t, records)
				return
			}

			assert.Len(t, records, 1)
			assert.Equal(t, pb.KubeClientOp_CREATE_OR_UPDATE, records[0].clientOp)

			secret, ok := records[0].kubeObject.(*corev1.Secret)
			assert.True(t, ok)
			assert.Equal(t, "spoke-account-iam-credentials", secret.Name)
			assert.Equal(t, "spoke-ns", secret.Namespace)
			assert.Equal(t, "client-id-abc", secret.Labels[util.StorageClientLabelKey])
			assert.Equal(t, "https://rgw.apps.example.com", secret.StringData["AWS_ENDPOINT_URL"])
			assert.Equal(t, "RGW12345678901234567", secret.StringData["AWS_ACCOUNT_ID"])
			assert.Equal(t, "TESTACCESSKEY123", secret.StringData["AWS_ACCESS_KEY_ID"])
			assert.Equal(t, "TESTSecretKey456", secret.StringData["AWS_SECRET_ACCESS_KEY"])
		})
	}
}

func TestGetConsumerConfig(t *testing.T) {
	ctx := context.Background()

	t.Run("empty ConfigMap name", func(t *testing.T) {
		consumer := &ocsv1a1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNamespace},
		}
		srv := newTestProviderServer(t, consumer)
		cfg, err := srv.getConsumerConfig(ctx, consumer)
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("ConfigMap not found", func(t *testing.T) {
		consumer := &ocsv1a1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNamespace},
			Status: ocsv1a1.StorageConsumerStatus{
				ResourceNameMappingConfigMap: corev1.LocalObjectReference{Name: "missing-cm"},
			},
		}
		srv := newTestProviderServer(t, consumer)
		cfg, err := srv.getConsumerConfig(ctx, consumer)
		assert.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))
		assert.Nil(t, cfg)
	})

	t.Run("ConfigMap with nil data", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "empty-cm", Namespace: testNamespace},
		}
		consumer := &ocsv1a1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNamespace},
			Status: ocsv1a1.StorageConsumerStatus{
				ResourceNameMappingConfigMap: corev1.LocalObjectReference{Name: cm.Name},
			},
		}
		srv := newTestProviderServer(t, consumer, cm)
		cfg, err := srv.getConsumerConfig(ctx, consumer)
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("success", func(t *testing.T) {
		_, consumer, _, consumerCM, _, _ := newRGWTestObjects()
		srv := newTestProviderServer(t, consumer, consumerCM)
		cfg, err := srv.getConsumerConfig(ctx, consumer)
		assert.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, "test-rgw-account", cfg.GetRGWAccountName())
	})
}

func TestGetRGWCredentialsResourceVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		_, consumer, account, consumerCM, rookSecret, _ := newRGWTestObjects()
		srv := newNotifyTestServer(t, consumer, consumerCM, account, rookSecret)
		version, err := srv.getRGWCredentialsResourceVersion(ctx, consumer)
		assert.NoError(t, err)
		assert.Equal(t, "12345", version)
	})

	t.Run("no ConfigMap name returns error", func(t *testing.T) {
		consumer := &ocsv1a1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNamespace},
		}
		srv := newNotifyTestServer(t, consumer)
		version, err := srv.getRGWCredentialsResourceVersion(ctx, consumer)
		assert.Error(t, err)
		assert.Empty(t, version)
	})

	t.Run("ConfigMap not found returns error", func(t *testing.T) {
		consumer := &ocsv1a1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNamespace},
			Status: ocsv1a1.StorageConsumerStatus{
				ResourceNameMappingConfigMap: corev1.LocalObjectReference{Name: "missing-cm"},
			},
		}
		srv := newNotifyTestServer(t, consumer)
		version, err := srv.getRGWCredentialsResourceVersion(ctx, consumer)
		assert.Error(t, err)
		assert.Empty(t, version)
	})

	t.Run("no RGW account name returns empty", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "no-rgw-cm", Namespace: testNamespace},
			Data:       map[string]string{"some-key": "some-value"},
		}
		consumer := &ocsv1a1.StorageConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: testNamespace},
			Status: ocsv1a1.StorageConsumerStatus{
				ResourceNameMappingConfigMap: corev1.LocalObjectReference{Name: cm.Name},
			},
		}
		srv := newNotifyTestServer(t, consumer, cm)
		version, err := srv.getRGWCredentialsResourceVersion(ctx, consumer)
		assert.NoError(t, err)
		assert.Empty(t, version)
	})
}

func TestGetBlockPoolsInfo(t *testing.T) {
	ctx := context.Background()

	t.Run("empty request returns empty response", func(t *testing.T) {
		srv := newTestProviderServer(t)
		resp, err := srv.GetBlockPoolsInfo(ctx, &pb.BlockPoolsInfoRequest{
			BlockPoolNames: []string{},
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Empty(t, resp.BlockPoolsInfo)
		assert.Empty(t, resp.Errors)
	})

	t.Run("block pool not found is skipped", func(t *testing.T) {
		srv := newTestProviderServer(t)
		resp, err := srv.GetBlockPoolsInfo(ctx, &pb.BlockPoolsInfoRequest{
			BlockPoolNames: []string{"non-existent-pool"},
		})
		assert.NoError(t, err)
		assert.Empty(t, resp.BlockPoolsInfo)
		assert.Empty(t, resp.Errors)
	})

	t.Run("pool without mirroring returns empty token", func(t *testing.T) {
		pool := &rookCephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pool-no-mirror",
				Namespace: testNamespace,
			},
			Status: &rookCephv1.CephBlockPoolStatus{
				PoolID: 42,
			},
		}
		srv := newTestProviderServer(t, pool)
		resp, err := srv.GetBlockPoolsInfo(ctx, &pb.BlockPoolsInfoRequest{
			BlockPoolNames: []string{"pool-no-mirror"},
		})
		assert.NoError(t, err)
		assert.Len(t, resp.BlockPoolsInfo, 1)
		assert.Equal(t, "pool-no-mirror", resp.BlockPoolsInfo[0].BlockPoolName)
		assert.Equal(t, "42", resp.BlockPoolsInfo[0].BlockPoolID)
		assert.Empty(t, resp.BlockPoolsInfo[0].MirroringToken)
		assert.Empty(t, resp.Errors)
	})

	t.Run("pool with mirroring enabled and token secret", func(t *testing.T) {
		pool := &rookCephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pool-mirrored",
				Namespace: testNamespace,
			},
			Spec: rookCephv1.NamedBlockPoolSpec{
				PoolSpec: rookCephv1.PoolSpec{
					Mirroring: rookCephv1.MirroringSpec{Enabled: true},
				},
			},
			Status: &rookCephv1.CephBlockPoolStatus{
				PoolID: 7,
				Info: map[string]string{
					mirroringTokenKey: "bootstrap-secret",
				},
			},
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-secret",
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"token": []byte("my-mirroring-token"),
			},
		}
		srv := newTestProviderServer(t, pool, secret)
		resp, err := srv.GetBlockPoolsInfo(ctx, &pb.BlockPoolsInfoRequest{
			BlockPoolNames: []string{"pool-mirrored"},
		})
		assert.NoError(t, err)
		assert.Len(t, resp.BlockPoolsInfo, 1)
		assert.Equal(t, "pool-mirrored", resp.BlockPoolsInfo[0].BlockPoolName)
		assert.Equal(t, "7", resp.BlockPoolsInfo[0].BlockPoolID)
		assert.Equal(t, "my-mirroring-token", resp.BlockPoolsInfo[0].MirroringToken)
		assert.Empty(t, resp.Errors)
	})

	t.Run("mirroring enabled but bootstrap secret not found", func(t *testing.T) {
		pool := &rookCephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pool-missing-secret",
				Namespace: testNamespace,
			},
			Spec: rookCephv1.NamedBlockPoolSpec{
				PoolSpec: rookCephv1.PoolSpec{
					Mirroring: rookCephv1.MirroringSpec{Enabled: true},
				},
			},
			Status: &rookCephv1.CephBlockPoolStatus{
				PoolID: 3,
				Info: map[string]string{
					mirroringTokenKey: "missing-secret",
				},
			},
		}
		srv := newTestProviderServer(t, pool)
		resp, err := srv.GetBlockPoolsInfo(ctx, &pb.BlockPoolsInfoRequest{
			BlockPoolNames: []string{"pool-missing-secret"},
		})
		assert.NoError(t, err)
		assert.Len(t, resp.BlockPoolsInfo, 1)
		assert.Equal(t, "pool-missing-secret", resp.BlockPoolsInfo[0].BlockPoolName)
		assert.Empty(t, resp.BlockPoolsInfo[0].MirroringToken, "token should be empty when secret not found")
		assert.Empty(t, resp.Errors, "missing secret should not produce an error entry")
	})

	t.Run("mirroring enabled but no token key in status info", func(t *testing.T) {
		pool := &rookCephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pool-no-info",
				Namespace: testNamespace,
			},
			Spec: rookCephv1.NamedBlockPoolSpec{
				PoolSpec: rookCephv1.PoolSpec{
					Mirroring: rookCephv1.MirroringSpec{Enabled: true},
				},
			},
			Status: &rookCephv1.CephBlockPoolStatus{
				PoolID: 5,
			},
		}
		srv := newTestProviderServer(t, pool)
		resp, err := srv.GetBlockPoolsInfo(ctx, &pb.BlockPoolsInfoRequest{
			BlockPoolNames: []string{"pool-no-info"},
		})
		assert.NoError(t, err)
		assert.Len(t, resp.BlockPoolsInfo, 1)
		assert.Empty(t, resp.BlockPoolsInfo[0].MirroringToken)
	})

	t.Run("multiple pools mixed", func(t *testing.T) {
		poolA := &rookCephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pool-a",
				Namespace: testNamespace,
			},
			Spec: rookCephv1.NamedBlockPoolSpec{
				PoolSpec: rookCephv1.PoolSpec{
					Mirroring: rookCephv1.MirroringSpec{Enabled: true},
				},
			},
			Status: &rookCephv1.CephBlockPoolStatus{
				PoolID: 1,
				Info: map[string]string{
					mirroringTokenKey: "secret-a",
				},
			},
		}
		secretA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secret-a",
				Namespace: testNamespace,
			},
			Data: map[string][]byte{"token": []byte("token-a")},
		}
		poolB := &rookCephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pool-b",
				Namespace: testNamespace,
			},
			Status: &rookCephv1.CephBlockPoolStatus{PoolID: 2},
		}

		srv := newTestProviderServer(t, poolA, secretA, poolB)
		resp, err := srv.GetBlockPoolsInfo(ctx, &pb.BlockPoolsInfoRequest{
			BlockPoolNames: []string{"pool-a", "non-existent", "pool-b"},
		})
		assert.NoError(t, err)
		assert.Len(t, resp.BlockPoolsInfo, 2, "non-existent pool should be skipped")
		assert.Empty(t, resp.Errors)

		infoByName := map[string]*pb.BlockPoolInfo{}
		for _, info := range resp.BlockPoolsInfo {
			infoByName[info.BlockPoolName] = info
		}

		assert.Equal(t, "token-a", infoByName["pool-a"].MirroringToken)
		assert.Equal(t, "1", infoByName["pool-a"].BlockPoolID)
		assert.Empty(t, infoByName["pool-b"].MirroringToken)
		assert.Equal(t, "2", infoByName["pool-b"].BlockPoolID)
	})
}

func TestIsRGWAccountEnabled(t *testing.T) {
	sc, consumer, _, _, _, _ := newRGWTestObjects()

	assert.True(t, isRGWAccountEnabled(consumer, sc))

	scNoAnnotation := sc.DeepCopy()
	scNoAnnotation.Annotations = nil
	assert.False(t, isRGWAccountEnabled(consumer, scNoAnnotation))

	consumerNoAnnotation := consumer.DeepCopy()
	consumerNoAnnotation.Annotations = nil
	assert.False(t, isRGWAccountEnabled(consumerNoAnnotation, sc))
}
