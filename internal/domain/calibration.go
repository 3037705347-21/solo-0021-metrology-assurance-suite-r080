package domain

import (
	"fmt"
	"strings"
	"time"
)

// CalibrationBasis is the real, traceable calibration authority that a plan
// and its arrangement rely on (for example a calibration certificate). A basis
// is only usable while the current time is inside its validity window.
type CalibrationBasis struct {
	ID                string    `json:"id"`
	DeviceID          string    `json:"device_id"`
	CertificateNumber string    `json:"certificate_number"`
	Issuer            string    `json:"issuer"`
	EffectiveAt       time.Time `json:"effective_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	CreatedAt         time.Time `json:"created_at"`
}

func NormalizeCertificateNumber(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func NewCalibrationBasis(id, deviceID, certificateNumber, issuer string, effectiveAt, expiresAt, at time.Time) (CalibrationBasis, error) {
	item := CalibrationBasis{
		ID:                strings.TrimSpace(id),
		DeviceID:          strings.TrimSpace(deviceID),
		CertificateNumber: NormalizeCertificateNumber(certificateNumber),
		Issuer:            strings.TrimSpace(issuer),
		EffectiveAt:       effectiveAt.UTC(),
		ExpiresAt:         expiresAt.UTC(),
		CreatedAt:         at.UTC(),
	}
	if err := item.Validate(); err != nil {
		return CalibrationBasis{}, err
	}
	return item, nil
}

func (b CalibrationBasis) Validate() error {
	if b.ID == "" || b.DeviceID == "" || b.CertificateNumber == "" || b.Issuer == "" {
		return fmt.Errorf("%w: basis id, device id, certificate number, and issuer are required", ErrInvalidInput)
	}
	if b.EffectiveAt.IsZero() || b.ExpiresAt.IsZero() || b.CreatedAt.IsZero() {
		return fmt.Errorf("%w: basis validity timestamps are required", ErrInvalidInput)
	}
	if !b.EffectiveAt.Before(b.ExpiresAt) {
		return fmt.Errorf("%w: basis must become effective before it expires", ErrInvalidInput)
	}
	return nil
}

// ValidAt reports whether the basis is a usable calibration authority at the
// given time. The expiry instant itself is treated as expired.
func (b CalibrationBasis) ValidAt(at time.Time) bool {
	stamp := at.UTC()
	return !stamp.Before(b.EffectiveAt) && stamp.Before(b.ExpiresAt)
}

type PlanState string

const (
	PlanPending   PlanState = "pending"
	PlanScheduled PlanState = "scheduled"
	PlanCompleted PlanState = "completed"
)

// PlanArrangement is an immutable snapshot of a booking. Its timeliness is
// frozen against the plan window that existed at booking time so later plan
// changes cannot rewrite a historically on-time booking into a late one.
type PlanArrangement struct {
	ArrangedAt   time.Time `json:"arranged_at"`
	ScheduledFor time.Time `json:"scheduled_for"`
	Assignee     string    `json:"assignee"`
	BasisID      string    `json:"basis_id"`
	BasisDueAt   time.Time `json:"basis_due_at"`
	CaseID       string    `json:"case_id,omitempty"`
	OnTime       bool      `json:"on_time"`
}

// PlanCompletion records the sealed recalibration case and the renewed basis
// that close the plan.
type PlanCompletion struct {
	CompletedAt time.Time `json:"completed_at"`
	CaseID      string    `json:"case_id"`
	BasisID     string    `json:"basis_id"`
	BasisDueAt  time.Time `json:"basis_due_at"`
}

// CalibrationPlan is a per-device recalibration window with a responsible
// owner. It moves from pending to scheduled to completed; a pending plan whose
// window has closed is explained as overdue without changing its state.
type CalibrationPlan struct {
	ID             string           `json:"id"`
	DeviceID       string           `json:"device_id"`
	BasisID        string           `json:"basis_id"`
	WindowStartsAt time.Time        `json:"window_starts_at"`
	WindowDueAt    time.Time        `json:"window_due_at"`
	Owner          string           `json:"owner"`
	State          PlanState        `json:"state"`
	Version        int              `json:"version"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	Arrangement    *PlanArrangement `json:"arrangement,omitempty"`
	Completion     *PlanCompletion  `json:"completion,omitempty"`
}

func NewCalibrationPlan(id, deviceID, basisID string, windowStartsAt, windowDueAt time.Time, owner string, at time.Time) (CalibrationPlan, error) {
	item := CalibrationPlan{
		ID:             strings.TrimSpace(id),
		DeviceID:       strings.TrimSpace(deviceID),
		BasisID:        strings.TrimSpace(basisID),
		WindowStartsAt: windowStartsAt.UTC(),
		WindowDueAt:    windowDueAt.UTC(),
		Owner:          strings.TrimSpace(owner),
		State:          PlanPending,
		Version:        1,
		CreatedAt:      at.UTC(),
		UpdatedAt:      at.UTC(),
	}
	if err := item.Validate(); err != nil {
		return CalibrationPlan{}, err
	}
	return item, nil
}

func (p CalibrationPlan) Validate() error {
	if p.ID == "" || p.DeviceID == "" || p.BasisID == "" || p.Owner == "" {
		return fmt.Errorf("%w: plan id, device id, basis id, and owner are required", ErrInvalidInput)
	}
	if p.WindowStartsAt.IsZero() || p.WindowDueAt.IsZero() || p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: plan window timestamps are required", ErrInvalidInput)
	}
	if p.WindowStartsAt.After(p.WindowDueAt) {
		return fmt.Errorf("%w: plan window must start before it closes", ErrInvalidInput)
	}
	if p.Version < 1 {
		return fmt.Errorf("%w: plan version is required", ErrInvalidInput)
	}
	switch p.State {
	case PlanPending, PlanScheduled, PlanCompleted:
		return nil
	default:
		return fmt.Errorf("%w: unsupported plan state %q", ErrInvalidInput, p.State)
	}
}

func (p CalibrationPlan) IsOpen() bool {
	return p.State != PlanCompleted
}

// Reschedule changes the window or owner. An existing arrangement is left
// untouched so its frozen on-time decision survives the new window.
func (p *CalibrationPlan) Reschedule(windowStartsAt, windowDueAt time.Time, owner string, at time.Time) error {
	if p.State == PlanCompleted {
		return fmt.Errorf("%w: a completed plan cannot be rescheduled", ErrStateConflict)
	}
	start := windowStartsAt.UTC()
	due := windowDueAt.UTC()
	nextOwner := strings.TrimSpace(owner)
	if start.IsZero() || due.IsZero() || nextOwner == "" {
		return fmt.Errorf("%w: plan window and owner are required", ErrInvalidInput)
	}
	if start.After(due) {
		return fmt.Errorf("%w: plan window must start before it closes", ErrInvalidInput)
	}
	p.WindowStartsAt = start
	p.WindowDueAt = due
	p.Owner = nextOwner
	p.Version++
	p.UpdatedAt = at.UTC()
	return nil
}

// Arrange books the plan against a real, currently valid basis. Timeliness is
// decided once, against the window in effect at booking time, and stored on the
// arrangement snapshot.
func (p *CalibrationPlan) Arrange(scheduledFor time.Time, assignee, caseID string, basis CalibrationBasis, at time.Time) error {
	if p.State != PlanPending || p.Arrangement != nil {
		return fmt.Errorf("%w: only a pending plan can be arranged", ErrStateConflict)
	}
	booked := scheduledFor.UTC()
	nextAssignee := strings.TrimSpace(assignee)
	if booked.IsZero() || nextAssignee == "" {
		return fmt.Errorf("%w: scheduled time and assignee are required", ErrInvalidInput)
	}
	if strings.TrimSpace(basis.ID) == "" || strings.TrimSpace(basis.DeviceID) != p.DeviceID {
		return fmt.Errorf("%w: arrangement requires the device calibration basis", ErrBasisInvalid)
	}
	stamp := at.UTC()
	p.Arrangement = &PlanArrangement{
		ArrangedAt:   stamp,
		ScheduledFor: booked,
		Assignee:     nextAssignee,
		BasisID:      basis.ID,
		BasisDueAt:   basis.ExpiresAt,
		CaseID:       strings.TrimSpace(caseID),
		OnTime:       !stamp.After(p.WindowDueAt),
	}
	p.State = PlanScheduled
	p.Version++
	p.UpdatedAt = stamp
	return nil
}

// Complete closes a booked plan. The recalibration case must already be sealed
// and the renewed basis must be real and currently valid; both facts are
// enforced by the service before calling this transition.
func (p *CalibrationPlan) Complete(caseID string, basis CalibrationBasis, at time.Time) error {
	if p.State != PlanScheduled || p.Arrangement == nil {
		return fmt.Errorf("%w: only an arranged plan can be completed", ErrStateConflict)
	}
	if p.Completion != nil {
		return fmt.Errorf("%w: plan %s is already completed", ErrStateConflict, p.ID)
	}
	sealedCase := strings.TrimSpace(caseID)
	if sealedCase == "" {
		return fmt.Errorf("%w: a sealed recalibration case is required to complete a plan", ErrInvalidInput)
	}
	if strings.TrimSpace(basis.ID) == "" || strings.TrimSpace(basis.DeviceID) != p.DeviceID {
		return fmt.Errorf("%w: completion requires the device calibration basis", ErrBasisInvalid)
	}
	stamp := at.UTC()
	p.Completion = &PlanCompletion{
		CompletedAt: stamp,
		CaseID:      sealedCase,
		BasisID:     basis.ID,
		BasisDueAt:  basis.ExpiresAt,
	}
	p.State = PlanCompleted
	p.Version++
	p.UpdatedAt = stamp
	return nil
}

// ExplainedStatus is the user-facing status derived from the stored plan state
// and the current time. Overdue is an explanation of a still-pending plan, not
// a stored state, so it cannot be rewritten by later plan changes.
type ExplainedStatus struct {
	Status string
	Reason string
}

func (p CalibrationPlan) ExplainedStatus(at time.Time) ExplainedStatus {
	switch p.State {
	case PlanCompleted:
		return ExplainedStatus{
			Status: string(PlanCompleted),
			Reason: "recalibration was completed against a sealed case and a valid basis",
		}
	case PlanScheduled:
		if p.Arrangement != nil && p.Arrangement.OnTime {
			return ExplainedStatus{
				Status: string(PlanScheduled),
				Reason: "arrangement was booked within the plan window",
			}
		}
		return ExplainedStatus{
			Status: string(PlanScheduled),
			Reason: "arrangement was booked after the plan window had closed",
		}
	default:
		if at.UTC().After(p.WindowDueAt) {
			return ExplainedStatus{
				Status: "overdue",
				Reason: "the plan window closed before an arrangement was booked",
			}
		}
		return ExplainedStatus{
			Status: string(PlanPending),
			Reason: "waiting for an arrangement within the plan window",
		}
	}
}
