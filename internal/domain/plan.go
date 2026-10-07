package domain

import (
	"fmt"
	"strings"
	"time"
)

type PlanState string

const (
	PlanPending   PlanState = "pending"
	PlanScheduled PlanState = "scheduled"
	PlanCompleted PlanState = "completed"
)

// PlanStatus is the derived, explained state of a plan. Overdue is never
// stored: it is computed from the current window so that revising a plan can
// never rewrite a historical on-time schedule into an overdue one.
type PlanStatus string

const (
	PlanStatusPending   PlanStatus = "pending"
	PlanStatusScheduled PlanStatus = "scheduled"
	PlanStatusOverdue   PlanStatus = "overdue"
	PlanStatusCompleted PlanStatus = "completed"
)

func ParsePlanStatus(value string) (PlanStatus, error) {
	switch status := PlanStatus(strings.TrimSpace(value)); status {
	case PlanStatusPending, PlanStatusScheduled, PlanStatusOverdue, PlanStatusCompleted:
		return status, nil
	default:
		return "", fmt.Errorf("%w: unsupported plan status %q", ErrInvalidInput, value)
	}
}

type CalibrationPlan struct {
	ID              string     `json:"id"`
	DeviceID        string     `json:"device_id"`
	Basis           string     `json:"basis"`
	BasisValidUntil time.Time  `json:"basis_valid_until"`
	WindowStart     time.Time  `json:"window_start"`
	WindowEnd       time.Time  `json:"window_end"`
	Owner           string     `json:"owner"`
	State           PlanState  `json:"state"`
	Version         int        `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

func NewCalibrationPlan(id, deviceID, basis string, basisValidUntil, windowStart, windowEnd time.Time, owner string, at time.Time) (CalibrationPlan, error) {
	item := CalibrationPlan{
		ID:              strings.TrimSpace(id),
		DeviceID:        strings.TrimSpace(deviceID),
		Basis:           strings.TrimSpace(basis),
		BasisValidUntil: basisValidUntil.UTC(),
		WindowStart:     windowStart.UTC(),
		WindowEnd:       windowEnd.UTC(),
		Owner:           strings.TrimSpace(owner),
		State:           PlanPending,
		Version:         1,
		CreatedAt:       at.UTC(),
		UpdatedAt:       at.UTC(),
	}
	if err := item.Validate(); err != nil {
		return CalibrationPlan{}, err
	}
	return item, nil
}

func (p CalibrationPlan) Validate() error {
	if p.ID == "" || p.DeviceID == "" || p.Basis == "" || p.Owner == "" {
		return fmt.Errorf("%w: plan id, device id, basis, and owner are required", ErrInvalidInput)
	}
	if p.WindowStart.IsZero() || p.WindowEnd.IsZero() || p.BasisValidUntil.IsZero() {
		return fmt.Errorf("%w: plan window and basis validity are required", ErrInvalidInput)
	}
	if !p.WindowStart.Before(p.WindowEnd) {
		return fmt.Errorf("%w: plan window start must be before window end", ErrInvalidInput)
	}
	if p.WindowEnd.After(p.BasisValidUntil) {
		return fmt.Errorf("%w: plan window must close on or before the calibration basis expires", ErrInvalidInput)
	}
	if p.Version < 1 || p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: plan version and timestamps are required", ErrInvalidInput)
	}
	switch p.State {
	case PlanPending, PlanScheduled, PlanCompleted:
		return nil
	default:
		return fmt.Errorf("%w: unsupported plan state %q", ErrInvalidInput, p.State)
	}
}

func (p CalibrationPlan) IsUnfinished() bool {
	return p.State != PlanCompleted
}

// Status derives the explained status at the given time. A completed plan
// stays completed forever; an unfinished plan is overdue once its window has
// closed, otherwise it reflects the stored scheduling state.
func (p CalibrationPlan) Status(at time.Time) PlanStatus {
	if p.State == PlanCompleted {
		return PlanStatusCompleted
	}
	if at.After(p.WindowEnd) {
		return PlanStatusOverdue
	}
	if p.State == PlanScheduled {
		return PlanStatusScheduled
	}
	return PlanStatusPending
}

func (p CalibrationPlan) BasisExpired(at time.Time) bool {
	return at.After(p.BasisValidUntil)
}

func (p *CalibrationPlan) MarkScheduled(at time.Time) error {
	if p.State != PlanPending {
		return fmt.Errorf("%w: plan is not pending", ErrStateConflict)
	}
	p.State = PlanScheduled
	p.Version++
	p.UpdatedAt = at.UTC()
	return nil
}

func (p *CalibrationPlan) MarkUnscheduled(at time.Time) error {
	if p.State != PlanScheduled {
		return fmt.Errorf("%w: plan is not scheduled", ErrStateConflict)
	}
	p.State = PlanPending
	p.Version++
	p.UpdatedAt = at.UTC()
	return nil
}

// Revise replaces the mutable planning fields. Completed plans are terminal.
// Existing schedules keep the window they snapshot at creation, so a revision
// never rewrites a historical on-time arrangement into an overdue one.
func (p *CalibrationPlan) Revise(basis string, basisValidUntil, windowStart, windowEnd time.Time, owner string, at time.Time) error {
	if p.State == PlanCompleted {
		return fmt.Errorf("%w: a completed plan cannot be revised", ErrStateConflict)
	}
	next := *p
	next.Basis = strings.TrimSpace(basis)
	next.BasisValidUntil = basisValidUntil.UTC()
	next.WindowStart = windowStart.UTC()
	next.WindowEnd = windowEnd.UTC()
	next.Owner = strings.TrimSpace(owner)
	if err := next.Validate(); err != nil {
		return err
	}
	*p = next
	p.Version++
	p.UpdatedAt = at.UTC()
	return nil
}

func (p *CalibrationPlan) Complete(at time.Time) error {
	if p.State == PlanCompleted {
		return fmt.Errorf("%w: plan is already completed", ErrStateConflict)
	}
	stamp := at.UTC()
	p.State = PlanCompleted
	p.Version++
	p.UpdatedAt = stamp
	p.CompletedAt = &stamp
	return nil
}
