package interfaces

// getters for fields are already provided by protobuf messages

type StorageClientInfo interface {
	GetClientPlatformVersion() string
	GetClientOperatorVersion() string
	GetClientOperatorNamespace() string
	GetClientID() string
	GetClientName() string
	GetClusterID() string
	GetClusterName() string

	SetClientPlatformVersion(string) StorageClientInfo
	SetClientOperatorVersion(string) StorageClientInfo
	SetClientOperatorNamespace(string) StorageClientInfo
	SetClientID(string) StorageClientInfo
	SetClientName(string) StorageClientInfo
	SetClusterID(string) StorageClientInfo
	SetClusterName(string) StorageClientInfo
}

type StorageClientStatus interface {
	StorageClientInfo

	GetStorageQuotaUtilizationRatio() float64

	SetStorageQuotaUtilizationRatio(float64) StorageClientStatus
}

type StorageClientOnboarding interface {
	StorageClientInfo

	GetOnboardingTicket() string
	GetConsumerName() string

	SetOnboardingTicket(string) StorageClientOnboarding
	SetConsumerName(string) StorageClientOnboarding
}
