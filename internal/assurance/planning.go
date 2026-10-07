package assurance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"example.com/solo-0021-metrology-assurance-suite/internal/contracts"
	"example.com/solo-0021-metrology-assurance-suite/internal/domain"
	"example.com/solo-0021-metrology-assurance-suite/internal/repository"
)

func (s *Service) CreatePlan(ctx context.Context, input contracts.CreatePlanRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		deviceID := strings.TrimSpace(input.DeviceID)
		device, exists := m.Device(deviceID)
		if !exists {
			return fmt.Errorf("%w: device %s", domain.ErrNotFound, deviceID)
		}
		if !device.IsActive() {
			return fmt.Errorf("%w: device %s is %s", domain.ErrStateConflict, device.ID, device.Status)
		}
		if active, exists := m.UnfinishedPlanForDevice(device.ID); exists {
			return fmt.Errorf("%w: plan %s is still %s", domain.ErrActivePlanExists, active.ID, active.State)
		}
		item, err := domain.NewCalibrationPlan(
			m.NextID("plan"),
			device.ID,
			input.Basis,
			input.BasisValidUntil,
			input.WindowStart,
			input.WindowEnd,
			input.Owner,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := m.PutPlan(item); err != nil {
			return err
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			"plan",
			item.ID,
			"",
			"plan_created",
			item.Owner,
			fmt.Sprintf("calibration plan opened for device %s with window %s to %s", item.DeviceID, formatTime(item.WindowStart), formatTime(item.WindowEnd)),
			s.now(),
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, item, s.now())
		return nil
	})
	return result, err
}

func (s *Service) Plans(ctx context.Context, deviceID, statusFilter string) (contracts.PlanListResponse, error) {
	var result contracts.PlanListResponse
	deviceID = strings.TrimSpace(deviceID)
	statusFilter = strings.TrimSpace(statusFilter)
	var wanted domain.PlanStatus
	if statusFilter != "" {
		parsed, err := domain.ParsePlanStatus(statusFilter)
		if err != nil {
			return result, err
		}
		wanted = parsed
	}
	err := s.repository.Read(ctx, func(snapshot repository.Snapshot) error {
		now := s.now()
		views := make([]contracts.PlanView, 0)
		for _, plan := range snapshot.AllPlans() {
			if deviceID != "" && plan.DeviceID != deviceID {
				continue
			}
			view := planView(snapshot.SchedulesForPlan(plan.ID), plan, now)
			if statusFilter != "" && view.Status != wanted {
				continue
			}
			views = append(views, view)
		}
		sort.Slice(views, func(i, j int) bool {
			if views[i].Plan.WindowEnd.Equal(views[j].Plan.WindowEnd) {
				return views[i].Plan.ID < views[j].Plan.ID
			}
			return views[i].Plan.WindowEnd.Before(views[j].Plan.WindowEnd)
		})
		result = contracts.PlanListResponse{Plans: views}
		return nil
	})
	return result, err
}

func (s *Service) Plan(ctx context.Context, id string) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Read(ctx, func(snapshot repository.Snapshot) error {
		item, exists := snapshot.Plan(strings.TrimSpace(id))
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, id)
		}
		result = planResponseFromSnapshot(snapshot, item, s.now())
		return nil
	})
	return result, err
}

func (s *Service) RevisePlan(ctx context.Context, id string, input contracts.RevisePlanRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		item, exists := m.Plan(strings.TrimSpace(id))
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, id)
		}
		if err := item.Revise(input.Basis, input.BasisValidUntil, input.WindowStart, input.WindowEnd, input.Owner, s.now()); err != nil {
			return err
		}
		if err := m.UpdatePlan(item); err != nil {
			return err
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			"plan",
			item.ID,
			"",
			"plan_revised",
			item.Owner,
			fmt.Sprintf("plan revised: window %s to %s, basis %s valid until %s", formatTime(item.WindowStart), formatTime(item.WindowEnd), item.Basis, formatTime(item.BasisValidUntil)),
			s.now(),
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, item, s.now())
		return nil
	})
	return result, err
}

func (s *Service) CreateSchedule(ctx context.Context, planID string, input contracts.CreateScheduleRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		plan, exists := m.Plan(strings.TrimSpace(planID))
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, planID)
		}
		if !plan.IsUnfinished() {
			return fmt.Errorf("%w: plan %s is completed", domain.ErrStateConflict, plan.ID)
		}
		if active, exists := m.ActiveScheduleForPlan(plan.ID); exists {
			return fmt.Errorf("%w: schedule %s is still %s", domain.ErrActiveScheduleExists, active.ID, active.State)
		}
		device, exists := m.Device(plan.DeviceID)
		if !exists {
			return fmt.Errorf("%w: device %s", domain.ErrNotFound, plan.DeviceID)
		}
		if !device.IsActive() {
			return fmt.Errorf("%w: device %s is %s", domain.ErrStateConflict, device.ID, device.Status)
		}
		if active, exists := m.ActiveCaseForDevice(device.ID); exists {
			return fmt.Errorf("%w: case %s is still %s", domain.ErrActiveCaseExists, active.ID, active.State)
		}
		now := s.now()
		if plan.BasisExpired(now) {
			return fmt.Errorf("%w: basis %s expired at %s", domain.ErrBasisExpired, plan.Basis, formatTime(plan.BasisValidUntil))
		}
		if input.ScheduledFor.Before(now) {
			return fmt.Errorf("%w: scheduled time must not be in the past", domain.ErrInvalidInput)
		}
		schedule, err := domain.NewCalibrationSchedule(
			m.NextID("schedule"),
			plan.ID,
			plan.DeviceID,
			input.Assignee,
			input.ScheduledFor,
			plan.WindowStart,
			plan.WindowEnd,
			now,
		)
		if err != nil {
			return err
		}
		if err := plan.MarkScheduled(now); err != nil {
			return err
		}
		if err := m.PutSchedule(schedule); err != nil {
			return err
		}
		if err := m.UpdatePlan(plan); err != nil {
			return err
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			"plan",
			plan.ID,
			"",
			"schedule_created",
			schedule.Assignee,
			fmt.Sprintf("schedule %s arranged for %s", schedule.ID, formatTime(schedule.ScheduledFor)),
			now,
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, plan, now)
		return nil
	})
	return result, err
}

func (s *Service) CompleteSchedule(ctx context.Context, scheduleID string, input contracts.CompleteScheduleRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		schedule, exists := m.Schedule(strings.TrimSpace(scheduleID))
		if !exists {
			return fmt.Errorf("%w: schedule %s", domain.ErrNotFound, scheduleID)
		}
		if !schedule.IsActive() {
			return fmt.Errorf("%w: schedule %s is %s", domain.ErrStateConflict, schedule.ID, schedule.State)
		}
		plan, exists := m.Plan(schedule.PlanID)
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, schedule.PlanID)
		}
		device, exists := m.Device(plan.DeviceID)
		if !exists {
			return fmt.Errorf("%w: device %s", domain.ErrNotFound, plan.DeviceID)
		}
		if !device.IsActive() {
			return fmt.Errorf("%w: device %s is %s", domain.ErrStateConflict, device.ID, device.Status)
		}
		now := s.now()
		if plan.BasisExpired(now) {
			return fmt.Errorf("%w: basis %s expired at %s", domain.ErrBasisExpired, plan.Basis, formatTime(plan.BasisValidUntil))
		}
		caseID := strings.TrimSpace(input.CaseID)
		item, exists := m.Case(caseID)
		if !exists {
			return fmt.Errorf("%w: case %s", domain.ErrNotFound, caseID)
		}
		if item.DeviceID != plan.DeviceID {
			return fmt.Errorf("%w: case %s belongs to device %s, not %s", domain.ErrInvalidInput, item.ID, item.DeviceID, plan.DeviceID)
		}
		if item.State != domain.CaseSealed {
			return fmt.Errorf("%w: case %s is %s, not sealed", domain.ErrStateConflict, item.ID, item.State)
		}
		if err := schedule.Complete(item.ID, now); err != nil {
			return err
		}
		if err := plan.Complete(now); err != nil {
			return err
		}
		if err := m.UpdateSchedule(schedule); err != nil {
			return err
		}
		if err := m.UpdatePlan(plan); err != nil {
			return err
		}
		detail := fmt.Sprintf("schedule %s completed on time with sealed case %s", schedule.ID, item.ID)
		if !*schedule.OnTime {
			detail = fmt.Sprintf("schedule %s completed after the plan window with sealed case %s", schedule.ID, item.ID)
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			"plan",
			plan.ID,
			item.ID,
			"schedule_completed",
			schedule.Assignee,
			detail,
			now,
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, plan, now)
		return nil
	})
	return result, err
}

func (s *Service) CancelSchedule(ctx context.Context, scheduleID string, input contracts.CancelScheduleRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		schedule, exists := m.Schedule(strings.TrimSpace(scheduleID))
		if !exists {
			return fmt.Errorf("%w: schedule %s", domain.ErrNotFound, scheduleID)
		}
		plan, exists := m.Plan(schedule.PlanID)
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, schedule.PlanID)
		}
		actor := strings.TrimSpace(input.Actor)
		reason := strings.TrimSpace(input.Reason)
		if actor == "" || reason == "" {
			return fmt.Errorf("%w: actor and reason are required", domain.ErrInvalidInput)
		}
		now := s.now()
		if err := schedule.Cancel(now); err != nil {
			return err
		}
		if err := plan.MarkUnscheduled(now); err != nil {
			return err
		}
		if err := m.UpdateSchedule(schedule); err != nil {
			return err
		}
		if err := m.UpdatePlan(plan); err != nil {
			return err
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			"plan",
			plan.ID,
			"",
			"schedule_cancelled",
			actor,
			fmt.Sprintf("schedule %s cancelled: %s", schedule.ID, reason),
			now,
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, plan, now)
		return nil
	})
	return result, err
}

func planResponse(m *repository.Mutation, plan domain.CalibrationPlan, now time.Time) contracts.PlanResponse {
	schedules := sortedSchedules(m.SchedulesForPlan(plan.ID))
	return contracts.PlanResponse{
		Plan:              plan,
		Status:            plan.Status(now),
		StatusExplanation: explainPlanStatus(plan, schedules, now),
		Schedules:         schedules,
		Events:            m.SubjectEvents("plan", plan.ID),
	}
}

func planResponseFromSnapshot(snapshot repository.Snapshot, plan domain.CalibrationPlan, now time.Time) contracts.PlanResponse {
	schedules := sortedSchedules(snapshot.SchedulesForPlan(plan.ID))
	return contracts.PlanResponse{
		Plan:              plan,
		Status:            plan.Status(now),
		StatusExplanation: explainPlanStatus(plan, schedules, now),
		Schedules:         schedules,
		Events:            snapshot.SubjectEvents("plan", plan.ID),
	}
}

func planView(schedules []domain.CalibrationSchedule, plan domain.CalibrationPlan, now time.Time) contracts.PlanView {
	ordered := sortedSchedules(schedules)
	return contracts.PlanView{
		Plan:              plan,
		Status:            plan.Status(now),
		StatusExplanation: explainPlanStatus(plan, ordered, now),
		Schedules:         ordered,
	}
}

func sortedSchedules(items []domain.CalibrationSchedule) []domain.CalibrationSchedule {
	result := append([]domain.CalibrationSchedule(nil), items...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func activeSchedule(items []domain.CalibrationSchedule) *domain.CalibrationSchedule {
	for index := range items {
		if items[index].IsActive() {
			return &items[index]
		}
	}
	return nil
}

func completedSchedule(items []domain.CalibrationSchedule) *domain.CalibrationSchedule {
	for index := range items {
		if items[index].State == domain.ScheduleCompleted {
			return &items[index]
		}
	}
	return nil
}

// explainPlanStatus renders the derived status in plain language so stewards
// can read pending, scheduled, overdue, and completed plans directly.
func explainPlanStatus(plan domain.CalibrationPlan, schedules []domain.CalibrationSchedule, now time.Time) string {
	switch plan.Status(now) {
	case domain.PlanStatusCompleted:
		if schedule := completedSchedule(schedules); schedule != nil {
			timing := "on time"
			if schedule.OnTime != nil && !*schedule.OnTime {
				timing = "after the plan window"
			}
			return fmt.Sprintf("completed %s by schedule %s with sealed case %s (%s)", formatTime(*schedule.CompletedAt), schedule.ID, schedule.CaseID, timing)
		}
		return fmt.Sprintf("completed at %s", formatTime(*plan.CompletedAt))
	case domain.PlanStatusOverdue:
		if schedule := activeSchedule(schedules); schedule != nil {
			return fmt.Sprintf("plan window closed at %s without completion; schedule %s for %s with %s is still outstanding", formatTime(plan.WindowEnd), schedule.ID, formatTime(schedule.ScheduledFor), schedule.Assignee)
		}
		return fmt.Sprintf("plan window closed at %s and no schedule is outstanding", formatTime(plan.WindowEnd))
	case domain.PlanStatusScheduled:
		if schedule := activeSchedule(schedules); schedule != nil {
			return fmt.Sprintf("scheduled for %s with %s; plan window closes at %s", formatTime(schedule.ScheduledFor), schedule.Assignee, formatTime(plan.WindowEnd))
		}
		return fmt.Sprintf("plan window closes at %s", formatTime(plan.WindowEnd))
	default:
		return fmt.Sprintf("no schedule exists; plan window closes at %s", formatTime(plan.WindowEnd))
	}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}
