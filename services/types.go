package services

type OnboardingSubjectRole string

const (
	ClientRole OnboardingSubjectRole = "ocs-client"
	PeerRole   OnboardingSubjectRole = "ocs-peer"
)

type OnboardingTicket struct {
	ID                string `json:"id"`
	ExpirationDate    int64  `json:"expirationDate,string"`
	StorageQuotaInGiB uint   `json:"storageQuotaInGiB,omitempty"`
	// OnboardingSubjectRole can be either ocs-client or ocs-peer
	SubjectRole OnboardingSubjectRole `json:"subjectRole,string"`
}
