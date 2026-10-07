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

type CreatePlanRequest struct {
	DeviceID        string    `json:"device_id"`
	Basis           string    `json:"basis"`
	BasisValidUntil time.Time `json:"basis_valid_until"`
	WindowStart     time.Time `json:"window_start"`
	WindowEnd       time.Time `json:"window_end"`
	Owner           string    `json:"owner"`
}

type RevisePlanRequest struct {
	Basis           string    `json:"basis"`
	BasisValidUntil time.Time `json:"basis_valid_until"`
	WindowStart     time.Time `json:"window_start"`
	WindowEnd       time.Time `json:"window_end"`
	Owner           string    `json:"owner"`
}

type CreateScheduleRequest struct {
	Assignee     string    `json:"assignee"`
	ScheduledFor time.Time `json:"scheduled_for"`
}

type CompleteScheduleRequest struct {
	CaseID string `json:"case_id"`
}

type CancelScheduleRequest struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
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

type EventsResponse struct {
	Events []domain.AuditEvent `json:"events"`
}

// PlanView explains the derived plan status next to the stored plan and its
// schedule history, so stewards can see pending, scheduled, overdue, and
// completed work without interpreting raw timestamps.
type PlanView struct {
	Plan              domain.CalibrationPlan       `json:"plan"`
	Status            domain.PlanStatus            `json:"status"`
	StatusExplanation string                       `json:"status_explanation"`
	Schedules         []domain.CalibrationSchedule `json:"schedules"`
}

type PlanResponse struct {
	Plan              domain.CalibrationPlan       `json:"plan"`
	Status            domain.PlanStatus            `json:"status"`
	StatusExplanation string                       `json:"status_explanation"`
	Schedules         []domain.CalibrationSchedule `json:"schedules"`
	Events            []domain.AuditEvent          `json:"events"`
}

type PlanListResponse struct {
	Plans []PlanView `json:"plans"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
