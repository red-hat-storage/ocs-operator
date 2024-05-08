package services

type OnboardingTicket struct {
	ExpirationDate int64  `json:"expirationDate,string"`
	Quota          string `json:"quota"`
}
