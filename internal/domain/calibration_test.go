package domain

import (
	"testing"
	"time"
)

func fixedClock(value string) time.Time {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return at
}

func testBasis(id, deviceID string, effective, expires time.Time) CalibrationBasis {
	basis, err := NewCalibrationBasis(id, deviceID, "CERT-"+id, "Lab", effective, expires, effective)
	if err != nil {
		panic(err)
	}
	return basis
}

func testPlan(deviceID, basisID string, starts, due, at time.Time) CalibrationPlan {
	plan, err := NewCalibrationPlan("plan-1", deviceID, basisID, starts, due, "Lin", at)
	if err != nil {
		panic(err)
	}
	return plan
}

func TestArrangementTimelinessIsFrozenAgainstLaterWindowChanges(t *testing.T) {
	now := fixedClock("2026-10-07T09:00:00Z")
	windowDue := fixedClock("2026-10-10T09:00:00Z")
	plan := testPlan("device-1", "basis-1", now, windowDue, now)
	basis := testBasis("basis-1", "device-1",
		fixedClock("2026-01-01T00:00:00Z"),
		fixedClock("2027-01-01T00:00:00Z"))

	if err := plan.Arrange(fixedClock("2026-10-09T09:00:00Z"), "Rui", "", basis, now); err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if plan.State != PlanScheduled {
		t.Fatalf("state = %q, want scheduled", plan.State)
	}
	if !plan.Arrangement.OnTime {
		t.Fatal("arrangement inside the window was not frozen as on time")
	}

	// Moving the window so the booking now looks late must not rewrite history.
	laterDue := fixedClock("2026-10-08T09:00:00Z")
	if err := plan.Reschedule(now, laterDue, "Mira", now); err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if !plan.Arrangement.OnTime {
		t.Fatal("rescheduling the window retroactively rewrote the on-time decision")
	}
	if plan.State != PlanScheduled {
		t.Fatalf("rescheduling changed state to %q", plan.State)
	}

	// Moving the window later likewise cannot turn a late booking on time.
	latePlan := testPlan("device-2", "basis-2",
		fixedClock("2026-10-01T09:00:00Z"),
		fixedClock("2026-10-05T09:00:00Z"),
		fixedClock("2026-10-01T09:00:00Z"))
	lateBasis := testBasis("basis-2", "device-2",
		fixedClock("2026-01-01T00:00:00Z"),
		fixedClock("2027-01-01T00:00:00Z"))
	bookAt := fixedClock("2026-10-06T09:00:00Z")
	if err := latePlan.Arrange(fixedClock("2026-10-08T09:00:00Z"), "Rui", "", lateBasis, bookAt); err != nil {
		t.Fatalf("late arrange: %v", err)
	}
	if latePlan.Arrangement.OnTime {
		t.Fatal("arrangement after the window closed was frozen as on time")
	}
	if got := latePlan.ExplainedStatus(bookAt).Status; got != "scheduled" {
		t.Fatalf("status = %q, want scheduled", got)
	}
	if err := latePlan.Reschedule(
		fixedClock("2026-10-01T09:00:00Z"),
		fixedClock("2026-12-01T09:00:00Z"),
		"Lin", bookAt); err != nil {
		t.Fatalf("reschedule late plan: %v", err)
	}
	if latePlan.Arrangement.OnTime {
		t.Fatal("extending the window retroactively made a late booking on time")
	}
}

func TestOverdueIsDerivedWithoutChangingStoredState(t *testing.T) {
	now := fixedClock("2026-10-07T09:00:00Z")
	plan := testPlan("device-1", "basis-1",
		fixedClock("2026-09-01T09:00:00Z"),
		fixedClock("2026-10-01T09:00:00Z"),
		fixedClock("2026-09-01T09:00:00Z"))

	explained := plan.ExplainedStatus(now)
	if explained.Status != "overdue" {
		t.Fatalf("status = %q, want overdue", explained.Status)
	}
	if plan.State != PlanPending {
		t.Fatalf("derived overdue rewrote stored state to %q", plan.State)
	}

	upcoming := testPlan("device-2", "basis-2", now, fixedClock("2026-10-20T09:00:00Z"), now)
	if got := upcoming.ExplainedStatus(now).Status; got != "pending" {
		t.Fatalf("status = %q, want pending", got)
	}
}

func TestBasisValidityWindow(t *testing.T) {
	basis := testBasis("basis-1", "device-1",
		fixedClock("2026-01-01T00:00:00Z"),
		fixedClock("2026-06-01T00:00:00Z"))
	cases := []struct {
		at   time.Time
		want bool
	}{
		{fixedClock("2025-12-31T23:59:59Z"), false},
		{fixedClock("2026-01-01T00:00:00Z"), true},
		{fixedClock("2026-05-31T23:59:59Z"), true},
		{fixedClock("2026-06-01T00:00:00Z"), false},
	}
	for _, tc := range cases {
		if got := basis.ValidAt(tc.at); got != tc.want {
			t.Errorf("ValidAt(%s) = %v, want %v", tc.at.Format(time.RFC3339), got, tc.want)
		}
	}
}

func TestArrangeRequiresPendingPlanAndDeviceBasis(t *testing.T) {
	now := fixedClock("2026-10-07T09:00:00Z")
	due := fixedClock("2026-10-20T09:00:00Z")
	basis := testBasis("basis-1", "device-1",
		fixedClock("2026-01-01T00:00:00Z"),
		fixedClock("2027-01-01T00:00:00Z"))
	foreign := testBasis("basis-2", "device-OTHER",
		fixedClock("2026-01-01T00:00:00Z"),
		fixedClock("2027-01-01T00:00:00Z"))

	plan := testPlan("device-1", "basis-1", now, due, now)
	if err := plan.Arrange(now, "Rui", "", foreign, now); err == nil {
		t.Fatal("arrangement accepted a basis owned by another device")
	}
	if err := plan.Arrange(now, "Rui", "", basis, now); err != nil {
		t.Fatalf("arrange: %v", err)
	}
	if err := plan.Arrange(now, "Rui", "", basis, now); err == nil {
		t.Fatal("a second arrangement was accepted")
	}

	// Completion requires a sealed case and a device basis, enforced by Complete.
	if err := plan.Complete("", basis, now); err == nil {
		t.Fatal("completion accepted an empty case")
	}
	if err := plan.Complete("case-1", foreign, now); err == nil {
		t.Fatal("completion accepted a basis owned by another device")
	}
	if err := plan.Complete("case-1", basis, now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if plan.State != PlanCompleted {
		t.Fatalf("state = %q, want completed", plan.State)
	}
	if err := plan.Reschedule(now, due, "Lin", now); err == nil {
		t.Fatal("a completed plan was rescheduled")
	}
	if err := plan.Complete("case-2", basis, now); err == nil {
		t.Fatal("a completed plan was completed again")
	}
}
