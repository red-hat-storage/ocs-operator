package common

type MockError string

const (
	// Onboarding
	OnboardInternalError MockError = "ONBOARD_INTERNAL_ERROR"
	OnboardInvalidToken  MockError = "ONBOARD_INVALID_TOKEN"
	OnboardInvalidArg    MockError = "ONBOARD_INVALID_ARG"

	// StorageConfig
	StorageConfigInternalError    MockError = "STORAGE_CONFIG_INTERNAL_ERROR"
	StorageConfigInvalidUID       MockError = "STORAGE_CONFIG_INVALID_UID"
	StorageConfigConsumerNotReady MockError = "STORAGE_CONFIG_CONSUMER_NOT_READY"

	// Offboard
	OffboardInternalError    MockError = "OFFBOARD_INTERNAL_ERROR"
	OffboardInvalidUID       MockError = "OFFBOARD_INVALID_UID"
	OffBoardConsumerNotFound MockError = "OFFBOARD_CONSUMER_NOT_FOUND"

	// Update
	UpdateInternalError    MockError = "UPDATE_INTERNAL_ERROR"
	UpdateInvalidArg       MockError = "UPDATE_INVALID_ARG"
	UpdateInvalidUID       MockError = "UPDATE_INVALID_UID"
	UpdateConsumerNotFound MockError = "UPDATE_CONSUMER_NOT_FOUND"

	MockConsumerID = "vMHA0ppPbjg5TlgvMFcaH4QlQEJB68u+1jWQJ9O9xvde8fxz5vBuu2F6bVIY6pAYLVrC3FajrK1KxmhFTzNDow=="

	MockProviderAPI = "MOCK_PROVIDER_API"
)

var (
	MockMonSecretData = map[string]string{
		"admin-secret": "admin-secret",
		"fsid":         "73af59d4-44d4-4154-98cf-b063d1c02315",
		"mon-secret":   "mon-secret",
	}

	MockMonConfigMapData = map[string]string{
		"maxMonId": "0",
		"data":     "a=10.20.30.40:1234",
		"mapping":  "{}",
	}
)
