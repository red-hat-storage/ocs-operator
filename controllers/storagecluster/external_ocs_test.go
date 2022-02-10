package storagecluster

import (
	"fmt"
	"os"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
	externalClient "github.com/red-hat-storage/ocs-operator/services/provider/client"
	"github.com/red-hat-storage/ocs-operator/services/provider/common"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestOnboardConsumer(t *testing.T) {

	var testCases = []struct {
		label       string
		errorType   common.MockError
		expectedErr bool
	}{
		{
			label:       "OnboardConsumer Happy Path",
			errorType:   common.MockError("Happy Path"),
			expectedErr: false,
		},
		{
			label:       "OnboardConsumer Internal Error",
			errorType:   common.OnboardInternalError,
			expectedErr: true,
		},
		{
			label:       "OnboardConsumer Invalid Token",
			errorType:   common.OnboardInvalidToken,
			expectedErr: true,
		},
		{
			label:       "OnboardConsumer Invalid Argument",
			errorType:   common.OnboardInvalidArg,
			expectedErr: true,
		},
	}

	for i, testCase := range testCases {
		t.Logf("Case %d: %s\n", i+1, testCase.label)

		clusterVersion := &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: "version",
			},
		}

		scheme := createFakeScheme(t)

		frecorder := record.NewFakeRecorder(1024)
		reporter := util.NewEventReporter(frecorder)

		r := &StorageClusterReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(clusterVersion).Build(),
			Scheme:   scheme,
			Log:      logf.Log.WithName("controller_storagecluster_test"),
			recorder: reporter,
		}

		instance := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				ExternalStorage: ocsv1.ExternalStorageClusterSpec{
					RequestedCapacity: func(q resource.Quantity) *resource.Quantity { return &q }(resource.MustParse("2Ti")),
				},
			},
		}

		err := os.Setenv(common.MockProviderAPI, string(testCase.errorType))
		assert.NoError(t, err)

		res, err := r.onboardConsumer(instance, &externalClient.OCSProviderClient{})
		if testCase.expectedErr {
			assert.Error(t, err)
			assert.Equal(t, "", instance.Status.ExternalStorage.ConsumerID)
			assert.Equal(t, "0", instance.Status.ExternalStorage.GrantedCapacity.String())
		} else {
			assert.NoError(t, err)
			assert.Equal(t, common.MockConsumerID, instance.Status.ExternalStorage.ConsumerID)
			assert.Equal(t, common.MockGrantedCapacity, instance.Status.ExternalStorage.GrantedCapacity.String())
		}
		assert.True(t, res.IsZero())
	}
}

func TestOffboardConsumer(t *testing.T) {

	var testCases = []struct {
		label       string
		errorType   common.MockError
		expectedErr bool
	}{
		{
			label:       "OffboardConsumer Happy Path",
			errorType:   common.MockError("Happy Path"),
			expectedErr: false,
		},
		{
			label:       "OffboardConsumer Internal Error",
			errorType:   common.OffboardInternalError,
			expectedErr: true,
		},
		{
			label:       "OffboardConsumer Invalid UID",
			errorType:   common.OffboardInvalidUID,
			expectedErr: true,
		},
		{
			label:       "OffboardConsumer Consumer Not Found",
			errorType:   common.OffBoardConsumerNotFound,
			expectedErr: true,
		},
	}

	for i, testCase := range testCases {
		t.Logf("Case %d: %s\n", i+1, testCase.label)

		scheme := createFakeScheme(t)

		frecorder := record.NewFakeRecorder(1024)
		reporter := util.NewEventReporter(frecorder)

		r := &StorageClusterReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:   scheme,
			Log:      logf.Log.WithName("controller_storagecluster_test"),
			recorder: reporter,
		}

		instance := &ocsv1.StorageCluster{}

		err := os.Setenv(common.MockProviderAPI, string(testCase.errorType))
		assert.NoError(t, err)

		res, err := r.offboardConsumer(instance, &externalClient.OCSProviderClient{})
		if testCase.expectedErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		assert.True(t, res.IsZero())
	}
}

func TestUpdateConsumerCapacity(t *testing.T) {

	var testCases = []struct {
		label             string
		errorType         common.MockError
		expectedErr       bool
		requestedCapacity string
	}{
		{
			label:             "UpdateCapacity Happy Path",
			errorType:         common.MockError("Happy Path"),
			expectedErr:       false,
			requestedCapacity: "2Ti",
		},
		{
			label:             "UpdateCapacity UnHappy Path",
			errorType:         common.MockError("UnHappy Path"),
			expectedErr:       true,
			requestedCapacity: "1Ti",
		},
		{
			label:             "UpdateCapacity Internal Error",
			errorType:         common.UpdateInternalError,
			expectedErr:       true,
			requestedCapacity: "2Ti",
		},
		{
			label:             "UpdateCapacity Invalid UID",
			errorType:         common.UpdateInvalidUID,
			expectedErr:       true,
			requestedCapacity: "2Ti",
		},
		{
			label:             "UpdateCapacity Invalid Argument",
			errorType:         common.UpdateInvalidArg,
			expectedErr:       true,
			requestedCapacity: "2Ti",
		},
		{
			label:             "UpdateCapacity Consumer Not Found",
			errorType:         common.UpdateConsumerNotFound,
			expectedErr:       true,
			requestedCapacity: "2Ti",
		},
	}

	for i, testCase := range testCases {
		t.Logf("Case %d: %s\n", i+1, testCase.label)

		scheme := createFakeScheme(t)

		frecorder := record.NewFakeRecorder(1024)
		reporter := util.NewEventReporter(frecorder)

		r := &StorageClusterReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:   scheme,
			Log:      logf.Log.WithName("controller_storagecluster_test"),
			recorder: reporter,
		}

		instance := &ocsv1.StorageCluster{
			Spec: ocsv1.StorageClusterSpec{
				ExternalStorage: ocsv1.ExternalStorageClusterSpec{
					RequestedCapacity: func(q resource.Quantity) *resource.Quantity { return &q }(resource.MustParse(testCase.requestedCapacity)),
				},
			},
		}

		err := os.Setenv(common.MockProviderAPI, string(testCase.errorType))
		assert.NoError(t, err)

		res, err := r.updateConsumerCapacity(instance, &externalClient.OCSProviderClient{})
		if testCase.expectedErr {
			assert.Error(t, err)
			assert.Equal(t, "0", instance.Status.ExternalStorage.GrantedCapacity.String())
			if testCase.requestedCapacity == "1Ti" {
				expectedErr := fmt.Errorf("GrantedCapacity is not equal to the RequestedCapacity in the UpdateCapacity response")
				assert.Equal(t, expectedErr, err)
			}

		} else {
			assert.NoError(t, err)
			assert.Equal(t, common.MockGrantedCapacity, instance.Status.ExternalStorage.GrantedCapacity.String())
		}
		assert.True(t, res.IsZero())
	}
}

func TestGetExternalConfigFromProvider(t *testing.T) {

	var testCases = []struct {
		label       string
		errorType   common.MockError
		expectedErr bool
	}{
		{
			label:       "GetStorageConfig Happy Path",
			errorType:   common.MockError("Happy Path"),
			expectedErr: false,
		},
		{
			label:       "GetStorageConfig Internal Error",
			errorType:   common.StorageConfigInternalError,
			expectedErr: true,
		},
		{
			label:       "GetStorageConfig Invalid UID",
			errorType:   common.StorageConfigInvalidUID,
			expectedErr: true,
		},
		{
			label:       "GetStorageConfig Consumer Not Found",
			errorType:   common.StorageConfigConsumerNotReady,
			expectedErr: true,
		},
	}

	for i, testCase := range testCases {
		t.Logf("Case %d: %s\n", i+1, testCase.label)

		scheme := createFakeScheme(t)

		frecorder := record.NewFakeRecorder(1024)
		reporter := util.NewEventReporter(frecorder)

		r := &StorageClusterReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:   scheme,
			Log:      logf.Log.WithName("controller_storagecluster_test"),
			recorder: reporter,
		}

		instance := &ocsv1.StorageCluster{}

		err := os.Setenv(common.MockProviderAPI, string(testCase.errorType))
		assert.NoError(t, err)

		externalResources, res, err := r.getExternalConfigFromProvider(instance, &externalClient.OCSProviderClient{})
		if testCase.expectedErr && testCase.errorType == common.StorageConfigConsumerNotReady {
			assert.NoError(t, err)
			assert.Empty(t, externalResources)
			assert.False(t, res.IsZero())
			assert.Equal(t, reconcile.Result{RequeueAfter: time.Second * 5}, res)
		} else if testCase.expectedErr {
			assert.Error(t, err)
			assert.Empty(t, externalResources)
			assert.True(t, res.IsZero())
		} else {
			assert.NoError(t, err)
			assert.NotEmpty(t, externalResources)
			assert.True(t, res.IsZero())
		}
	}
}
