package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	"github.com/red-hat-storage/ocs-operator/v4/services"
	"github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

func TestOnboardConsumer(t *testing.T) {
	tests := []struct {
		name                  string
		consumer              *ocsv1a1.StorageConsumer
		clientOperatorVersion string
		updateCache           func(t *testing.T, consumerManager *ocsConsumerManager)
		onboardingTicket      func(t *testing.T, fakeClient client.Client) string
		expectError           bool
		expectedGRPCCode      codes.Code
		expectedErrorMessage  string
		expectedConsumerUID   string
	}{
		{
			name:                  "empty client operator version",
			consumer:              nil,
			clientOperatorVersion: "",
			expectError:           true,
			expectedGRPCCode:      codes.InvalidArgument,
			expectedErrorMessage:  "malformed ClientOperatorVersion",
		},
		{
			name:                  "malformed client operator version",
			consumer:              nil,
			clientOperatorVersion: "invalid-version",
			expectError:           true,
			expectedGRPCCode:      codes.InvalidArgument,
			expectedErrorMessage:  "malformed ClientOperatorVersion",
		},
		{
			name:                  "version mismatch - major version different",
			consumer:              nil,
			clientOperatorVersion: "3.15.0",
			expectError:           true,
			expectedGRPCCode:      codes.FailedPrecondition,
			expectedErrorMessage:  "major and minor versions should match",
		},
		{
			name:                  "version mismatch - minor version different",
			consumer:              nil,
			clientOperatorVersion: "4.14.0",
			expectError:           true,
			expectedGRPCCode:      codes.FailedPrecondition,
			expectedErrorMessage:  "major and minor versions should match",
		},
		{
			name:                  "onboarding key secret not found",
			consumer:              nil,
			clientOperatorVersion: version.Version,
			expectError:           true,
			expectedGRPCCode:      codes.Internal,
			expectedErrorMessage:  "failed to get public key",
		},
		{
			name:                  "empty consumer name",
			consumer:              nil,
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)
				return "test-ticket"
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name:                  "empty onboarding ticket",
			consumer:              nil,
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)
				return ""
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name: "invalid ticket format - no dot separator",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)
				return "invalid-ticket-no-dot"
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name: "invalid ticket format - multiple dots",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)
				return "part1.part2.part3"
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name: "invalid base64 in ticket",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)
				return "invalid-base64!@#.signature"
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name: "invalid JSON in ticket",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)
				// Create valid base64 but invalid JSON
				invalidJSON := "{invalid-json"
				encodedJSON := base64.StdEncoding.EncodeToString([]byte(invalidJSON))
				return encodedJSON + ".signature"
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name: "ticket with peer role instead of client role",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				ticket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(24 * time.Hour).Unix(),
					SubjectRole:    services.PeerRole,
				}

				return signOnboardingTicket(t, keyPair.privateKey, ticket)
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "expecting role ocs-client found role ocs-peer",
		},
		{
			name: "ticket with invalid role",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				ticket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(24 * time.Hour).Unix(),
					SubjectRole:    "invalid-role",
				}

				return signOnboardingTicket(t, keyPair.privateKey, ticket)
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name: "expired ticket",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				ticket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(-24 * time.Hour).Unix(),
					SubjectRole:    services.ClientRole,
				}

				return signOnboardingTicket(t, keyPair.privateKey, ticket)
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name: "invalid signature in ticket",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				ticket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(24 * time.Hour).Unix(),
					SubjectRole:    services.ClientRole,
				}

				// Create ticket with valid JSON but invalid signature
				ticketJSON, _ := json.Marshal(ticket)
				encodedJSON := base64.StdEncoding.EncodeToString(ticketJSON)
				invalidSignature := base64.StdEncoding.EncodeToString([]byte("invalid-signature"))
				return encodedJSON + "." + invalidSignature
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "onboarding ticket is not valid",
		},
		{
			name:                  "consumer not found",
			consumer:              nil,
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				ticket := services.OnboardingTicket{
					ID:             "non-existent-consumer",
					ExpirationDate: time.Now().Add(24 * time.Hour).Unix(),
					SubjectRole:    services.ClientRole,
				}

				return signOnboardingTicket(t, keyPair.privateKey, ticket)
			},
			expectError:          true,
			expectedGRPCCode:     codes.Internal,
			expectedErrorMessage: "failed to get storageconsumer",
		},
		{
			name: "consumer already enabled",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
				Spec: ocsv1a1.StorageConsumerSpec{
					Enable: true, // Already enabled
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)

				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				// Create and sign ticket
				ticket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(24 * time.Hour).Unix(),
					SubjectRole:    services.ClientRole,
				}
				signedTicket := signOnboardingTicket(t, keyPair.privateKey, ticket)

				// Create onboarding secret
				createOnboardingSecret(t, fakeClient, "test-consumer-uid-123", signedTicket)

				return signedTicket
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "refusing to onboard onto storageconsumer",
		},
		{
			name: "onboarding secret not found",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				ticket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(24 * time.Hour).Unix(),
					SubjectRole:    services.ClientRole,
				}

				// NOTE: Intentionally NOT creating the onboarding-token secret
				// This test case verifies the "onboarding secret not found" error
				return signOnboardingTicket(t, keyPair.privateKey, ticket)
			},
			expectError:          true,
			expectedGRPCCode:     codes.Internal,
			expectedErrorMessage: "failed to get onboarding secret",
		},
		{
			name: "ticket mismatch with secret",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)
				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				// Create consumer secret with a different ticket than what we'll return
				createOnboardingSecret(t, fakeClient, "test-consumer-uid-123", "stored-ticket")

				// Create a valid ticket with different content than what's stored in the secret
				differentTicket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(48 * time.Hour).Unix(), // Different expiration
					SubjectRole:    services.ClientRole,
				}

				return signOnboardingTicket(t, keyPair.privateKey, differentTicket)
			},
			expectError:          true,
			expectedGRPCCode:     codes.InvalidArgument,
			expectedErrorMessage: "supplied onboarding ticket does not match mapped secret",
		},
		{
			name: "successful onboarding with complete flow",
			consumer: &ocsv1a1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: testNamespace,
					UID:       "test-consumer-uid-123",
				},
			},
			clientOperatorVersion: version.Version,
			onboardingTicket: func(t *testing.T, fakeClient client.Client) string {
				keyPair := generateTestKeyPair(t)

				createOnboardingKeySecret(t, fakeClient, keyPair.publicPEM)

				// Create and sign ticket
				ticket := services.OnboardingTicket{
					ID:             "test-consumer",
					ExpirationDate: time.Now().Add(24 * time.Hour).Unix(),
					SubjectRole:    services.ClientRole,
				}
				signedTicket := signOnboardingTicket(t, keyPair.privateKey, ticket)

				// Create onboarding secret
				createOnboardingSecret(t, fakeClient, "test-consumer-uid-123", signedTicket)

				return signedTicket
			},
			updateCache: func(t *testing.T, consumerManager *ocsConsumerManager) {
				consumerManager.nameByUID[types.UID("test-consumer-uid-123")] = "test-consumer"
			},
			expectedConsumerUID: "test-consumer-uid-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&ocsv1a1.StorageConsumer{}).
				Build()

			consumerManager := &ocsConsumerManager{
				client:    fakeClient,
				namespace: testNamespace,
				nameByUID: make(map[types.UID]string),
			}

			server := &OCSProviderServer{
				client:          fakeClient,
				namespace:       testNamespace,
				consumerManager: consumerManager,
			}

			if tt.updateCache != nil {
				tt.updateCache(t, server.consumerManager)
			}

			consumerName := ""
			if tt.consumer != nil {
				err := fakeClient.Create(context.Background(), tt.consumer)
				assert.NoError(t, err)
				consumerName = tt.consumer.Name
			} else if tt.name == "consumer not found" {
				// Special case: ticket references non-existent consumer
				consumerName = "non-existent-consumer"
			}

			// Get the onboarding ticket from the function
			onboardingTicket := ""
			if tt.onboardingTicket != nil {
				onboardingTicket = tt.onboardingTicket(t, fakeClient)
			}

			req := &pb.OnboardConsumerRequest{
				ConsumerName:          consumerName,
				ClientOperatorVersion: tt.clientOperatorVersion,
				OnboardingTicket:      onboardingTicket,
			}

			resp, err := server.OnboardConsumer(context.TODO(), req)

			if !tt.expectError {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, tt.expectedConsumerUID, resp.GetStorageConsumerUUID())

				// Verify consumer was enabled
				consumer := &ocsv1a1.StorageConsumer{}
				consumer.Name = tt.consumer.Name
				consumer.Namespace = testNamespace
				err = fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(consumer), consumer)
				assert.NoError(t, err)
				assert.True(t, consumer.Spec.Enable)
			} else {
				assert.Error(t, err)
				assert.Nil(t, resp)

				st, ok := status.FromError(err)
				assert.True(t, ok, "Error should be a gRPC status error")
				assert.Equal(t, tt.expectedGRPCCode, st.Code())
				assert.Contains(t, err.Error(), tt.expectedErrorMessage)
			}
		})
	}
}
