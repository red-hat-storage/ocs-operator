package server

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		objIdxByName[obj.GetName()] = idx
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
			if !equality.Semantic.DeepEqual(gotObj, wantObj) {
				t.Fatalf("expected %v to be deep equal to %v", gotObj, wantObj)
			}
		})
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
				return &OCSProviderServer{}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_UNKNOWN,
			},
			ExpectedErrorCode: codes.Internal,
		},
		{
			name: "notify succeeded - obc created",
			setupServer: func(t *testing.T) *OCSProviderServer {
				scheme, schemeErr := newScheme()
				if schemeErr != nil {
					t.Fatalf("newScheme() error = %v", schemeErr)
				}
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(storageConsumer).
					WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
					Build()
				return &OCSProviderServer{
					client:          fakeClient,
					scheme:          scheme,
					consumerManager: createTestConsumerManager(fakeClient),
					namespace:       testNamespace,
				}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_OBC_CREATED,
				Payload:             obcCreatePayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
			validate: func(t *testing.T, srv *OCSProviderServer) {
				expectedName := getObcHashedName(string(storageConsumer.UID), obcName, obcNamespace)
				obc := &nbv1.ObjectBucketClaim{}
				if err := srv.client.Get(ctx, types.NamespacedName{
					Name:      expectedName,
					Namespace: testNamespace,
				}, obc); err != nil {
					t.Fatalf("expected OBC to be created: %v", err)
				}
				if obc.Labels[labelKeyRemoteObcOriginalName] != obcName {
					t.Fatalf("expected label %s=test-obc, got %v", labelKeyRemoteObcOriginalName, obc.Labels)
				}
				if obc.Labels[labelKeyRemoteObcOriginalNamespace] != obcNamespace {
					t.Fatalf("expected label %s=app-namespace, got %v", labelKeyRemoteObcOriginalNamespace, obc.Labels)
				}
				if obc.Labels[labelKeyObcConsumerName] != storageConsumer.Name {
					t.Fatalf("expected label %s=%s, got %v", labelKeyObcConsumerName, storageConsumer.Name, obc.Labels)
				}
				if obc.Labels[labelKeyObcConsumerUUID] != string(storageConsumer.UID) {
					t.Fatalf("expected label %s=%s, got %v", labelKeyObcConsumerUUID, storageConsumer.UID, obc.Labels)
				}
				if obc.Annotations[annotationKeyRemoteObcCreation] != "true" {
					t.Fatalf("expected annotation %s=true, got %v", annotationKeyRemoteObcCreation, obc.Annotations)
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
				return &OCSProviderServer{}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_OBC_CREATED,
				Payload:             []byte("not valid json"),
			},
			ExpectedErrorCode: codes.InvalidArgument,
		},
		{
			name: "notify succeeded - obc created - OBC already exists",
			setupServer: func(t *testing.T) *OCSProviderServer {
				scheme, schemeErr := newScheme()
				if schemeErr != nil {
					t.Fatalf("newScheme() error = %v", schemeErr)
				}
				obcHashedName := getObcHashedName(string(storageConsumer.UID), obcName, obcNamespace)
				existingObc := &nbv1.ObjectBucketClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      obcHashedName,
						Namespace: testNamespace,
					},
				}
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(storageConsumer, existingObc).
					WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
					Build()
				return &OCSProviderServer{
					client:          fakeClient,
					scheme:          scheme,
					consumerManager: createTestConsumerManager(fakeClient),
					namespace:       testNamespace,
				}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_OBC_CREATED,
				Payload:             obcCreatePayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
		},
		{
			name: "notify succeeded - obc deleted",
			setupServer: func(t *testing.T) *OCSProviderServer {
				scheme, schemeErr := newScheme()
				if schemeErr != nil {
					t.Fatalf("newScheme() error = %v", schemeErr)
				}
				obcHashedName := getObcHashedName(string(storageConsumer.UID), obcName, obcNamespace)
				obc := &nbv1.ObjectBucketClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      obcHashedName,
						Namespace: testNamespace,
						Labels: map[string]string{
							labelKeyRemoteObcOriginalName:      obcName,
							labelKeyRemoteObcOriginalNamespace: obcNamespace,
							labelKeyObcConsumerName:            storageConsumer.Name,
						},
					},
				}
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(storageConsumer, obc). // add the obc to the created resources
					WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
					Build()
				return &OCSProviderServer{
					client:          fakeClient,
					scheme:          scheme,
					consumerManager: createTestConsumerManager(fakeClient),
					namespace:       testNamespace,
				}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_OBC_DELETED,
				Payload:             obcDeletepayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
			validate: func(t *testing.T, srv *OCSProviderServer) {
				expectedName := getObcHashedName(string(storageConsumer.UID), obcName, obcNamespace)
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
				scheme, schemeErr := newScheme()
				if schemeErr != nil {
					t.Fatalf("newScheme() error = %v", schemeErr)
				}
				// Storage consumer exists but no OBC in the cluster
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(storageConsumer).
					WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
					Build()
				return &OCSProviderServer{
					client:          fakeClient,
					scheme:          scheme,
					consumerManager: createTestConsumerManager(fakeClient),
					namespace:       testNamespace,
				}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_OBC_DELETED,
				Payload:             obcDeletepayloadBytes,
			},
			ExpectedErrorCode: codes.OK,
		},
		{
			name: "notify failed - obc deleted - payload does not contain the name of the obc",
			setupServer: func(t *testing.T) *OCSProviderServer {
				scheme, schemeErr := newScheme()
				if schemeErr != nil {
					t.Fatalf("newScheme() error = %v", schemeErr)
				}
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(storageConsumer).
					WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
					Build()
				return &OCSProviderServer{
					client:          fakeClient,
					scheme:          scheme,
					consumerManager: createTestConsumerManager(fakeClient),
					namespace:       testNamespace,
				}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_OBC_DELETED,
				Payload:             util.JsonMustMarshal(types.NamespacedName{Name: "", Namespace: obcNamespace}),
			},
			ExpectedErrorCode: codes.InvalidArgument,
		},
		{
			name: "notify failed - obc deleted - payload does not contain the namespace of the obc",
			setupServer: func(t *testing.T) *OCSProviderServer {
				scheme, schemeErr := newScheme()
				if schemeErr != nil {
					t.Fatalf("newScheme() error = %v", schemeErr)
				}
				fakeClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(storageConsumer).
					WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc).
					Build()
				return &OCSProviderServer{
					client:          fakeClient,
					scheme:          scheme,
					consumerManager: createTestConsumerManager(fakeClient),
					namespace:       testNamespace,
				}
			},
			req: &pb.NotifyRequest{
				StorageConsumerUUID: string(storageConsumer.UID),
				Reason:              pb.NotifyReason_OBC_DELETED,
				Payload:             util.JsonMustMarshal(types.NamespacedName{Name: obcName, Namespace: ""}),
			},
			ExpectedErrorCode: codes.InvalidArgument,
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
