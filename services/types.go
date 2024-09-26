package services

type OnboardingSubjectRole string

const (
	ClientRole OnboardingSubjectRole = "ocs-client"
	PeerRole   OnboardingSubjectRole = "ocs-peer"
)

type OnboardingTicket struct {
	ID                string                `json:"id"`
	ExpirationDate    int64                 `json:"expirationDate,string"`
	SubjectRole       OnboardingSubjectRole `json:"subjectRole"`
	StorageQuotaInGiB *uint                 `json:"storageQuotaInGiB,omitempty"`
}
