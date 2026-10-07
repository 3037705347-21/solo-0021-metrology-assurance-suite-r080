package assurance

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/solo-0021-metrology-assurance-suite/internal/contracts"
	"example.com/solo-0021-metrology-assurance-suite/internal/domain"
	"example.com/solo-0021-metrology-assurance-suite/internal/repository"
)

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTestService(t *testing.T, start time.Time) (*Service, *clock) {
	t.Helper()
	clk := &clock{at: start}
	service := NewService(repository.New())
	service.now = clk.now
	return service, clk
}

func mustRegisterDevice(t *testing.T, service *Service, ctx context.Context, tag string) string {
	t.Helper()
	device, err := service.RegisterDevice(ctx, contracts.RegisterDeviceRequest{
		AssetTag: tag,
		Model:    "Bench Meter",
		Room:     "Room A",
		Steward:  "Lin",
	})
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	return device.ID
}

func mustRegisterBasis(t *testing.T, service *Service, ctx context.Context, deviceID, certificate string, effective, expires time.Time) string {
	t.Helper()
	basis, err := service.RegisterBasis(ctx, contracts.RegisterBasisRequest{
		DeviceID:          deviceID,
		CertificateNumber: certificate,
		Issuer:            "Lab",
		EffectiveAt:       effective,
		ExpiresAt:         expires,
	})
	if err != nil {
		t.Fatalf("register basis: %v", err)
	}
	return basis.ID
}

func sealCase(t *testing.T, service *Service, ctx context.Context, deviceID string, observed float64, technician string) string {
	t.Helper()
	opened, err := service.OpenCase(ctx, contracts.OpenCaseRequest{
		DeviceID:  deviceID,
		Nominal:   25,
		Tolerance: 0.5,
		Steward:   "Qiao",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	if _, err := service.RecordMeasurement(ctx, opened.Case.ID, contracts.RecordMeasurementRequest{
		Observed:   observed,
		Technician: technician,
	}); err != nil {
		t.Fatalf("record measurement: %v", err)
	}
	sealed, err := service.SealCase(ctx, opened.Case.ID, contracts.SealCaseRequest{
		Reviewer: "Verifier",
		Summary:  "sealed",
	})
	if err != nil {
		t.Fatalf("seal case: %v", err)
	}
	return sealed.Case.ID
}

// TestArrangeGuards verifies that no invalid arrangement can be formed while a
// device has an unfinished case or while the cited basis has expired.
func TestArrangeGuards(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	service, clk := newTestService(t, start)

	deviceID := mustRegisterDevice(t, service, ctx, "MET-3001")
	basisID := mustRegisterBasis(t, service, ctx, deviceID, "CERT-3001",
		start.AddDate(0, -1, 0), start.AddDate(0, 0, 30))
	expiredBasisID := mustRegisterBasis(t, service, ctx, deviceID, "CERT-OLD",
		start.AddDate(0, -2, 0), start.AddDate(0, 0, -1))

	plan, err := service.CreatePlan(ctx, contracts.CreatePlanRequest{
		DeviceID:       deviceID,
		BasisID:        basisID,
		WindowStartsAt: start,
		WindowDueAt:    start.AddDate(0, 0, 10),
		Owner:          "Lin",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	planID := plan.Plan.ID

	// Expired basis cannot back the arrangement.
	if _, err := service.ArrangePlan(ctx, planID, contracts.ArrangePlanRequest{
		ScheduledFor: start.AddDate(0, 0, 5),
		Assignee:     "Rui",
		BasisID:      expiredBasisID,
	}); !errors.Is(err, domain.ErrBasisInvalid) {
		t.Fatalf("arrange with expired basis: err = %v, want ErrBasisInvalid", err)
	}

	// An unfinished case blocks the arrangement.
	opened, err := service.OpenCase(ctx, contracts.OpenCaseRequest{
		DeviceID: deviceID, Nominal: 25, Tolerance: 0.5, Steward: "Qiao",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	if _, err := service.ArrangePlan(ctx, planID, contracts.ArrangePlanRequest{
		ScheduledFor: start.AddDate(0, 0, 5),
		Assignee:     "Rui",
		BasisID:      basisID,
	}); !errors.Is(err, domain.ErrActiveCaseExists) {
		t.Fatalf("arrange with open case: err = %v, want ErrActiveCaseExists", err)
	}

	// After sealing the case, the arrangement succeeds even though time has
	// advanced; the booking is still inside the plan window.
	clk.advance(2 * 24 * time.Hour)
	if _, err := service.RecordMeasurement(ctx, opened.Case.ID, contracts.RecordMeasurementRequest{
		Observed: 25.2, Technician: "Rui",
	}); err != nil {
		t.Fatalf("record measurement: %v", err)
	}
	if _, err := service.SealCase(ctx, opened.Case.ID, contracts.SealCaseRequest{
		Reviewer: "Verifier", Summary: "sealed",
	}); err != nil {
		t.Fatalf("seal: %v", err)
	}
	arranged, err := service.ArrangePlan(ctx, planID, contracts.ArrangePlanRequest{
		ScheduledFor: start.AddDate(0, 0, 5),
		Assignee:     "Rui",
		BasisID:      basisID,
	})
	if err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if arranged.Plan.Status != "scheduled" || arranged.Plan.Arrangement == nil || !arranged.Plan.Arrangement.OnTime {
		t.Fatalf("arranged plan view = %+v", arranged.Plan)
	}
}

// TestPlanChangesDoNotRewriteHistory drives the full lifecycle through the
// service and proves a frozen on-time booking survives a later window change,
// while completion needs a sealed case and a currently valid basis.
func TestPlanChangesDoNotRewriteHistory(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	service, clk := newTestService(t, start)

	deviceID := mustRegisterDevice(t, service, ctx, "MET-4001")
	basisID := mustRegisterBasis(t, service, ctx, deviceID, "CERT-4001",
		start.AddDate(-1, 0, 0), start.AddDate(1, 0, 0))

	created, err := service.CreatePlan(ctx, contracts.CreatePlanRequest{
		DeviceID:       deviceID,
		BasisID:        basisID,
		WindowStartsAt: start,
		WindowDueAt:    start.AddDate(0, 0, 10),
		Owner:          "Lin",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	planID := created.Plan.ID

	arranged, err := service.ArrangePlan(ctx, planID, contracts.ArrangePlanRequest{
		ScheduledFor: start.AddDate(0, 0, 5),
		Assignee:     "Rui",
		BasisID:      basisID,
	})
	if err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if !arranged.Plan.Arrangement.OnTime {
		t.Fatal("booking inside the window was not recorded as on time")
	}

	// Shrink the window so the booking would be late under the new window.
	shrunk, err := service.ReschedulePlan(ctx, planID, contracts.ReschedulePlanRequest{
		WindowStartsAt: start,
		WindowDueAt:    start.AddDate(0, 0, 2),
		Owner:          "Mira",
	})
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if !shrunk.Plan.Arrangement.OnTime {
		t.Fatal("plan change rewrote the historical on-time arrangement")
	}

	// Completion rejects an unsealed case.
	opened, err := service.OpenCase(ctx, contracts.OpenCaseRequest{
		DeviceID: deviceID, Nominal: 25, Tolerance: 0.5, Steward: "Qiao",
	})
	if err != nil {
		t.Fatalf("open recalibration case: %v", err)
	}
	if _, err := service.CompletePlan(ctx, planID, contracts.CompletePlanRequest{
		CaseID: opened.Case.ID, BasisID: basisID,
	}); !errors.Is(err, domain.ErrStateConflict) {
		t.Fatalf("complete with unsealed case: err = %v, want ErrStateConflict", err)
	}

	clk.advance(24 * time.Hour)
	if _, err := service.RecordMeasurement(ctx, opened.Case.ID, contracts.RecordMeasurementRequest{
		Observed: 25.1, Technician: "Rui",
	}); err != nil {
		t.Fatalf("record measurement: %v", err)
	}
	if _, err := service.SealCase(ctx, opened.Case.ID, contracts.SealCaseRequest{
		Reviewer: "Verifier", Summary: "recalibrated",
	}); err != nil {
		t.Fatalf("seal recalibration case: %v", err)
	}

	completed, err := service.CompletePlan(ctx, planID, contracts.CompletePlanRequest{
		CaseID: opened.Case.ID, BasisID: basisID,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Plan.Status != "completed" || completed.Plan.Completion == nil {
		t.Fatalf("completed view = %+v", completed.Plan)
	}

	// A second open plan is now allowed because the previous one is complete.
	if _, err := service.CreatePlan(ctx, contracts.CreatePlanRequest{
		DeviceID:       deviceID,
		BasisID:        basisID,
		WindowStartsAt: clk.now(),
		WindowDueAt:    clk.now().AddDate(0, 0, 30),
		Owner:          "Lin",
	}); err != nil {
		t.Fatalf("create follow-up plan: %v", err)
	}
}

// TestBasisExpiryBlocksCompletionButNotFrozenArrangement proves an arrangement
// stays on time after its basis later expires, while completion requires the
// renewed, currently valid basis.
func TestBasisExpiryBlocksCompletionButNotFrozenArrangement(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	service, clk := newTestService(t, start)

	deviceID := mustRegisterDevice(t, service, ctx, "MET-5001")
	basisID := mustRegisterBasis(t, service, ctx, deviceID, "CERT-5001",
		start.AddDate(0, 0, -10), start.AddDate(0, 0, 5))

	created, err := service.CreatePlan(ctx, contracts.CreatePlanRequest{
		DeviceID:       deviceID,
		BasisID:        basisID,
		WindowStartsAt: start,
		WindowDueAt:    start.AddDate(0, 0, 20),
		Owner:          "Lin",
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	arranged, err := service.ArrangePlan(ctx, created.Plan.ID, contracts.ArrangePlanRequest{
		ScheduledFor: start.AddDate(0, 0, 3),
		Assignee:     "Rui",
		BasisID:      basisID,
	})
	if err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if !arranged.Plan.Arrangement.OnTime {
		t.Fatal("on-time arrangement was not frozen")
	}

	// The basis expires and recalibration completes afterwards.
	clk.advance(6 * 24 * time.Hour)
	recalCaseID := sealCase(t, service, ctx, deviceID, 25.1, "Rui")
	if _, err := service.CompletePlan(ctx, created.Plan.ID, contracts.CompletePlanRequest{
		CaseID: recalCaseID, BasisID: basisID,
	}); !errors.Is(err, domain.ErrBasisInvalid) {
		t.Fatalf("complete on expired basis: err = %v, want ErrBasisInvalid", err)
	}

	renewedID := mustRegisterBasis(t, service, ctx, deviceID, "CERT-5001-R",
		clk.now().AddDate(0, 0, -1), clk.now().AddDate(1, 0, 0))
	completed, err := service.CompletePlan(ctx, created.Plan.ID, contracts.CompletePlanRequest{
		CaseID: recalCaseID, BasisID: renewedID,
	})
	if err != nil {
		t.Fatalf("complete with renewed basis: %v", err)
	}
	if completed.Plan.Status != "completed" {
		t.Fatalf("status = %q, want completed", completed.Plan.Status)
	}

	// The frozen arrangement survived all later changes.
	if completed.Plan.Arrangement == nil || !completed.Plan.Arrangement.OnTime {
		t.Fatalf("frozen arrangement lost: %+v", completed.Plan.Arrangement)
	}
}

func TestListPlansDerivedStatusFilter(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	service, clk := newTestService(t, start)

	deviceID := mustRegisterDevice(t, service, ctx, "MET-6001")
	basisID := mustRegisterBasis(t, service, ctx, deviceID, "CERT-6001",
		start.AddDate(-1, 0, 0), start.AddDate(1, 0, 0))
	pending, err := service.CreatePlan(ctx, contracts.CreatePlanRequest{
		DeviceID:       deviceID,
		BasisID:        basisID,
		WindowStartsAt: start,
		WindowDueAt:    start.AddDate(0, 0, 10),
		Owner:          "Lin",
	})
	if err != nil {
		t.Fatalf("create pending plan: %v", err)
	}

	otherID := mustRegisterDevice(t, service, ctx, "MET-6002")
	otherBasisID := mustRegisterBasis(t, service, ctx, otherID, "CERT-6002",
		start.AddDate(-1, 0, 0), start.AddDate(1, 0, 0))
	if _, err := service.CreatePlan(ctx, contracts.CreatePlanRequest{
		DeviceID:       otherID,
		BasisID:        otherBasisID,
		WindowStartsAt: start.AddDate(0, 0, -20),
		WindowDueAt:    start.AddDate(0, 0, -10),
		Owner:          "Lin",
	}); err != nil {
		t.Fatalf("create overdue plan: %v", err)
	}

	overdue, err := service.ListPlans(ctx, "overdue")
	if err != nil {
		t.Fatalf("list overdue: %v", err)
	}
	if len(overdue.Plans) != 1 {
		t.Fatalf("overdue plans = %d, want 1", len(overdue.Plans))
	}
	if overdue.Plans[0].State != domain.PlanPending {
		t.Fatalf("overdue plan stored state = %q, want pending", overdue.Plans[0].State)
	}

	all, err := service.ListPlans(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all.Plans) != 2 || all.Plans[0].Status != "overdue" {
		t.Fatalf("all plans should sort overdue first: %+v", all.Plans)
	}

	// Advancing the clock makes the first plan overdue without any mutation.
	clk.advance(11 * 24 * time.Hour)
	overdueAgain, err := service.ListPlans(ctx, "overdue")
	if err != nil {
		t.Fatalf("list overdue after clock advance: %v", err)
	}
	if len(overdueAgain.Plans) != 2 {
		t.Fatalf("overdue plans = %d, want 2", len(overdueAgain.Plans))
	}
	still, err := service.Plan(ctx, pending.Plan.ID)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if still.Plan.State != domain.PlanPending || still.Plan.Status != "overdue" {
		t.Fatalf("stored = %q derived = %q, want pending/overdue", still.Plan.State, still.Plan.Status)
	}
}
