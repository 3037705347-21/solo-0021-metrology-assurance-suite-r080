package contracts

import (
	"time"

	"example.com/solo-0021-metrology-assurance-suite/internal/domain"
)

type RegisterDeviceRequest struct {
	AssetTag string `json:"asset_tag"`
	Model    string `json:"model"`
	Room     string `json:"room"`
	Steward  string `json:"steward"`
}

type OpenCaseRequest struct {
	DeviceID  string  `json:"device_id"`
	Nominal   float64 `json:"nominal"`
	Tolerance float64 `json:"tolerance"`
	Steward   string  `json:"steward"`
}

type RecordMeasurementRequest struct {
	Observed   float64 `json:"observed"`
	Technician string  `json:"technician"`
}

type ReopenCaseRequest struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

type SealCaseRequest struct {
	Reviewer string `json:"reviewer"`
	Summary  string `json:"summary"`
}

type RegisterBasisRequest struct {
	DeviceID          string    `json:"device_id"`
	CertificateNumber string    `json:"certificate_number"`
	Issuer            string    `json:"issuer"`
	EffectiveAt       time.Time `json:"effective_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type CreatePlanRequest struct {
	DeviceID       string    `json:"device_id"`
	BasisID        string    `json:"basis_id"`
	WindowStartsAt time.Time `json:"window_starts_at"`
	WindowDueAt    time.Time `json:"window_due_at"`
	Owner          string    `json:"owner"`
}

type ReschedulePlanRequest struct {
	WindowStartsAt time.Time `json:"window_starts_at"`
	WindowDueAt    time.Time `json:"window_due_at"`
	Owner          string    `json:"owner"`
}

type ArrangePlanRequest struct {
	ScheduledFor time.Time `json:"scheduled_for"`
	Assignee     string    `json:"assignee"`
	BasisID      string    `json:"basis_id"`
	CaseID       string    `json:"case_id,omitempty"`
}

type CompletePlanRequest struct {
	CaseID  string `json:"case_id"`
	BasisID string `json:"basis_id"`
}

type DeviceResponse struct {
	Device domain.Device `json:"device"`
}

type CaseResponse struct {
	Case         domain.VerificationCase `json:"case"`
	Measurements []domain.Measurement    `json:"measurements"`
	Seal         *domain.EvidenceSeal    `json:"seal,omitempty"`
	Events       []domain.AuditEvent     `json:"events"`
}

type BasisResponse struct {
	Basis domain.CalibrationBasis `json:"basis"`
}

type BasesResponse struct {
	Bases []domain.CalibrationBasis `json:"bases"`
}

// PlanView augments the stored plan with the status derived at read time, plus
// the workload fields the responsible owner plans against.
type PlanView struct {
	domain.CalibrationPlan
	Status       string `json:"status"`
	StatusReason string `json:"status_reason"`
}

type PlanResponse struct {
	Plan   PlanView            `json:"plan"`
	Events []domain.AuditEvent `json:"events"`
}

type PlansResponse struct {
	Plans []PlanView `json:"plans"`
}

type EventsResponse struct {
	Events []domain.AuditEvent `json:"events"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
