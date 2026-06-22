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

// newNotifyTestServer creates an OCSProviderServer for Notify tests with the given objects pre-seeded in the fake client.
func newNotifyTestServer(t *testing.T, objs ...client.Object) *OCSProviderServer {
	t.Helper()
	scheme, schemeErr := newScheme()
	if schemeErr != nil {
		t.Fatalf("newScheme() error = %v", schemeErr)
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
		Build()
	return &OCSProviderServer{
		client:          fakeClient,
		scheme:          scheme,
		consumerManager: createTestConsumerManager(fakeClient),
		namespace:       testNamespace,
	}
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
	*rookCephv1.CephObjectStoreUser,
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
		},
	}

	account := &rookCephv1.CephObjectStoreAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rgw-account",
			Namespace: testNamespace,
		},
		Status: &rookCephv1.ObjectStoreAccountStatus{
			Phase:     "Ready",
			AccountID: "RGW12345678901234567",
		},
	}

	adminUser := &rookCephv1.CephObjectStoreUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenerateRGWAdminUserName("test-consumer"),
			Namespace: testNamespace,
		},
		Status: &rookCephv1.ObjectStoreUserStatus{
			Phase: "Ready",
			Info: map[string]string{
				"secretName": "rook-ceph-object-user-test-secret",
			},
		},
	}

	rookSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "rook-ceph-object-user-test-secret",
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
			Name:      util.GenerateNameForCephObjectStore(storageCluster) + "-secure",
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

	return storageCluster, consumer, account, adminUser, rookSecret, rgwRoute
}

func TestGetRGWAdminCredentialsSecret(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		objs         func() []client.Object
		modSC        func(*ocsv1.StorageCluster)
		modConsumer  func(*ocsv1a1.StorageConsumer)
		expectSecret bool
	}{
		{
			name:   "missing StorageCluster annotation",
			modSC:  func(sc *ocsv1.StorageCluster) { sc.Annotations = nil },
		},
		{
			name:        "missing consumer annotation",
			modConsumer: func(c *ocsv1a1.StorageConsumer) { c.Annotations = nil },
		},
		{
			name: "Admin User not found",
			objs: func() []client.Object {
				_, consumer, _, _, rookSecret, _ := newRGWTestObjects()
				return []client.Object{consumer, rookSecret}
			},
		},
		{
			name: "Admin User not Ready",
			objs: func() []client.Object {
				_, consumer, _, adminUser, rookSecret, _ := newRGWTestObjects()
				adminUser.Status.Phase = "Pending"
				return []client.Object{consumer, adminUser, rookSecret}
			},
		},
		{
			name: "Admin User missing secretName",
			objs: func() []client.Object {
				_, consumer, _, adminUser, rookSecret, _ := newRGWTestObjects()
				adminUser.Status.Info = nil
				return []client.Object{consumer, adminUser, rookSecret}
			},
		},
		{
			name: "Rook Secret not found",
			objs: func() []client.Object {
				_, consumer, _, adminUser, _, _ := newRGWTestObjects()
				return []client.Object{consumer, adminUser}
			},
		},
		{
			name:         "success",
			expectSecret: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, consumer, _, adminUser, rookSecret, _ := newRGWTestObjects()
			if tt.modSC != nil {
				tt.modSC(sc)
			}
			if tt.modConsumer != nil {
				tt.modConsumer(consumer)
			}

			var objs []client.Object
			if tt.objs != nil {
				objs = tt.objs()
			} else {
				objs = []client.Object{consumer, adminUser, rookSecret}
			}

			srv := newNotifyTestServer(t, objs...)
			secret, err := srv.getRGWAdminCredentialsSecret(ctx, consumer, sc)

			assert.NoError(t, err)
			if tt.expectSecret {
				assert.NotNil(t, secret)
				assert.Equal(t, "TESTACCESSKEY123", string(secret.Data["AccessKey"]))
				assert.Equal(t, "12345", secret.ResourceVersion)
			} else {
				assert.Nil(t, secret)
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
	}{
		{
			name: "Account CR not found",
			objs: func() []client.Object {
				_, consumer, _, adminUser, rookSecret, rgwRoute := newRGWTestObjects()
				return []client.Object{consumer, adminUser, rookSecret, rgwRoute}
			},
		},
		{
			name: "Account CR not Ready",
			objs: func() []client.Object {
				_, consumer, account, adminUser, rookSecret, rgwRoute := newRGWTestObjects()
				account.Status.Phase = "Pending"
				return []client.Object{consumer, account, adminUser, rookSecret, rgwRoute}
			},
		},
		{
			name: "Route has no admitted host",
			objs: func() []client.Object {
				_, consumer, account, adminUser, rookSecret, rgwRoute := newRGWTestObjects()
				rgwRoute.Status.Ingress = nil
				return []client.Object{consumer, account, adminUser, rookSecret, rgwRoute}
			},
		},
		{
			name:        "success",
			expectAdded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, consumer, account, adminUser, rookSecret, rgwRoute := newRGWTestObjects()

			var objs []client.Object
			if tt.objs != nil {
				objs = tt.objs()
			} else {
				objs = []client.Object{consumer, account, adminUser, rookSecret, rgwRoute}
			}

			srv := newNotifyTestServer(t, objs...)
			consumerConfig := util.WrapStorageConsumerResourceMap(map[string]string{
				"rgw-account-name":            account.Name,
				"rgw-credentials-secret-name": "spoke-account-iam-credentials",
			})

			records, err := srv.appendRGWAccountCredentialsKubeResources(ctx, nil, consumer, consumerConfig, sc)
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

func TestGetRGWCredentialsResourceVersion(t *testing.T) {
	ctx := context.Background()
	sc, consumer, _, adminUser, rookSecret, _ := newRGWTestObjects()
	srv := newNotifyTestServer(t, consumer, adminUser, rookSecret)

	version, err := srv.getRGWCredentialsResourceVersion(ctx, consumer, sc)
	assert.NoError(t, err)
	assert.Equal(t, "12345", version)
}
