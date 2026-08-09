package auth

import "time"

type User struct {
	ID                   string    `json:"id"`
	Email                string    `json:"email"`
	APIKey               string    `json:"api_key,omitempty"`
	PlanID               string    `json:"plan_id"`
	SubscriptionID       string    `json:"subscription_id,omitempty"`
	LicenseKey           string    `json:"license_key,omitempty"`
	Recurrence           string    `json:"recurrence,omitempty"`
	ExpiresAt            time.Time `json:"expires_at"`
	PausedAt             time.Time `json:"paused_at,omitempty"`
	RemainingDays        int       `json:"remaining_days,omitempty"`
	PauseCount           int       `json:"pause_count"`
	LastPausedAt         time.Time `json:"last_paused_at,omitempty"`
	Banned               bool      `json:"banned,omitempty"`
	BanReason            string    `json:"ban_reason,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	SystemIndexerEnabled bool      `json:"system_indexer_enabled"`
}

func (u *User) IsPaused() bool {
	return !u.PausedAt.IsZero()
}
