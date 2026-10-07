package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"example.com/solo-0021-metrology-assurance-suite/internal/bootstrap"
)

type apiResponse struct {
	Status int
	Body   map[string]any
	Raw    string
}

type harness struct {
	baseURL string
	client  *http.Client
	server  *httptest.Server
}

func main() {
	workflow := flag.String("workflow", "", "workflow id to verify")
	flag.Parse()

	checks := map[string]func() error{
		"register-device":          checkRegisterDevice,
		"open-verification-case":   checkOpenVerificationCase,
		"record-measurement":       checkRecordMeasurement,
		"reopen-verification-case": checkReopenVerificationCase,
		"seal-verification":        checkSealVerification,
		"calibration-planning":     checkCalibrationPlanning,
	}
	run, ok := checks[*workflow]
	if !ok {
		fmt.Fprintf(flag.CommandLine.Output(), "unknown workflow %q\n", *workflow)
		flag.Usage()
		os.Exit(2)
	}
	if err := run(); err != nil {
		fmt.Printf("workflow %s: failed: %v\n", *workflow, err)
		os.Exit(1)
	}
	fmt.Printf("workflow %s: ok\n", *workflow)
}

func checkRegisterDevice() error {
	h := newHarness()
	defer h.close()

	response, err := h.request(http.MethodPost, "/devices", map[string]any{
		"asset_tag": " met-1001 ",
		"model":     "Bench Meter",
		"room":      "Room A",
		"steward":   "Lin",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(response, http.StatusCreated); err != nil {
		return err
	}
	id, err := nestedString(response.Body, "device", "id")
	if err != nil {
		return err
	}
	tag, err := nestedString(response.Body, "device", "asset_tag")
	if err != nil {
		return err
	}
	if tag != "MET-1001" {
		return fmt.Errorf("asset tag was not normalized: %q", tag)
	}

	read, err := h.request(http.MethodGet, "/devices/"+id, nil)
	if err != nil {
		return err
	}
	if err := expectStatus(read, http.StatusOK); err != nil {
		return err
	}
	readID, err := nestedString(read.Body, "device", "id")
	if err != nil {
		return err
	}
	if readID != id {
		return fmt.Errorf("read returned device %q, expected %q", readID, id)
	}

	duplicate, err := h.request(http.MethodPost, "/devices", map[string]any{
		"asset_tag": "MET-1001",
		"model":     "Other Meter",
		"room":      "Room B",
		"steward":   "Mira",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(duplicate, http.StatusConflict); err != nil {
		return err
	}
	return expectErrorCode(duplicate, "duplicate_asset_tag")
}

func checkOpenVerificationCase() error {
	h := newHarness()
	defer h.close()

	deviceID, err := h.createDevice("MET-1101")
	if err != nil {
		return err
	}
	opened, err := h.openCase(deviceID)
	if err != nil {
		return err
	}
	caseID, err := nestedString(opened.Body, "case", "id")
	if err != nil {
		return err
	}
	state, err := nestedString(opened.Body, "case", "state")
	if err != nil {
		return err
	}
	if state != "awaiting_measurement" {
		return fmt.Errorf("opened case state = %q", state)
	}

	read, err := h.request(http.MethodGet, "/cases/"+caseID, nil)
	if err != nil {
		return err
	}
	if err := expectStatus(read, http.StatusOK); err != nil {
		return err
	}

	second, err := h.request(http.MethodPost, "/cases", map[string]any{
		"device_id": deviceID,
		"nominal":   25.0,
		"tolerance": 0.5,
		"steward":   "Qiao",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(second, http.StatusConflict); err != nil {
		return err
	}
	return expectErrorCode(second, "active_case_exists")
}

func checkRecordMeasurement() error {
	h := newHarness()
	defer h.close()

	deviceID, err := h.createDevice("MET-1201")
	if err != nil {
		return err
	}
	opened, err := h.openCase(deviceID)
	if err != nil {
		return err
	}
	caseID, err := nestedString(opened.Body, "case", "id")
	if err != nil {
		return err
	}
	accepted, err := h.recordMeasurement(caseID, 25.2, "Rui")
	if err != nil {
		return err
	}
	if err := expectStatus(accepted, http.StatusCreated); err != nil {
		return err
	}
	state, err := nestedString(accepted.Body, "case", "state")
	if err != nil {
		return err
	}
	if state != "review_ready" {
		return fmt.Errorf("accepted measurement state = %q", state)
	}
	acceptedValue, err := nestedBool(accepted.Body, "measurements", 0, "accepted")
	if err != nil {
		return err
	}
	if !acceptedValue {
		return errors.New("in-tolerance measurement was not accepted")
	}

	second, err := h.recordMeasurement(caseID, 25.1, "Rui")
	if err != nil {
		return err
	}
	if err := expectStatus(second, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(second, "state_conflict"); err != nil {
		return err
	}

	rejectDeviceID, err := h.createDevice("MET-1202")
	if err != nil {
		return err
	}
	rejectCase, err := h.openCase(rejectDeviceID)
	if err != nil {
		return err
	}
	rejectCaseID, err := nestedString(rejectCase.Body, "case", "id")
	if err != nil {
		return err
	}
	rejected, err := h.recordMeasurement(rejectCaseID, 26.5, "Rui")
	if err != nil {
		return err
	}
	if err := expectStatus(rejected, http.StatusCreated); err != nil {
		return err
	}
	rejectedState, err := nestedString(rejected.Body, "case", "state")
	if err != nil {
		return err
	}
	if rejectedState != "recheck_required" {
		return fmt.Errorf("out-of-tolerance state = %q", rejectedState)
	}
	rejectedValue, err := nestedBool(rejected.Body, "measurements", 0, "accepted")
	if err != nil {
		return err
	}
	if rejectedValue {
		return errors.New("out-of-tolerance measurement was accepted")
	}
	return nil
}

func checkReopenVerificationCase() error {
	h := newHarness()
	defer h.close()

	caseID, err := h.rejectedCase("MET-1301")
	if err != nil {
		return err
	}
	reopened, err := h.request(http.MethodPost, "/cases/"+caseID+"/reopen", map[string]any{
		"actor":  "Lin",
		"reason": "measurement setup was replaced",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(reopened, http.StatusOK); err != nil {
		return err
	}
	state, err := nestedString(reopened.Body, "case", "state")
	if err != nil {
		return err
	}
	if state != "awaiting_measurement" {
		return fmt.Errorf("reopened case state = %q", state)
	}
	measurements, err := nestedSlice(reopened.Body, "measurements")
	if err != nil {
		return err
	}
	if len(measurements) != 1 {
		return fmt.Errorf("reopen changed measurement history: %d entries", len(measurements))
	}

	second, err := h.request(http.MethodPost, "/cases/"+caseID+"/reopen", map[string]any{
		"actor":  "Lin",
		"reason": "duplicate request",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(second, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(second, "state_conflict"); err != nil {
		return err
	}

	events, err := h.request(http.MethodGet, "/cases/"+caseID+"/events", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(events, http.StatusOK); err != nil {
		return err
	}
	if !hasEvent(events.Body, "case_reopened") {
		return errors.New("reopen audit event is missing")
	}
	return nil
}

func checkSealVerification() error {
	h := newHarness()
	defer h.close()

	caseID, err := h.readyCase("MET-1401")
	if err != nil {
		return err
	}
	type outcome struct {
		response apiResponse
		err      error
	}
	results := make(chan outcome, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			response, err := h.request(http.MethodPost, "/cases/"+caseID+"/seal", map[string]any{
				"reviewer": fmt.Sprintf("Reviewer-%d", index+1),
				"summary":  "accepted measurement sealed for review",
			})
			results <- outcome{response: response, err: err}
		}(index)
	}
	wait.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for item := range results {
		if item.err != nil {
			return item.err
		}
		switch item.response.Status {
		case http.StatusCreated:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			return fmt.Errorf("unexpected concurrent seal status %d: %s", item.response.Status, item.response.Raw)
		}
	}
	if successes != 1 || conflicts != 1 {
		return fmt.Errorf("concurrent seal outcomes: %d success, %d conflict", successes, conflicts)
	}

	read, err := h.request(http.MethodGet, "/cases/"+caseID, nil)
	if err != nil {
		return err
	}
	if err := expectStatus(read, http.StatusOK); err != nil {
		return err
	}
	state, err := nestedString(read.Body, "case", "state")
	if err != nil {
		return err
	}
	if state != "sealed" {
		return fmt.Errorf("sealed case state = %q", state)
	}
	sealID, err := nestedString(read.Body, "seal", "id")
	if err != nil {
		return err
	}
	if sealID == "" {
		return errors.New("evidence seal is missing")
	}
	return nil
}

func checkCalibrationPlanning() error {
	h := newHarness()
	defer h.close()
	now := time.Now().UTC()

	deviceID, err := h.createDevice("MET-1501")
	if err != nil {
		return err
	}
	opened, err := h.openCase(deviceID)
	if err != nil {
		return err
	}
	caseID, err := nestedString(opened.Body, "case", "id")
	if err != nil {
		return err
	}
	accepted, err := h.recordMeasurement(caseID, 25.2, "Rui")
	if err != nil {
		return err
	}
	if err := expectStatus(accepted, http.StatusCreated); err != nil {
		return err
	}

	plan, err := h.createPlan(deviceID, now.Add(1*time.Hour), now.Add(24*time.Hour), now.Add(48*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(plan, http.StatusCreated); err != nil {
		return err
	}
	if err := expectPlanStatus(plan, "pending"); err != nil {
		return err
	}
	explanation, err := nestedString(plan.Body, "status_explanation")
	if err != nil {
		return err
	}
	if explanation == "" {
		return errors.New("pending plan has no status explanation")
	}
	planID, err := nestedString(plan.Body, "plan", "id")
	if err != nil {
		return err
	}

	duplicate, err := h.createPlan(deviceID, now.Add(1*time.Hour), now.Add(24*time.Hour), now.Add(48*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(duplicate, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(duplicate, "active_plan_exists"); err != nil {
		return err
	}

	blocked, err := h.createSchedule(planID, now.Add(12*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(blocked, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(blocked, "active_case_exists"); err != nil {
		return err
	}

	if err := h.sealCase(caseID); err != nil {
		return err
	}
	scheduled, err := h.createSchedule(planID, now.Add(12*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(scheduled, http.StatusCreated); err != nil {
		return err
	}
	if err := expectPlanStatus(scheduled, "scheduled"); err != nil {
		return err
	}
	scheduleID, err := nestedStringAt(scheduled.Body, "schedules", 0, "id")
	if err != nil {
		return err
	}

	second, err := h.createSchedule(planID, now.Add(13*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(second, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(second, "active_schedule_exists"); err != nil {
		return err
	}

	otherCaseID, err := h.sealedCase("MET-1599")
	if err != nil {
		return err
	}
	mismatch, err := h.completeSchedule(scheduleID, otherCaseID)
	if err != nil {
		return err
	}
	if err := expectStatus(mismatch, http.StatusBadRequest); err != nil {
		return err
	}
	if err := expectErrorCode(mismatch, "invalid_input"); err != nil {
		return err
	}

	completed, err := h.completeSchedule(scheduleID, caseID)
	if err != nil {
		return err
	}
	if err := expectStatus(completed, http.StatusOK); err != nil {
		return err
	}
	if err := expectPlanStatus(completed, "completed"); err != nil {
		return err
	}
	onTime, err := nestedBool(completed.Body, "schedules", 0, "on_time")
	if err != nil {
		return err
	}
	if !onTime {
		return errors.New("schedule completed inside the window was not on time")
	}

	revised, err := h.revisePlan(planID, now.Add(2*time.Hour), now.Add(20*time.Hour), now.Add(48*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(revised, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(revised, "state_conflict"); err != nil {
		return err
	}

	// A plan revision must not rewrite the snapshot window of an existing
	// schedule: the historical on-time arrangement stays on time.
	plan2, err := h.createPlan(deviceID, now.Add(1*time.Hour), now.Add(48*time.Hour), now.Add(72*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(plan2, http.StatusCreated); err != nil {
		return err
	}
	plan2ID, err := nestedString(plan2.Body, "plan", "id")
	if err != nil {
		return err
	}
	scheduled2, err := h.createSchedule(plan2ID, now.Add(24*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(scheduled2, http.StatusCreated); err != nil {
		return err
	}
	snapshotEnd, err := nestedStringAt(scheduled2.Body, "schedules", 0, "window_end")
	if err != nil {
		return err
	}
	revised2, err := h.revisePlan(plan2ID, now.Add(1*time.Hour), now.Add(12*time.Hour), now.Add(72*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(revised2, http.StatusOK); err != nil {
		return err
	}
	reread, err := h.request(http.MethodGet, "/plans/"+plan2ID, nil)
	if err != nil {
		return err
	}
	if err := expectStatus(reread, http.StatusOK); err != nil {
		return err
	}
	keptEnd, err := nestedStringAt(reread.Body, "schedules", 0, "window_end")
	if err != nil {
		return err
	}
	if keptEnd != snapshotEnd {
		return fmt.Errorf("plan revision rewrote schedule window: %q became %q", snapshotEnd, keptEnd)
	}
	if err := expectPlanStatus(reread, "scheduled"); err != nil {
		return err
	}

	// A plan whose window has closed without completion is explained as
	// overdue, and a lapsed basis blocks any new schedule.
	device2ID, err := h.createDevice("MET-1502")
	if err != nil {
		return err
	}
	badWindow, err := h.createPlan(device2ID, now.Add(24*time.Hour), now.Add(1*time.Hour), now.Add(48*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(badWindow, http.StatusBadRequest); err != nil {
		return err
	}
	if err := expectErrorCode(badWindow, "invalid_input"); err != nil {
		return err
	}
	beyondBasis, err := h.createPlan(device2ID, now.Add(1*time.Hour), now.Add(48*time.Hour), now.Add(24*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(beyondBasis, http.StatusBadRequest); err != nil {
		return err
	}
	if err := expectErrorCode(beyondBasis, "invalid_input"); err != nil {
		return err
	}

	overdue, err := h.createPlan(device2ID, now.Add(-48*time.Hour), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(overdue, http.StatusCreated); err != nil {
		return err
	}
	if err := expectPlanStatus(overdue, "overdue"); err != nil {
		return err
	}
	overduePlanID, err := nestedString(overdue.Body, "plan", "id")
	if err != nil {
		return err
	}
	lapsed, err := h.createSchedule(overduePlanID, now.Add(1*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(lapsed, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(lapsed, "basis_expired"); err != nil {
		return err
	}

	overdueList, err := h.request(http.MethodGet, "/plans?status=overdue", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(overdueList, http.StatusOK); err != nil {
		return err
	}
	overduePlans, err := nestedSlice(overdueList.Body, "plans")
	if err != nil {
		return err
	}
	if len(overduePlans) != 1 {
		return fmt.Errorf("overdue list has %d plans, expected 1", len(overduePlans))
	}
	devicePlans, err := h.request(http.MethodGet, "/plans?device_id="+deviceID, nil)
	if err != nil {
		return err
	}
	if err := expectStatus(devicePlans, http.StatusOK); err != nil {
		return err
	}
	devicePlanList, err := nestedSlice(devicePlans.Body, "plans")
	if err != nil {
		return err
	}
	if len(devicePlanList) != 2 {
		return fmt.Errorf("device plan list has %d plans, expected 2", len(devicePlanList))
	}
	badFilter, err := h.request(http.MethodGet, "/plans?status=bogus", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(badFilter, http.StatusBadRequest); err != nil {
		return err
	}

	// Revising the overdue plan with a valid basis and window makes it
	// schedulable again; cancellation returns it to pending.
	revised3, err := h.revisePlan(overduePlanID, now.Add(1*time.Hour), now.Add(24*time.Hour), now.Add(48*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(revised3, http.StatusOK); err != nil {
		return err
	}
	if err := expectPlanStatus(revised3, "pending"); err != nil {
		return err
	}
	scheduled3, err := h.createSchedule(overduePlanID, now.Add(12*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(scheduled3, http.StatusCreated); err != nil {
		return err
	}
	schedule3ID, err := nestedStringAt(scheduled3.Body, "schedules", 0, "id")
	if err != nil {
		return err
	}
	cancelled, err := h.request(http.MethodPost, "/schedules/"+schedule3ID+"/cancel", map[string]any{
		"actor":  "Lin",
		"reason": "assignee unavailable in this window",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(cancelled, http.StatusOK); err != nil {
		return err
	}
	if err := expectPlanStatus(cancelled, "pending"); err != nil {
		return err
	}
	scheduled4, err := h.createSchedule(overduePlanID, now.Add(12*time.Hour))
	if err != nil {
		return err
	}
	if err := expectStatus(scheduled4, http.StatusCreated); err != nil {
		return err
	}
	schedule4ID, err := nestedStringAt(scheduled4.Body, "schedules", 1, "id")
	if err != nil {
		return err
	}
	opened2, err := h.openCase(device2ID)
	if err != nil {
		return err
	}
	case2ID, err := nestedString(opened2.Body, "case", "id")
	if err != nil {
		return err
	}
	accepted2, err := h.recordMeasurement(case2ID, 25.1, "Rui")
	if err != nil {
		return err
	}
	if err := expectStatus(accepted2, http.StatusCreated); err != nil {
		return err
	}
	if err := h.sealCase(case2ID); err != nil {
		return err
	}
	completed2, err := h.completeSchedule(schedule4ID, case2ID)
	if err != nil {
		return err
	}
	if err := expectStatus(completed2, http.StatusOK); err != nil {
		return err
	}
	if err := expectPlanStatus(completed2, "completed"); err != nil {
		return err
	}

	events, err := h.request(http.MethodGet, "/plans/"+overduePlanID+"/events", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(events, http.StatusOK); err != nil {
		return err
	}
	for _, kind := range []string{"plan_created", "plan_revised", "schedule_created", "schedule_cancelled", "schedule_completed"} {
		if !hasEvent(events.Body, kind) {
			return fmt.Errorf("plan audit event %q is missing", kind)
		}
	}
	return nil
}

func newHarness() *harness {
	server := httptest.NewServer(bootstrap.NewHandler())
	return &harness{
		baseURL: server.URL,
		client:  server.Client(),
		server:  server,
	}
}

func (h *harness) close() {
	h.server.Close()
}

func (h *harness) request(method, path string, body any) (apiResponse, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return apiResponse{}, err
		}
		reader = bytes.NewReader(encoded)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, h.baseURL+path, reader)
	if err != nil {
		return apiResponse{}, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := h.client.Do(request)
	if err != nil {
		return apiResponse{}, err
	}
	defer response.Body.Close()
	rawBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return apiResponse{}, err
	}
	result := apiResponse{Status: response.StatusCode, Raw: string(rawBytes)}
	if len(bytes.TrimSpace(rawBytes)) > 0 {
		if err := json.Unmarshal(rawBytes, &result.Body); err != nil {
			return apiResponse{}, fmt.Errorf("decode %s %s response: %w", method, path, err)
		}
	}
	return result, nil
}

func (h *harness) createDevice(tag string) (string, error) {
	response, err := h.request(http.MethodPost, "/devices", map[string]any{
		"asset_tag": tag,
		"model":     "Bench Meter",
		"room":      "Room A",
		"steward":   "Lin",
	})
	if err != nil {
		return "", err
	}
	if err := expectStatus(response, http.StatusCreated); err != nil {
		return "", err
	}
	return nestedString(response.Body, "device", "id")
}

func (h *harness) openCase(deviceID string) (apiResponse, error) {
	response, err := h.request(http.MethodPost, "/cases", map[string]any{
		"device_id": deviceID,
		"nominal":   25.0,
		"tolerance": 0.5,
		"steward":   "Qiao",
	})
	if err != nil {
		return apiResponse{}, err
	}
	if err := expectStatus(response, http.StatusCreated); err != nil {
		return apiResponse{}, err
	}
	return response, nil
}

func (h *harness) recordMeasurement(caseID string, observed float64, technician string) (apiResponse, error) {
	return h.request(http.MethodPost, "/cases/"+caseID+"/measurements", map[string]any{
		"observed":   observed,
		"technician": technician,
	})
}

func (h *harness) sealCase(caseID string) error {
	response, err := h.request(http.MethodPost, "/cases/"+caseID+"/seal", map[string]any{
		"reviewer": "Wei",
		"summary":  "accepted measurement sealed for review",
	})
	if err != nil {
		return err
	}
	return expectStatus(response, http.StatusCreated)
}

func (h *harness) sealedCase(tag string) (string, error) {
	caseID, err := h.readyCase(tag)
	if err != nil {
		return "", err
	}
	if err := h.sealCase(caseID); err != nil {
		return "", err
	}
	return caseID, nil
}

func (h *harness) createPlan(deviceID string, windowStart, windowEnd, basisUntil time.Time) (apiResponse, error) {
	return h.request(http.MethodPost, "/plans", map[string]any{
		"device_id":         deviceID,
		"basis":             "JJG 123-2020",
		"basis_valid_until": basisUntil.Format(time.RFC3339),
		"window_start":      windowStart.Format(time.RFC3339),
		"window_end":        windowEnd.Format(time.RFC3339),
		"owner":             "Lin",
	})
}

func (h *harness) revisePlan(planID string, windowStart, windowEnd, basisUntil time.Time) (apiResponse, error) {
	return h.request(http.MethodPost, "/plans/"+planID+"/revise", map[string]any{
		"basis":             "JJG 123-2020",
		"basis_valid_until": basisUntil.Format(time.RFC3339),
		"window_start":      windowStart.Format(time.RFC3339),
		"window_end":        windowEnd.Format(time.RFC3339),
		"owner":             "Lin",
	})
}

func (h *harness) createSchedule(planID string, scheduledFor time.Time) (apiResponse, error) {
	return h.request(http.MethodPost, "/plans/"+planID+"/schedules", map[string]any{
		"assignee":      "Rui",
		"scheduled_for": scheduledFor.Format(time.RFC3339),
	})
}

func (h *harness) completeSchedule(scheduleID, caseID string) (apiResponse, error) {
	return h.request(http.MethodPost, "/schedules/"+scheduleID+"/complete", map[string]any{
		"case_id": caseID,
	})
}

func expectPlanStatus(response apiResponse, expected string) error {
	status, err := nestedString(response.Body, "status")
	if err != nil {
		return err
	}
	if status != expected {
		return fmt.Errorf("plan status = %q, expected %q: %s", status, expected, response.Raw)
	}
	return nil
}

func (h *harness) rejectedCase(tag string) (string, error) {
	deviceID, err := h.createDevice(tag)
	if err != nil {
		return "", err
	}
	opened, err := h.openCase(deviceID)
	if err != nil {
		return "", err
	}
	caseID, err := nestedString(opened.Body, "case", "id")
	if err != nil {
		return "", err
	}
	response, err := h.recordMeasurement(caseID, 26.5, "Rui")
	if err != nil {
		return "", err
	}
	if err := expectStatus(response, http.StatusCreated); err != nil {
		return "", err
	}
	return caseID, nil
}

func (h *harness) readyCase(tag string) (string, error) {
	deviceID, err := h.createDevice(tag)
	if err != nil {
		return "", err
	}
	opened, err := h.openCase(deviceID)
	if err != nil {
		return "", err
	}
	caseID, err := nestedString(opened.Body, "case", "id")
	if err != nil {
		return "", err
	}
	response, err := h.recordMeasurement(caseID, 25.2, "Rui")
	if err != nil {
		return "", err
	}
	if err := expectStatus(response, http.StatusCreated); err != nil {
		return "", err
	}
	return caseID, nil
}

func expectStatus(response apiResponse, expected int) error {
	if response.Status != expected {
		return fmt.Errorf("status = %d, expected %d: %s", response.Status, expected, response.Raw)
	}
	return nil
}

func expectErrorCode(response apiResponse, expected string) error {
	code, err := nestedString(response.Body, "error", "code")
	if err != nil {
		return err
	}
	if code != expected {
		return fmt.Errorf("error code = %q, expected %q", code, expected)
	}
	return nil
}

func nestedString(root map[string]any, path ...string) (string, error) {
	value, err := nestedValue(root, path...)
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", strings.Join(path, "."))
	}
	return text, nil
}

func nestedBool(root map[string]any, path ...any) (bool, error) {
	value, err := nestedAny(root, path...)
	if err != nil {
		return false, err
	}
	flag, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%v is not a boolean", path)
	}
	return flag, nil
}

func nestedStringAt(root map[string]any, path ...any) (string, error) {
	value, err := nestedAny(root, path...)
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%v is not a string", path)
	}
	return text, nil
}

func nestedSlice(root map[string]any, key string) ([]any, error) {
	value, ok := root[key]
	if !ok {
		return nil, fmt.Errorf("missing %s", key)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an array", key)
	}
	return items, nil
}

func hasEvent(root map[string]any, kind string) bool {
	items, err := nestedSlice(root, "events")
	if err != nil {
		return false
	}
	for _, item := range items {
		event, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if event["kind"] == kind {
			return true
		}
	}
	return false
}

func nestedValue(root map[string]any, path ...string) (any, error) {
	parts := make([]any, 0, len(path))
	for _, part := range path {
		parts = append(parts, part)
	}
	return nestedAny(root, parts...)
}

func nestedAny(root map[string]any, path ...any) (any, error) {
	var current any = root
	for _, part := range path {
		switch typed := part.(type) {
		case string:
			object, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s is not an object", typed)
			}
			value, ok := object[typed]
			if !ok {
				return nil, fmt.Errorf("missing %s", typed)
			}
			current = value
		case int:
			items, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("index %d is not inside an array", typed)
			}
			if typed < 0 || typed >= len(items) {
				return nil, fmt.Errorf("index %d is out of range", typed)
			}
			current = items[typed]
		default:
			return nil, fmt.Errorf("unsupported path element %T", part)
		}
	}
	return current, nil
}
