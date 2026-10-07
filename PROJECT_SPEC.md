# Metrology Assurance Suite

## Project Goal

Metrology Assurance Suite is a local HTTP service that keeps a traceable record
of laboratory measurement assurance work. It registers devices, opens a
verification case for one device, records technician measurements, requests a
repeat measurement when tolerance is exceeded, and seals the final evidence
record after review. It also plans recalibration work ahead of calibration
basis expiry: a steward creates a per-device plan window with a responsible
owner, books an arrangement against a real, currently valid calibration basis,
and completes the plan against a sealed recalibration case and a renewed basis.

The service is intentionally self-contained. It uses an in-memory repository
with atomic copy-on-write mutations and does not require a database or an
external service.

## Users

- Laboratory steward: registers devices and opens verification cases.
- Measurement technician: records observed values for an open case.
- Quality verifier: reviews accepted measurements and seals the case.

## Core Entities

- Device: a laboratory asset with a unique asset tag, model, room, steward, and
  lifecycle state.
- VerificationCase: one bounded assurance activity for a device, with a nominal
  value, tolerance, current state, version, and assigned steward.
- Measurement: an observed value, computed deviation, acceptance decision,
  technician, and timestamp.
- EvidenceSeal: the immutable closing record for a reviewed case, including the
  selected accepted measurement.
- CalibrationBasis: the real calibration authority for a device (for example a
  calibration certificate) with a certificate number, issuer, and validity
  window. A basis is usable only while the current time is inside that window.
- CalibrationPlan: a per-device recalibration window with a responsible owner,
  an immutable arrangement snapshot, and an optional completion record.
- AuditEvent: an append-only trace of meaningful case, device, basis, and plan
  changes.

## Workflows

### 1. Register Device

The steward sends `POST /devices` with an asset tag, model, room, and steward.
The service validates the request, normalizes the asset tag, rejects duplicate
tags, and creates an active device.

### 2. Open Verification Case

The steward sends `POST /cases` with a device ID, nominal value, tolerance, and
owner. The service verifies that the device exists, is active, and has no
unfinished case. A valid request creates a case in `awaiting_measurement`.

### 3. Record Measurement

A technician sends `POST /cases/{id}/measurements`. The case must be
`awaiting_measurement`. The service computes the signed deviation from the
nominal value. An in-tolerance measurement moves the case to
`review_ready`; an out-of-tolerance measurement moves it to
`recheck_required`.

### 4. Reopen Verification Case

The steward sends `POST /cases/{id}/reopen` after an out-of-tolerance result.
Only `recheck_required` cases may move back to `awaiting_measurement`. The
service records the actor and reason in the audit trail.

### 5. Seal Verification

The verifier sends `POST /cases/{id}/seal` after an accepted measurement. Only
`review_ready` cases may be sealed. The service selects the accepted
measurement with the smallest absolute deviation, creates exactly one evidence
seal, moves the case to `sealed`, and rejects later mutations.

### 6. Register Calibration Basis

The steward sends `POST /calibration-bases` with a device, a certificate
number, an issuer, and a validity window (`effective_at` until `expires_at`).
The service verifies the device exists and is active, normalizes the
certificate number, rejects a duplicate certificate for the same device, and
records the basis. A basis is valid at an instant only inside its window; the
expiry instant itself is treated as expired.

### 7. Create Calibration Plan

The steward sends `POST /plans` with a device, a currently valid basis for that
device, a window (`window_starts_at` until `window_due_at`), and an owner. The
service rejects an unknown, foreign, or expired basis and a second open plan
for the same device. The new plan is `pending`.

### 8. Explain Due Status

The service derives a read-time status from the stored plan and the current
time:

- `pending`: waiting for an arrangement within the plan window.
- `scheduled`: an arrangement has been booked; the reason says whether it was
  booked within the window.
- `overdue`: a pending plan whose window closed before an arrangement was
  booked. Overdue is a derived explanation, not a stored state, so it changes
  with the clock without mutating history.
- `completed`: recalibration finished against a sealed case and a valid basis.

`GET /plans` lists plans sorted with overdue work first and accepts an optional
`status` filter; `GET /plans/{id}` returns one plan with its audit events.

### 9. Arrange Recalibration

The owner sends `POST /plans/{id}/arrangements` with a scheduled time, an
assignee, and the basis used for the work. The service rejects the booking
when the plan is not pending, the device is not active, the device already has
an unfinished verification case, or the basis is unknown, foreign, or expired.
The arrangement is an immutable snapshot. Its `on_time` decision is computed
once against the window in effect at booking time and stored on the snapshot,
so later rescheduling cannot rewrite a historically on-time (or late) booking.

### 10. Reschedule Plan

The owner sends `POST /plans/{id}/reschedule` with a new window or owner. A
completed plan cannot be rescheduled. Rescheduling changes only the plan
window and owner; the frozen arrangement snapshot and its timeliness are left
untouched.

### 11. Complete Calibration Plan

The steward sends `POST /plans/{id}/completions` with a sealed recalibration
case and a currently valid basis. The service rejects completion when the case
is unknown, belongs to another device, or is not sealed, or when the basis is
unknown, foreign, or expired. Completion records the sealed case and renewed
basis, moves the plan to `completed`, and freezes further plan changes.

## State And Rules

Device state:

- `active -> retired`
- `retired` is terminal.

VerificationCase state:

- `awaiting_measurement -> review_ready`
- `awaiting_measurement -> recheck_required`
- `recheck_required -> awaiting_measurement`
- `review_ready -> sealed`
- `sealed` is terminal.

CalibrationPlan state:

- `pending -> scheduled`
- `scheduled -> completed`
- `completed` is terminal.
- `overdue` is not a stored state; it is derived at read time from a pending
  plan whose window has closed.

Required invariants:

- Asset tags are unique after case-insensitive normalization.
- A device has at most one unfinished verification case.
- Measurements are accepted only while a case is awaiting measurement.
- A tolerance must be positive.
- A case can have at most one evidence seal.
- A sealed case cannot accept measurements, reopen, or seal again.
- Calibration certificate numbers are unique per device after normalization.
- A basis is usable only inside its validity window; the expiry instant is
  expired.
- A device has at most one open (`pending` or `scheduled`) calibration plan.
- A plan must cite a real basis owned by its device.
- An arrangement requires an active device, a pending plan, no unfinished case
  for the device, and a currently valid basis owned by the device.
- An arrangement's on-time decision is frozen at booking time against the
  window then in effect; rescheduling never rewrites it.
- Completing a plan requires a sealed case for the same device and a currently
  valid basis owned by the device.
- A completed plan cannot be rescheduled, arranged, or completed again.
- Every mutation that changes a case, basis, or plan also appends an audit
  event in the same repository commit.

## Modules

- `cmd/metrology-service`: process entrypoint and graceful HTTP shutdown.
- `cmd/workflowcheck`: bounded smoke checks that call the public HTTP API.
- `internal/httpapi`: routing, request decoding, and stable error mapping.
- `internal/assurance`: workflow orchestration and atomic decisions.
- `internal/domain`: entities, validation, state transitions, and errors.
- `internal/repository`: mutex-protected in-memory persistence with copy-on-write
  commits.
- `internal/contracts`: request and response contracts shared by the HTTP layer.

## Public Interfaces

- `GET /healthz`
- `POST /devices`
- `GET /devices/{id}`
- `POST /cases`
- `GET /cases/{id}`
- `POST /cases/{id}/measurements`
- `POST /cases/{id}/reopen`
- `POST /cases/{id}/seal`
- `GET /cases/{id}/events`
- `POST /calibration-bases`
- `GET /devices/{id}/bases`
- `POST /plans`
- `GET /plans` (optional `status=pending|scheduled|overdue|completed`)
- `GET /plans/{id}`
- `POST /plans/{id}/arrangements`
- `POST /plans/{id}/completions`
- `POST /plans/{id}/reschedule`
- `GET /plans/{id}/events`

## Validation Plan

The baseline build command is `go build ./...`. Each declared workflow has a
bounded production smoke command under `cmd/workflowcheck`. The command starts
an HTTP test server in the same process, calls the public routes, and verifies
both success and failure behavior. The seal check also races two concurrent
seal requests to prove that the repository mutation lock permits exactly one
seal. The calibration plan check verifies the derived pending/overdue and
scheduled/completed explanations, proves an arrangement is blocked by an
unfinished case or an expired basis, proves a frozen on-time booking survives a
later window change, and proves completion requires a sealed same-device case
and a currently valid basis.

Domain and service unit tests cover the state transitions, the basis validity
window, the frozen arrangement snapshot, derived overdue status, and the
service-level guards with a controllable clock.
