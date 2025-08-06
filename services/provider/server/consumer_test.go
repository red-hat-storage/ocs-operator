package server

import (
	"context"
	"testing"

	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	providerClient "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/client"
	"github.com/red-hat-storage/ocs-operator/services/provider/api/v4/interfaces"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func createTestConsumerManager(client client.Client) *ocsConsumerManager {
	return &ocsConsumerManager{
		client:    client,
		namespace: testNamespace,
		nameByUID: make(map[types.UID]string),
	}
}

func TestEnableStorageConsumer(t *testing.T) {
	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer",
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
	}

	tests := []struct {
		name         string
		consumer     *ocsv1a1.StorageConsumer
		existingObjs []client.Object
		expectError  bool
	}{
		{
			name:         "successful enable",
			consumer:     consumer,
			existingObjs: []client.Object{consumer},
			expectError:  false,
		},
		{
			name:         "consumer not found",
			consumer:     consumer,
			existingObjs: []client.Object{},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			_, err = testConsumerManager.EnableStorageConsumer(context.TODO(), tt.consumer)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetByName(t *testing.T) {
	tests := []struct {
		name         string
		consumerName string
		existingObjs []client.Object
		expectError  bool
		expectedUID  string
	}{
		{
			name:         "successful get by name",
			consumerName: "test-consumer",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
					},
				},
			},
			expectError: false,
			expectedUID: "test-uid-123",
		},
		{
			name:         "consumer not found",
			consumerName: "test-consumer",
			existingObjs: []client.Object{},
			expectError:  true,
		},
		{
			name:         "consumer in different namespace",
			consumerName: "test-consumer",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: "different-namespace",
						UID:       "test-uid-123",
					},
				},
			},
			expectError: true,
		},
		{
			name:         "empty consumer name",
			consumerName: "",
			existingObjs: []client.Object{},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			result, err := testConsumerManager.GetByName(context.TODO(), tt.consumerName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedUID, string(result.UID))
			}
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name         string
		consumerID   string
		existingObjs []client.Object
		setupFunc    func(*ocsConsumerManager)
		expectError  bool
		expectedName string
	}{
		{
			name:       "successful get by ID",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
					},
				},
			},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},
			expectError:  false,
			expectedName: "test-consumer",
		},
		{
			name:       "consumer not found in cache",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
					},
				},
			},
			setupFunc:   nil,
			expectError: true,
		},
		{
			name:         "consumer not found in cluster",
			consumerID:   "non-existing-uid",
			existingObjs: []client.Object{},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("non-existing-uid")] = "non-existing-consumer"
			},
			expectError: true,
		},
		{
			name:         "empty consumer ID",
			consumerID:   "",
			existingObjs: []client.Object{},
			expectError:  true,
		},
		{
			name:       "consumer name in cache but UID mismatch",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "different-uid",
					},
				},
			},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			if tt.setupFunc != nil {
				tt.setupFunc(testConsumerManager)
			}

			result, err := testConsumerManager.Get(context.TODO(), tt.consumerID)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedName, result.Name)
			}
		})
	}
}

func TestUpdateConsumerStatus(t *testing.T) {
	tests := []struct {
		name         string
		consumerID   string
		existingObjs []client.Object
		setupFunc    func(*ocsConsumerManager)
		status       interfaces.StorageClientStatus
		expectError  bool
	}{
		{
			name:       "successful status update",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
					},
				},
			},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},

			status: func() interfaces.StorageClientStatus {
				status := providerClient.NewStorageClientStatus()
				status.SetClientID("client-456")
				status.SetPlatformVersion("4.16.0")
				status.SetOperatorVersion("4.16.0")
				status.SetOperatorNamespace("test-namespace")
				status.SetClusterID("cluster-789")
				status.SetClientName("test-client-name")
				status.SetClusterName("test-cluster-name")
				status.SetStorageQuotaUtilizationRatio(0.75)
				return status
			}(),
			expectError: false,
		},
		{
			name:         "consumer not found",
			consumerID:   "non-existing-uid",
			existingObjs: []client.Object{},
			setupFunc:    nil,
			status:       nil,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&ocsv1a1.StorageConsumer{}).
				WithObjects(tt.existingObjs...).
				Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			if tt.setupFunc != nil {
				tt.setupFunc(testConsumerManager)
			}

			err = testConsumerManager.UpdateConsumerStatus(context.TODO(), tt.consumerID, tt.status)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAddAnnotation(t *testing.T) {
	tests := []struct {
		name         string
		consumerID   string
		existingObjs []client.Object
		setupFunc    func(*ocsConsumerManager)
		key          string
		value        string
		expectError  bool
	}{
		{
			name:       "successful add annotation",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
					},
				},
			},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},
			key:         "test-key",
			value:       "test-value",
			expectError: false,
		},
		{
			name:         "consumer not found",
			consumerID:   "non-existing-uid",
			existingObjs: []client.Object{},
			setupFunc:    nil,
			key:          "test-key",
			value:        "test-value",
			expectError:  true,
		},
		{
			name:       "update existing annotation",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
						Annotations: map[string]string{
							"existing-key": "old-value",
						},
					},
				},
			},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},
			key:         "existing-key",
			value:       "new-value",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			if tt.setupFunc != nil {
				tt.setupFunc(testConsumerManager)
			}

			err = testConsumerManager.AddAnnotation(context.TODO(), tt.consumerID, tt.key, tt.value)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				actualConsumer := &ocsv1a1.StorageConsumer{}
				actualConsumer.Name = "test-consumer"
				actualConsumer.Namespace = testNamespace
				err := fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(actualConsumer), actualConsumer)
				assert.NoError(t, err)
				assert.Equal(t, actualConsumer.GetAnnotations()[tt.key], tt.value)
			}
		})
	}
}

func TestRemoveAnnotation(t *testing.T) {
	tests := []struct {
		name         string
		consumerID   string
		existingObjs []client.Object
		setupFunc    func(*ocsConsumerManager)
		key          string
		expectError  bool
	}{
		{
			name:       "successful remove annotation",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
						Annotations: map[string]string{
							"test-key": "test-value",
							"keep-key": "keep-value",
						},
					},
				},
			},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},
			key:         "test-key",
			expectError: false,
		},
		{
			name:       "remove non-existing annotation",
			consumerID: "test-uid-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: testNamespace,
						UID:       "test-uid-123",
						Annotations: map[string]string{
							"test-key": "test-value",
							"keep-key": "keep-value",
						},
					},
				},
			},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},
			key:         "non-existing-key",
			expectError: false,
		},
		{
			name:         "consumer not found",
			consumerID:   "non-existing-uid",
			existingObjs: []client.Object{},
			setupFunc:    nil,
			key:          "test-key",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			if tt.setupFunc != nil {
				tt.setupFunc(testConsumerManager)
			}

			err = testConsumerManager.RemoveAnnotation(context.TODO(), tt.consumerID, tt.key)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				actualConsumer := &ocsv1a1.StorageConsumer{}
				actualConsumer.Name = "test-consumer"
				actualConsumer.Namespace = testNamespace
				err := fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(actualConsumer), actualConsumer)
				assert.NoError(t, err)
				assert.NotContains(t, actualConsumer.GetAnnotations(), tt.key)
			}
		})
	}
}

func TestGetByClientID(t *testing.T) {
	consumer1 := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer-1",
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
		Status: ocsv1a1.StorageConsumerStatus{
			Client: &ocsv1a1.ClientStatus{
				ID: "client-123",
			},
		},
	}

	consumer2 := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer-2",
			Namespace: testNamespace,
			UID:       "test-uid-456",
		},
		Status: ocsv1a1.StorageConsumerStatus{
			Client: &ocsv1a1.ClientStatus{
				ID: "client-456",
			},
		},
	}

	tests := []struct {
		name         string
		clientID     string
		existingObjs []client.Object
		found        bool
		expectedName string
	}{
		{
			name:         "successful get by client ID",
			clientID:     "client-123",
			existingObjs: []client.Object{consumer1, consumer2},
			found:        true,
			expectedName: "test-consumer-1",
		},
		{
			name:         "client not found",
			clientID:     "non-existing-client",
			existingObjs: []client.Object{consumer1, consumer2},
			found:        false,
		},
		{
			name:         "empty client ID",
			clientID:     "",
			existingObjs: []client.Object{consumer1, consumer2},
			found:        false,
		},
		{
			name:         "no consumers",
			clientID:     "client-123",
			existingObjs: []client.Object{},
			found:        false,
		},
		{
			name:     "consumer with nil client status",
			clientID: "client-123",
			existingObjs: []client.Object{
				&ocsv1a1.StorageConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer-nil-client",
						Namespace: testNamespace,
						UID:       "test-uid-nil",
					},
					Status: ocsv1a1.StorageConsumerStatus{
						Client: nil,
					},
				},
			},
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			result, err := testConsumerManager.GetByClientID(context.TODO(), tt.clientID)

			if !tt.found {
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedName, result.Name)
			}
		})
	}
}

func TestClearClientInformation(t *testing.T) {
	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-consumer",
			Namespace: testNamespace,
			UID:       "test-uid-123",
		},
		Status: ocsv1a1.StorageConsumerStatus{
			Client: &ocsv1a1.ClientStatus{
				ID:   "client-123",
				Name: "client-name",
			},
		},
	}

	tests := []struct {
		name         string
		consumerID   string
		existingObjs []client.Object
		setupFunc    func(*ocsConsumerManager)
		expectError  bool
	}{
		{
			name:         "successful clear client information",
			consumerID:   "test-uid-123",
			existingObjs: []client.Object{consumer},
			setupFunc: func(testConsumerManager *ocsConsumerManager) {
				testConsumerManager.nameByUID[types.UID("test-uid-123")] = "test-consumer"
			},
			expectError: false,
		},
		{
			name:         "consumer not found",
			consumerID:   "non-existing-uid",
			existingObjs: []client.Object{},
			setupFunc:    nil,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&ocsv1a1.StorageConsumer{}).
				WithObjects(tt.existingObjs...).
				Build()
			testConsumerManager := createTestConsumerManager(fakeClient)

			if tt.setupFunc != nil {
				tt.setupFunc(testConsumerManager)
			}

			err = testConsumerManager.ClearClientInformation(context.TODO(), tt.consumerID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				actualConsumer := &ocsv1a1.StorageConsumer{}
				err := fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(consumer), actualConsumer)
				assert.NoError(t, err)
				assert.Nil(t, actualConsumer.Status.Client)
			}
		})
	}
}
