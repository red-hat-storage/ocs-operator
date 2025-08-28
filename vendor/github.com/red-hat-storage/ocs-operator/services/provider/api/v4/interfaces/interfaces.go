package interfaces

type StorageClientStatus interface {
	GetPlatformVersion() string
	GetOperatorVersion() string
	GetOperatorNamespace() string
	GetClusterID() string
	GetClusterName() string
	GetClientName() string
	GetClientID() string
	GetStorageQuotaUtilizationRatio() float64

	SetPlatformVersion(string) StorageClientStatus
	SetOperatorVersion(string) StorageClientStatus
	SetOperatorNamespace(string) StorageClientStatus
	SetClusterID(string) StorageClientStatus
	SetClusterName(string) StorageClientStatus
	SetClientName(string) StorageClientStatus
	SetClientID(string) StorageClientStatus
	SetStorageQuotaUtilizationRatio(float64) StorageClientStatus
}

type StorageClientOnboarding interface {
	// getters for fields are already provided by protobuf messages
	GetOnboardingTicket() string
	GetConsumerName() string
	GetClientOperatorVersion() string
	GetClientPlatformVersion() string
	GetClientOperatorNamespace() string
	GetClientID() string
	GetClientName() string
	GetClusterID() string
	GetClusterName() string

	SetOnboardingTicket(string) StorageClientOnboarding
	SetConsumerName(string) StorageClientOnboarding
	SetClientOperatorVersion(string) StorageClientOnboarding
	SetClientPlatformVersion(string) StorageClientOnboarding
	SetClientOperatorNamespace(string) StorageClientOnboarding
	SetClientID(string) StorageClientOnboarding
	SetClientName(string) StorageClientOnboarding
	SetClusterID(string) StorageClientOnboarding
	SetClusterName(string) StorageClientOnboarding
}
