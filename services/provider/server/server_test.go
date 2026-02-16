package server

import (
	"context"
	"reflect"
	"testing"

	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// currently Notify is unimplemented, so we just test that the unimplemented error is returned
func TestNotify_Unimplemented(t *testing.T) {
	// setup (service and request)
	srv := &OCSProviderServer{}
	req := &pb.NotifyRequest{
		StorageConsumerUUID: "storage-consumer-123",
		Reason:              pb.NotifyReason_OBC_CREATED,
	}

	// call Notify
	ctx := context.Background()
	resp, err := srv.Notify(ctx, req)

	// check the response and error
	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected Unimplemented, got %v", status.Code(err))
	}
}

func TestOffboardConsumer(t *testing.T) {
	obc := &nbv1.ObjectBucketClaim{}
	obc.Name = "test-obc-name"
	obc.Namespace = "test-app"
	obc.Labels = map[string]string{"storage-consumer-uuid": "consumer-uid-123"}

	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer",
			Namespace: testNamespace,
			UID:       "consumer-uid-456",
		},
	}

	tests := []struct {
		name              string
		objs              []client.Object
		consumerUUID      string
		isErrorExpected   bool
		expectedErrorCode codes.Code
	}{
		{
			name:              "block offboarding when OBCs exist",
			objs:              []client.Object{obc},
			consumerUUID:      "consumer-uid-123",
			isErrorExpected:   true,
			expectedErrorCode: codes.FailedPrecondition,
		},
		{
			name:            "succeed offboarding when no OBCs",
			objs:            []client.Object{consumer},
			consumerUUID:    "consumer-uid-456",
			isErrorExpected: false,
		},
	}

	scheme, err := newScheme()
	assert.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// WithStatusSubresource and WithIndex are required for the success case:
			// ClearClientInformation looks up StorageConsumer by UID (via List+MatchingFields)
			// and patches status; the fake client needs the index and status subresource for that.
			b := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).
				WithStatusSubresource(&ocsv1a1.StorageConsumer{}).
				WithIndex(&ocsv1a1.StorageConsumer{}, util.ObjectUidIndexName, util.ObjectUidIndexFieldFunc)
			fakeClient := b.Build()

			srv := &OCSProviderServer{
				client:          fakeClient,
				consumerManager: createTestConsumerManager(fakeClient),
				namespace:       testNamespace,
			}

			ctx := context.Background()
			req := &pb.OffboardConsumerRequest{StorageConsumerUUID: tt.consumerUUID}
			resp, err := srv.OffboardConsumer(ctx, req)

			if tt.isErrorExpected {
				assert.Nil(t, resp)
				assert.Error(t, err)
				assert.Equal(t, tt.expectedErrorCode, status.Code(err))
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}
