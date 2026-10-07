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
		"plan-calibration-due":     checkPlanCalibrationDue,
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

// checkPlanCalibrationDue exercises the calibration due plan lifecycle: a
// pending window, a frozen on-time arrangement that survives a later window
// change, an overdue plan whose stored state is never rewritten, and the real
// device/basis/unfinished-case guards around arranging and completing.
func checkPlanCalibrationDue() error {
	h := newHarness()
	defer h.close()

	past := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	farPast := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	nearFuture := time.Now().Add(72 * time.Hour).Format(time.RFC3339)
	farFuture := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	soon := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
	later := time.Now().Add(96 * time.Hour).Format(time.RFC3339)

	// A real, active device and a currently valid calibration basis.
	deviceID, err := h.createDevice("MET-2001")
	if err != nil {
		return err
	}
	basisID, err := h.createBasis(deviceID, "CERT-2001", past, farFuture)
	if err != nil {
		return err
	}
	if _, err := h.createBasis(deviceID, "cert-2001", past, farFuture); err == nil {
		return errors.New("duplicate normalized certificate was accepted")
	}

	// Creating a plan requires the real, currently valid basis.
	invalidBasis, err := h.request(http.MethodPost, "/plans", map[string]any{
		"device_id":        deviceID,
		"basis_id":         "basis-9999",
		"window_starts_at": past,
		"window_due_at":    nearFuture,
		"owner":            "Lin",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(invalidBasis, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(invalidBasis, "basis_invalid"); err != nil {
		return err
	}

	planID, err := h.createPlan(deviceID, basisID, past, nearFuture, "Lin")
	if err != nil {
		return err
	}

	// Only one open plan per device.
	duplicate, err := h.request(http.MethodPost, "/plans", map[string]any{
		"device_id":        deviceID,
		"basis_id":         basisID,
		"window_starts_at": past,
		"window_due_at":    nearFuture,
		"owner":            "Lin",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(duplicate, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(duplicate, "active_plan_exists"); err != nil {
		return err
	}

	if err := expectPlanStatus(h, planID, "pending"); err != nil {
		return err
	}

	// A second device with an already-closed window explains overdue without
	// any stored state change.
	overdueDeviceID, err := h.createDevice("MET-2002")
	if err != nil {
		return err
	}
	overdueBasisID, err := h.createBasis(overdueDeviceID, "CERT-2002", past, farFuture)
	if err != nil {
		return err
	}
	overduePlanID, err := h.createPlan(overdueDeviceID, overdueBasisID, farPast, past, "Lin")
	if err != nil {
		return err
	}
	if err := expectPlanStatus(h, overduePlanID, "overdue"); err != nil {
		return err
	}
	stored, err := h.request(http.MethodGet, "/plans/"+overduePlanID, nil)
	if err != nil {
		return err
	}
	storedState, err := nestedString(stored.Body, "plan", "state")
	if err != nil {
		return err
	}
	if storedState != "pending" {
		return fmt.Errorf("overdue explanation rewrote stored state to %q", storedState)
	}
	overdueList, err := h.request(http.MethodGet, "/plans?status=overdue", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(overdueList, http.StatusOK); err != nil {
		return err
	}
	if !listContainsPlan(overdueList.Body, overduePlanID) || listContainsPlan(overdueList.Body, planID) {
		return errors.New("overdue status filter did not return only overdue plans")
	}

	// An unfinished case blocks an arrangement (no invalid arrangement while
	// assurance work for the device is already open).
	opened, err := h.openCase(deviceID)
	if err != nil {
		return err
	}
	blocked, err := h.arrangePlan(planID, soon, "Rui", basisID, "")
	if err != nil {
		return err
	}
	if err := expectStatus(blocked, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(blocked, "active_case_exists"); err != nil {
		return err
	}
	conflictingCaseID, err := nestedString(opened.Body, "case", "id")
	if err != nil {
		return err
	}

	// An expired basis cannot back an arrangement.
	expiredBasisID, err := h.createBasis(overdueDeviceID, "CERT-EXPIRED", farPast, past)
	if err != nil {
		return err
	}
	expiredArrangement, err := h.arrangePlan(overduePlanID, soon, "Rui", expiredBasisID, "")
	if err != nil {
		return err
	}
	if err := expectStatus(expiredArrangement, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(expiredArrangement, "basis_invalid"); err != nil {
		return err
	}

	// Close the blocking case so the on-time arrangement can proceed.
	if _, err := h.recordMeasurement(conflictingCaseID, 25.2, "Rui"); err != nil {
		return err
	}
	sealed, err := h.request(http.MethodPost, "/cases/"+conflictingCaseID+"/seal", map[string]any{
		"reviewer": "Verifier",
		"summary":  "first cycle sealed",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(sealed, http.StatusCreated); err != nil {
		return err
	}

	arranged, err := h.arrangePlan(planID, soon, "Rui", basisID, "")
	if err != nil {
		return err
	}
	if err := expectStatus(arranged, http.StatusCreated); err != nil {
		return err
	}
	status, err := nestedString(arranged.Body, "plan", "status")
	if err != nil {
		return err
	}
	if status != "scheduled" {
		return fmt.Errorf("arranged plan status = %q", status)
	}
	onTime, err := nestedBool(arranged.Body, "plan", "arrangement", "on_time")
	if err != nil {
		return err
	}
	if !onTime {
		return errors.New("arrangement booked before the window closed was not frozen as on time")
	}
	reason, err := nestedString(arranged.Body, "plan", "status_reason")
	if err != nil {
		return err
	}
	if !strings.Contains(reason, "within the plan window") {
		return fmt.Errorf("scheduled status does not explain the on-time booking: %q", reason)
	}

	// A second arrangement is rejected.
	secondArrangement, err := h.arrangePlan(planID, later, "Rui", basisID, "")
	if err != nil {
		return err
	}
	if err := expectStatus(secondArrangement, http.StatusConflict); err != nil {
		return err
	}
	if err := expectErrorCode(secondArrangement, "state_conflict"); err != nil {
		return err
	}

	// Moving the plan window forward must not retroactively rewrite the
	// historically on-time arrangement, nor reopen the plan.
	rescheduled, err := h.request(http.MethodPost, "/plans/"+planID+"/reschedule", map[string]any{
		"window_starts_at": nearFuture,
		"window_due_at":    farFuture,
		"owner":            "Mira",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(rescheduled, http.StatusOK); err != nil {
		return err
	}
	stillOnTime, err := nestedBool(rescheduled.Body, "plan", "arrangement", "on_time")
	if err != nil {
		return err
	}
	if !stillOnTime {
		return errors.New("plan change retroactively rewrote an on-time arrangement")
	}
	stillScheduled, err := nestedString(rescheduled.Body, "plan", "state")
	if err != nil {
		return err
	}
	if stillScheduled != "scheduled" {
		return fmt.Errorf("rescheduling changed stored plan state to %q", stillScheduled)
	}

	// Completion requires a sealed recalibration case for the same device.
	unsealedDeviceID, err := h.createDevice("MET-2003")
	if err != nil {
		return err
	}
	unsealedBasisID, err := h.createBasis(unsealedDeviceID, "CERT-2003", past, farFuture)
	if err != nil {
		return err
	}
	unsealedPlanID, err := h.createPlan(unsealedDeviceID, unsealedBasisID, past, nearFuture, "Lin")
	if err != nil {
		return err
	}
	unsealedCase, err := h.openCaseFor(unsealedDeviceID)
	if err != nil {
		return err
	}
	unsealedCaseID, err := nestedString(unsealedCase.Body, "case", "id")
	if err != nil {
		return err
	}
	if blockedArrangement, err := h.arrangePlan(unsealedPlanID, soon, "Rui", unsealedBasisID, unsealedCaseID); err != nil {
		return fmt.Errorf("arrange request failed: %w", err)
	} else if err := expectStatus(blockedArrangement, http.StatusConflict); err != nil {
		return err
	}
	if _, err := h.recordMeasurement(unsealedCaseID, 25.2, "Rui"); err != nil {
		return err
	}
	if _, err := h.request(http.MethodPost, "/cases/"+unsealedCaseID+"/seal", map[string]any{
		"reviewer": "Verifier",
		"summary":  "sealed for plan completion",
	}); err != nil {
		return err
	}

	// Back on the main device: run the recalibration cycle through sealing and
	// register a renewed, still-valid basis.
	recalibratedCase, err := h.openCaseFor(deviceID)
	if err != nil {
		return err
	}
	recalibratedCaseID, err := nestedString(recalibratedCase.Body, "case", "id")
	if err != nil {
		return err
	}
	if _, err := h.recordMeasurement(recalibratedCaseID, 25.1, "Rui"); err != nil {
		return err
	}
	if _, err := h.request(http.MethodPost, "/cases/"+recalibratedCaseID+"/seal", map[string]any{
		"reviewer": "Verifier",
		"summary":  "recalibration sealed",
	}); err != nil {
		return err
	}
	renewedBasisID, err := h.createBasis(deviceID, "CERT-2001-R", past, farFuture)
	if err != nil {
		return err
	}

	// A foreign device's sealed case cannot complete the plan.
	foreign, err := h.request(http.MethodPost, "/plans/"+planID+"/completions", map[string]any{
		"case_id":  unsealedCaseID,
		"basis_id": renewedBasisID,
	})
	if err != nil {
		return err
	}
	if err := expectStatus(foreign, http.StatusBadRequest); err != nil {
		return err
	}

	completed, err := h.request(http.MethodPost, "/plans/"+planID+"/completions", map[string]any{
		"case_id":  recalibratedCaseID,
		"basis_id": renewedBasisID,
	})
	if err != nil {
		return err
	}
	if err := expectStatus(completed, http.StatusCreated); err != nil {
		return err
	}
	completedStatus, err := nestedString(completed.Body, "plan", "status")
	if err != nil {
		return err
	}
	if completedStatus != "completed" {
		return fmt.Errorf("completed plan status = %q", completedStatus)
	}

	// A completed plan is frozen: further window changes are rejected.
	again, err := h.request(http.MethodPost, "/plans/"+planID+"/reschedule", map[string]any{
		"window_starts_at": nearFuture,
		"window_due_at":    farFuture,
		"owner":            "Mira",
	})
	if err != nil {
		return err
	}
	if err := expectStatus(again, http.StatusConflict); err != nil {
		return err
	}

	// The audit trail explains every meaningful plan change.
	events, err := h.request(http.MethodGet, "/plans/"+planID+"/events", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(events, http.StatusOK); err != nil {
		return err
	}
	for _, kind := range []string{
		"calibration_plan_created",
		"calibration_plan_arranged",
		"calibration_plan_rescheduled",
		"calibration_plan_completed",
	} {
		if !hasEvent(events.Body, kind) {
			return fmt.Errorf("plan audit event %q is missing", kind)
		}
	}
	return nil
}

func (h *harness) createBasis(deviceID, certificate, effective, expires string) (string, error) {
	response, err := h.request(http.MethodPost, "/calibration-bases", map[string]any{
		"device_id":          deviceID,
		"certificate_number": certificate,
		"issuer":             "Metrology Lab",
		"effective_at":       effective,
		"expires_at":         expires,
	})
	if err != nil {
		return "", err
	}
	if err := expectStatus(response, http.StatusCreated); err != nil {
		return "", err
	}
	return nestedString(response.Body, "basis", "id")
}

func (h *harness) createPlan(deviceID, basisID, starts, due, owner string) (string, error) {
	response, err := h.request(http.MethodPost, "/plans", map[string]any{
		"device_id":        deviceID,
		"basis_id":         basisID,
		"window_starts_at": starts,
		"window_due_at":    due,
		"owner":            owner,
	})
	if err != nil {
		return "", err
	}
	if err := expectStatus(response, http.StatusCreated); err != nil {
		return "", err
	}
	return nestedString(response.Body, "plan", "id")
}

func (h *harness) arrangePlan(planID, scheduledFor, assignee, basisID, caseID string) (apiResponse, error) {
	body := map[string]any{
		"scheduled_for": scheduledFor,
		"assignee":      assignee,
		"basis_id":      basisID,
	}
	if caseID != "" {
		body["case_id"] = caseID
	}
	return h.request(http.MethodPost, "/plans/"+planID+"/arrangements", body)
}

func (h *harness) openCaseFor(deviceID string) (apiResponse, error) {
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

func expectPlanStatus(h *harness, planID, expected string) error {
	response, err := h.request(http.MethodGet, "/plans/"+planID, nil)
	if err != nil {
		return err
	}
	if err := expectStatus(response, http.StatusOK); err != nil {
		return err
	}
	status, err := nestedString(response.Body, "plan", "status")
	if err != nil {
		return err
	}
	if status != expected {
		return fmt.Errorf("plan %s status = %q, expected %q", planID, status, expected)
	}
	return nil
}

func listContainsPlan(root map[string]any, planID string) bool {
	items, err := nestedSlice(root, "plans")
	if err != nil {
		return false
	}
	for _, item := range items {
		plan, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if plan["id"] == planID {
			return true
		}
	}
	return false
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
