package storagecluster

import (
	//"context"
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCephRGWRoutes(t *testing.T) {
	var cases = []struct {
		label                string
		createRuntimeObjects bool
		platform             configv1.PlatformType
	}{
		{
			label:                "case 1", // Ensure that RGW routes are created on non-cloud Platform
			createRuntimeObjects: false,
		},
	}

	for _, c := range cases {
		platform.SetFakePlatformInstanceForTesting(true, c.platform)
		var objects []client.Object
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, objects, nil)
		if c.createRuntimeObjects {
			objects = createUpdateRuntimeObjects(t) //nolint:staticcheck //no need to use objects as they update in runtime
		}
		assertCephRGWRoutes(t, reconciler, cr, request)
		platform.UnsetFakePlatformInstanceForTesting()
	}
}
func assertCephRGWRoutes(t *testing.T, reconciler *StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	expectedCos, err := reconciler.newCephRGWRoutes(cr)
	assert.NoError(t, err)
	actualCos := &routev1.Route{}
	request.Name = "ocsinit-cephobjectstore"
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, actualCos)
	// for any cloud platform, 'route' should not be created
	// 'Get' should have thrown an error
	platformType, detectErr := platform.GetPlatformType()
	assert.NoError(t, detectErr)
	if platform.SkipObjectStore(platformType) {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		//Uses the same name as the cephobjectstore
		actualCos := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-cephobjectstore",
			},
			Spec: routev1.RouteSpec{
				To: routev1.RouteTargetReference{
					Kind: "Service",
					Name: "rook-ceph-rgw-ocsinit-cephobjectstore",
				},
				Port: &routev1.RoutePort{
					TargetPort: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "http",
					},
				},
				TLS: &routev1.TLSConfig{
					Termination:                   routev1.TLSTerminationEdge,
					InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyAllow,
				},
			},
		}
		actualCosSecure := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-cephobjectstore-secure",
			},
			Spec: routev1.RouteSpec{
				To: routev1.RouteTargetReference{
					Kind: "Service",
					Name: "rook-ceph-rgw-ocsinit-cephobjectstore",
				},
				Port: &routev1.RoutePort{
					TargetPort: intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "https",
					},
				},
				TLS: &routev1.TLSConfig{
					Termination:                   routev1.TLSTerminationReencrypt,
					InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
				},
			},
		}
		assert.Equal(t, expectedCos[0].ObjectMeta.Name, actualCos.ObjectMeta.Name)
		assert.Equal(t, expectedCos[0].Spec, actualCos.Spec)
		assert.Equal(t, expectedCos[1].ObjectMeta.Name, actualCosSecure.ObjectMeta.Name)
		assert.Equal(t, expectedCos[1].Spec, actualCosSecure.Spec)
	}

	assert.Equal(t, len(expectedCos[0].OwnerReferences), 1)
}
