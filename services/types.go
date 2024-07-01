package services

type Role string

const (
	ClientRole        Role = "client"
	MirroringPeerRole Role = "mirroring-peer"
)

type OnboardingTicket struct {
	ID             string `json:"id"`
	ExpirationDate int64  `json:"expirationDate,string"`
	// Role can be either client or mirroring-peer
	Role Role `json:"role,string"`
}
