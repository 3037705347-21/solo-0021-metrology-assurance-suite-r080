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

const (
	planSubjectType  = "plan"
	basisSubjectType = "basis"
)

func (s *Service) RegisterBasis(ctx context.Context, input contracts.RegisterBasisRequest) (domain.CalibrationBasis, error) {
	var created domain.CalibrationBasis
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		deviceID := strings.TrimSpace(input.DeviceID)
		device, exists := m.Device(deviceID)
		if !exists {
			return fmt.Errorf("%w: device %s", domain.ErrNotFound, deviceID)
		}
		if !device.IsActive() {
			return fmt.Errorf("%w: device %s is %s", domain.ErrStateConflict, device.ID, device.Status)
		}
		certificate := domain.NormalizeCertificateNumber(input.CertificateNumber)
		if _, exists := m.BasisByCertificate(device.ID, certificate); exists {
			return fmt.Errorf("%w: %s", domain.ErrDuplicateCertificate, certificate)
		}
		item, err := domain.NewCalibrationBasis(
			m.NextID("basis"),
			device.ID,
			input.CertificateNumber,
			input.Issuer,
			input.EffectiveAt,
			input.ExpiresAt,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := m.PutBasis(item); err != nil {
			return err
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			basisSubjectType,
			item.ID,
			"",
			"calibration_basis_registered",
			device.Steward,
			fmt.Sprintf("calibration basis %s registered for device %s", item.CertificateNumber, device.ID),
			s.now(),
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		created = item
		return nil
	})
	return created, err
}

func (s *Service) DeviceBases(ctx context.Context, deviceID string) ([]domain.CalibrationBasis, error) {
	var result []domain.CalibrationBasis
	err := s.repository.Read(ctx, func(snapshot repository.Snapshot) error {
		id := strings.TrimSpace(deviceID)
		device, exists := snapshot.Device(id)
		if !exists {
			return fmt.Errorf("%w: device %s", domain.ErrNotFound, id)
		}
		result = snapshot.BasesForDevice(device.ID)
		return nil
	})
	return result, err
}

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
		if open, exists := m.OpenPlanForDevice(device.ID); exists {
			return fmt.Errorf("%w: plan %s is still %s", domain.ErrActivePlanExists, open.ID, open.State)
		}
		basis, err := s.validDeviceBasis(m, device.ID, input.BasisID)
		if err != nil {
			return err
		}
		item, err := domain.NewCalibrationPlan(
			m.NextID("plan"),
			device.ID,
			basis.ID,
			input.WindowStartsAt,
			input.WindowDueAt,
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
			planSubjectType,
			item.ID,
			"",
			"calibration_plan_created",
			item.Owner,
			fmt.Sprintf("recalibration window for device %s closes at %s", device.ID, item.WindowDueAt.Format(time.RFC3339)),
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

func (s *Service) ListPlans(ctx context.Context, statusFilter string) (contracts.PlansResponse, error) {
	var views []contracts.PlanView
	filter := strings.TrimSpace(statusFilter)
	err := s.repository.Read(ctx, func(snapshot repository.Snapshot) error {
		now := s.now()
		for _, item := range snapshot.AllPlans() {
			view := planView(item, now)
			if filter != "" && view.Status != filter {
				continue
			}
			views = append(views, view)
		}
		sort.SliceStable(views, func(i, j int) bool {
			if views[i].Status == "overdue" && views[j].Status != "overdue" {
				return true
			}
			if views[i].Status != "overdue" && views[j].Status == "overdue" {
				return false
			}
			if views[i].WindowDueAt.Equal(views[j].WindowDueAt) {
				return views[i].ID < views[j].ID
			}
			return views[i].WindowDueAt.Before(views[j].WindowDueAt)
		})
		return nil
	})
	if err != nil {
		return contracts.PlansResponse{}, err
	}
	if views == nil {
		views = []contracts.PlanView{}
	}
	return contracts.PlansResponse{Plans: views}, nil
}

func (s *Service) ArrangePlan(ctx context.Context, planID string, input contracts.ArrangePlanRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		id := strings.TrimSpace(planID)
		item, exists := m.Plan(id)
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, id)
		}
		device, exists := m.Device(item.DeviceID)
		if !exists {
			return fmt.Errorf("%w: device %s", domain.ErrNotFound, item.DeviceID)
		}
		if !device.IsActive() {
			return fmt.Errorf("%w: device %s is %s", domain.ErrStateConflict, device.ID, device.Status)
		}
		if active, exists := m.ActiveCaseForDevice(device.ID); exists {
			return fmt.Errorf("%w: case %s is still %s", domain.ErrActiveCaseExists, active.ID, active.State)
		}
		basis, err := s.validDeviceBasis(m, device.ID, input.BasisID)
		if err != nil {
			return err
		}
		if caseID := strings.TrimSpace(input.CaseID); caseID != "" {
			linked, exists := m.Case(caseID)
			if !exists {
				return fmt.Errorf("%w: case %s", domain.ErrNotFound, caseID)
			}
			if linked.DeviceID != device.ID {
				return fmt.Errorf("%w: case %s belongs to another device", domain.ErrInvalidInput, caseID)
			}
		}
		now := s.now()
		if err := item.Arrange(input.ScheduledFor, input.Assignee, input.CaseID, basis, now); err != nil {
			return err
		}
		if err := m.UpdatePlan(item); err != nil {
			return err
		}
		decision := "within the plan window"
		if !item.Arrangement.OnTime {
			decision = "after the plan window closed"
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			planSubjectType,
			item.ID,
			"",
			"calibration_plan_arranged",
			item.Arrangement.Assignee,
			fmt.Sprintf("recalibration arranged for %s, booked %s", item.Arrangement.ScheduledFor.Format(time.RFC3339), decision),
			now,
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, item, now)
		return nil
	})
	return result, err
}

func (s *Service) CompletePlan(ctx context.Context, planID string, input contracts.CompletePlanRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		id := strings.TrimSpace(planID)
		item, exists := m.Plan(id)
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, id)
		}
		device, exists := m.Device(item.DeviceID)
		if !exists {
			return fmt.Errorf("%w: device %s", domain.ErrNotFound, item.DeviceID)
		}
		if !device.IsActive() {
			return fmt.Errorf("%w: device %s is %s", domain.ErrStateConflict, device.ID, device.Status)
		}
		caseID := strings.TrimSpace(input.CaseID)
		recalibrationCase, exists := m.Case(caseID)
		if !exists {
			return fmt.Errorf("%w: case %s", domain.ErrNotFound, caseID)
		}
		if recalibrationCase.DeviceID != device.ID {
			return fmt.Errorf("%w: case %s belongs to another device", domain.ErrInvalidInput, caseID)
		}
		if recalibrationCase.State != domain.CaseSealed {
			return fmt.Errorf("%w: case %s is %s, not sealed", domain.ErrStateConflict, caseID, recalibrationCase.State)
		}
		basis, err := s.validDeviceBasis(m, device.ID, input.BasisID)
		if err != nil {
			return err
		}
		now := s.now()
		if err := item.Complete(recalibrationCase.ID, basis, now); err != nil {
			return err
		}
		if err := m.UpdatePlan(item); err != nil {
			return err
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			planSubjectType,
			item.ID,
			recalibrationCase.ID,
			"calibration_plan_completed",
			device.Steward,
			fmt.Sprintf("recalibration completed with sealed case %s and basis %s", recalibrationCase.ID, basis.CertificateNumber),
			now,
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, item, now)
		return nil
	})
	return result, err
}

func (s *Service) ReschedulePlan(ctx context.Context, planID string, input contracts.ReschedulePlanRequest) (contracts.PlanResponse, error) {
	var result contracts.PlanResponse
	err := s.repository.Mutate(ctx, func(m *repository.Mutation) error {
		id := strings.TrimSpace(planID)
		item, exists := m.Plan(id)
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, id)
		}
		now := s.now()
		previousDue := item.WindowDueAt
		if err := item.Reschedule(input.WindowStartsAt, input.WindowDueAt, input.Owner, now); err != nil {
			return err
		}
		if err := m.UpdatePlan(item); err != nil {
			return err
		}
		event, err := domain.NewAuditEvent(
			m.NextID("event"),
			planSubjectType,
			item.ID,
			"",
			"calibration_plan_rescheduled",
			item.Owner,
			fmt.Sprintf("plan window moved from closing %s to %s; existing arrangement timeliness is unchanged", previousDue.Format(time.RFC3339), item.WindowDueAt.Format(time.RFC3339)),
			now,
		)
		if err != nil {
			return err
		}
		m.AppendEvent(event)
		result = planResponse(m, item, now)
		return nil
	})
	return result, err
}

func (s *Service) PlanEvents(ctx context.Context, planID string) ([]domain.AuditEvent, error) {
	var events []domain.AuditEvent
	err := s.repository.Read(ctx, func(snapshot repository.Snapshot) error {
		id := strings.TrimSpace(planID)
		item, exists := snapshot.Plan(id)
		if !exists {
			return fmt.Errorf("%w: plan %s", domain.ErrNotFound, id)
		}
		events = snapshot.SubjectEvents(planSubjectType, item.ID)
		return nil
	})
	return events, err
}

// validDeviceBasis loads a basis and proves it is real, owned by the device,
// and a currently usable calibration authority.
func (s *Service) validDeviceBasis(m *repository.Mutation, deviceID, basisID string) (domain.CalibrationBasis, error) {
	id := strings.TrimSpace(basisID)
	basis, exists := m.Basis(id)
	if !exists {
		return domain.CalibrationBasis{}, fmt.Errorf("%w: basis %s", domain.ErrBasisInvalid, id)
	}
	if basis.DeviceID != deviceID {
		return domain.CalibrationBasis{}, fmt.Errorf("%w: basis %s belongs to another device", domain.ErrBasisInvalid, basis.ID)
	}
	if !basis.ValidAt(s.now()) {
		return domain.CalibrationBasis{}, fmt.Errorf("%w: basis %s expired at %s", domain.ErrBasisInvalid, basis.ID, basis.ExpiresAt.Format(time.RFC3339))
	}
	return basis, nil
}

func planView(item domain.CalibrationPlan, now time.Time) contracts.PlanView {
	explained := item.ExplainedStatus(now)
	return contracts.PlanView{
		CalibrationPlan: item,
		Status:          explained.Status,
		StatusReason:    explained.Reason,
	}
}

func planResponse(m *repository.Mutation, item domain.CalibrationPlan, now time.Time) contracts.PlanResponse {
	return contracts.PlanResponse{
		Plan:   planView(item, now),
		Events: m.SubjectEvents(planSubjectType, item.ID),
	}
}

func planResponseFromSnapshot(snapshot repository.Snapshot, item domain.CalibrationPlan, now time.Time) contracts.PlanResponse {
	return contracts.PlanResponse{
		Plan:   planView(item, now),
		Events: snapshot.SubjectEvents(planSubjectType, item.ID),
	}
}
