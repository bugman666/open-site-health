package probe

import "time"

// Availability is the connectivity outcome of one probe.
type Availability string

const (
	Up   Availability = "up"
	Down Availability = "down"
)

// CertStatus is independent of Availability so a live site with an
// expiring certificate is not reported as down.
type CertStatus string

const (
	CertOK      CertStatus = "ok"
	CertWarn    CertStatus = "warn"
	CertExpired CertStatus = "expired"
	CertNA      CertStatus = "n/a"
)

// Result is one check against one target.
type Result struct {
	ID           string       `json:"id"`
	TargetID     string       `json:"target_id"`
	URL          string       `json:"url"`
	CheckedAt    time.Time    `json:"checked_at"`
	Availability Availability `json:"availability"`
	CertStatus   CertStatus   `json:"cert_status"`
	HTTPStatus   int          `json:"http_status,omitempty"`
	CertExpiry   *time.Time   `json:"cert_expiry,omitempty"`
	LatencyMS    int64        `json:"latency_ms,omitempty"`
	Message      string       `json:"message,omitempty"`
}
