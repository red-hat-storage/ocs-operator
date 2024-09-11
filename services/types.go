package services

type OnboardingSubjectRole string

type OnboardingSubjectRoleSpec struct {
	Role          OnboardingSubjectRole `json:"role"`
	ClientOptions ClientRoleOptions     `json:"clientRoleOptions,omitempty"`
}

const (
	ClientRole OnboardingSubjectRole = "ocs-client"
)

type ClientRoleOptions struct {
	StorageQuotaInGiB uint `json:"storageQuotaInGiB,omitempty"`
}

type OnboardingTicket struct {
	ID             string `json:"id"`
	ExpirationDate int64  `json:"expirationDate,string"`

	// SubjectRole specifies the role and options for the role
	SubjectRole OnboardingSubjectRoleSpec `json:"subjectRole"`
}
