package services

type OnboardingTicket struct {
	ID                string `json:"id"`
	ExpirationDate    int64  `json:"expirationDate,string"`
	StorageQuotaInGiB uint   `json:"storageQuotaInGiB,omitempty"`
}
