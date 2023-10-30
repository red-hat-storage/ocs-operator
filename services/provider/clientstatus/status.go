package clientstatus

type StorageClientStatus interface {
	GetPlatformVersion() string
	GetOperatorVersion() string

	SetPlatformVersion(string) StorageClientStatus
	SetOperatorVersion(string) StorageClientStatus
}
