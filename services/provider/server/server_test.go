package server

import (
	"context"
	"reflect"
	"testing"

	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
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

func TestGetExternalEndpointConfig(t *testing.T) {
	namespace := "test-ns"
	ctx := context.Background()

	routeWithTLSAndAdmitted := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: noobaaS3RouteName, Namespace: namespace},
		Spec: routev1.RouteSpec{
			TLS: &routev1.TLSConfig{Termination: routev1.TLSTerminationEdge},
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{{
				Host: "s3.example.com",
				Conditions: []routev1.RouteIngressCondition{{
					Type:   routev1.RouteAdmitted,
					Status: corev1.ConditionTrue,
				}},
			}},
		},
	}

	routeNoTLSAdmitted := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: noobaaS3RouteName, Namespace: namespace},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{{
				Host: "s3.example.com",
				Conditions: []routev1.RouteIngressCondition{{
					Type:   routev1.RouteAdmitted,
					Status: corev1.ConditionTrue,
				}},
			}},
		},
	}

	routeAdmittedButNoHost := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: noobaaS3RouteName, Namespace: namespace},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{{
				Host: "",
				Conditions: []routev1.RouteIngressCondition{{
					Type:   routev1.RouteAdmitted,
					Status: corev1.ConditionTrue,
				}},
			}},
		},
	}

	routeNotAdmitted := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: noobaaS3RouteName, Namespace: namespace},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{{
				Host: "s3.example.com",
				Conditions: []routev1.RouteIngressCondition{{
					Type:   routev1.RouteAdmitted,
					Status: corev1.ConditionFalse,
				}},
			}},
		},
	}

	tests := []struct {
		name       string
		objects    []client.Object
		namespace  string
		wantConfig []*pb.ExternalEndpointConfig
		wantErr    bool
	}{
		{
			name:       "route not found returns nil config and no error",
			objects:    nil,
			namespace:  namespace,
			wantConfig: nil,
			wantErr:    false,
		},
		{
			name:       "route with TLS and admitted ingress returns https URL",
			objects:    []client.Object{routeWithTLSAndAdmitted},
			namespace:  namespace,
			wantConfig: []*pb.ExternalEndpointConfig{{ExposeAs: "noobaaS3", EndpointUrl: "https://s3.example.com"}},
			wantErr:    false,
		},
		{
			name:       "route without TLS and admitted ingress returns http URL",
			objects:    []client.Object{routeNoTLSAdmitted},
			namespace:  namespace,
			wantConfig: []*pb.ExternalEndpointConfig{{ExposeAs: "noobaaS3", EndpointUrl: "http://s3.example.com"}},
			wantErr:    false,
		},
		{
			name:       "route admitted but empty host returns nil config",
			objects:    []client.Object{routeAdmittedButNoHost},
			namespace:  namespace,
			wantConfig: nil,
			wantErr:    false,
		},
		{
			name:       "route not admitted returns nil config",
			objects:    []client.Object{routeNotAdmitted},
			namespace:  namespace,
			wantConfig: nil,
			wantErr:    false,
		},
	}

	scheme, err := newScheme()
	assert.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()
			srv := &OCSProviderServer{client: fakeClient}

			got, err := srv.getExternalEndpointConfig(ctx, tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Fatalf("getExternalEndpointConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != len(tt.wantConfig) {
				t.Errorf("getExternalEndpointConfig() length = %d, want %d", len(got), len(tt.wantConfig))
			} else {
				for i := range got {
					if got[i].GetExposeAs() != tt.wantConfig[i].GetExposeAs() || got[i].GetEndpointUrl() != tt.wantConfig[i].GetEndpointUrl() {
						t.Errorf("getExternalEndpointConfig()[%d] = ExposeAs=%q EndpointUrl=%q, want ExposeAs=%q EndpointUrl=%q",
							i, got[i].GetExposeAs(), got[i].GetEndpointUrl(), tt.wantConfig[i].GetExposeAs(), tt.wantConfig[i].GetEndpointUrl())
					}
				}
			}
		})
	}
}
