package interfaces

type StorageClientStatus interface {
	// TODO: it was mistake not using full name of the field and we are just
	// doing indirection for getters, change this interface after ensuring
	// no client is using it
	GetPlatformVersion() string
	GetOperatorVersion() string
	GetClusterID() string
	GetClusterName() string
	GetClientName() string
	GetStorageQuotaUtilizationRatio() float64

	SetPlatformVersion(string) StorageClientStatus
	SetOperatorVersion(string) StorageClientStatus
	SetClusterID(string) StorageClientStatus
	SetClusterName(string) StorageClientStatus
	SetClientName(string) StorageClientStatus
	SetStorageQuotaUtilizationRatio(float64) StorageClientStatus
}

type StorageClientOnboarding interface {
	// getters for fields are already provided by protobuf messages
	GetOnboardingTicket() string
	GetConsumerName() string
	GetClientOperatorVersion() string

	SetOnboardingTicket(string) StorageClientOnboarding
	SetConsumerName(string) StorageClientOnboarding
	SetClientOperatorVersion(string) StorageClientOnboarding
}
