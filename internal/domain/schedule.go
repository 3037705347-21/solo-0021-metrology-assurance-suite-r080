package domain

import (
	"fmt"
	"strings"
	"time"
)

type ScheduleState string

const (
	ScheduleArranged  ScheduleState = "arranged"
	ScheduleCompleted ScheduleState = "completed"
	ScheduleCancelled ScheduleState = "cancelled"
)

// CalibrationSchedule is one arrangement inside a plan window. The window is
// snapshot at creation so later plan revisions cannot retroactively change
// whether this arrangement was made on time.
type CalibrationSchedule struct {
	ID           string        `json:"id"`
	PlanID       string        `json:"plan_id"`
	DeviceID     string        `json:"device_id"`
	Assignee     string        `json:"assignee"`
	ScheduledFor time.Time     `json:"scheduled_for"`
	WindowStart  time.Time     `json:"window_start"`
	WindowEnd    time.Time     `json:"window_end"`
	State        ScheduleState `json:"state"`
	CaseID       string        `json:"case_id,omitempty"`
	OnTime       *bool         `json:"on_time,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	CancelledAt  *time.Time    `json:"cancelled_at,omitempty"`
}

func NewCalibrationSchedule(id, planID, deviceID, assignee string, scheduledFor, windowStart, windowEnd, at time.Time) (CalibrationSchedule, error) {
	item := CalibrationSchedule{
		ID:           strings.TrimSpace(id),
		PlanID:       strings.TrimSpace(planID),
		DeviceID:     strings.TrimSpace(deviceID),
		Assignee:     strings.TrimSpace(assignee),
		ScheduledFor: scheduledFor.UTC(),
		WindowStart:  windowStart.UTC(),
		WindowEnd:    windowEnd.UTC(),
		State:        ScheduleArranged,
		CreatedAt:    at.UTC(),
	}
	if err := item.Validate(); err != nil {
		return CalibrationSchedule{}, err
	}
	return item, nil
}

func (s CalibrationSchedule) Validate() error {
	if s.ID == "" || s.PlanID == "" || s.DeviceID == "" || s.Assignee == "" {
		return fmt.Errorf("%w: schedule id, plan id, device id, and assignee are required", ErrInvalidInput)
	}
	if s.ScheduledFor.IsZero() || s.WindowStart.IsZero() || s.WindowEnd.IsZero() || s.CreatedAt.IsZero() {
		return fmt.Errorf("%w: schedule times are required", ErrInvalidInput)
	}
	if !s.WindowStart.Before(s.WindowEnd) {
		return fmt.Errorf("%w: schedule window start must be before window end", ErrInvalidInput)
	}
	if s.ScheduledFor.Before(s.WindowStart) || s.ScheduledFor.After(s.WindowEnd) {
		return fmt.Errorf("%w: scheduled time is outside the plan window", ErrInvalidInput)
	}
	switch s.State {
	case ScheduleArranged, ScheduleCompleted, ScheduleCancelled:
		return nil
	default:
		return fmt.Errorf("%w: unsupported schedule state %q", ErrInvalidInput, s.State)
	}
}

func (s CalibrationSchedule) IsActive() bool {
	return s.State == ScheduleArranged
}

// Complete freezes the on-time fact against the snapshot window taken when
// the schedule was created, never against a later revised plan window.
func (s *CalibrationSchedule) Complete(caseID string, at time.Time) error {
	if s.State != ScheduleArranged {
		return fmt.Errorf("%w: only an arranged schedule can be completed", ErrStateConflict)
	}
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return fmt.Errorf("%w: a sealed case id is required to complete a schedule", ErrInvalidInput)
	}
	stamp := at.UTC()
	onTime := !stamp.After(s.WindowEnd)
	s.State = ScheduleCompleted
	s.CaseID = caseID
	s.OnTime = &onTime
	s.CompletedAt = &stamp
	return nil
}

func (s *CalibrationSchedule) Cancel(at time.Time) error {
	if s.State != ScheduleArranged {
		return fmt.Errorf("%w: only an arranged schedule can be cancelled", ErrStateConflict)
	}
	stamp := at.UTC()
	s.State = ScheduleCancelled
	s.CancelledAt = &stamp
	return nil
}
