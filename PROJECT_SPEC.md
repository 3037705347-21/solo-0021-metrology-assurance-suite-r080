# Metrology Assurance Suite

## Project Goal

Metrology Assurance Suite is a local HTTP service that keeps a traceable record
of laboratory measurement assurance work. It registers devices, opens a
verification case for one device, records technician measurements, requests a
repeat measurement when tolerance is exceeded, and seals the final evidence
record after review. Stewards also plan ahead: each device can carry a
calibration plan with a bounded window and a responsible owner, and the plan
is fulfilled by scheduling and completing an arrangement against a sealed
verification case before the calibration basis expires.

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
- CalibrationPlan: a bounded recalibration intent for one device, with a
  calibration basis, basis expiry, plan window, owner, state, and version.
- CalibrationSchedule: one arrangement inside a plan window, with an assignee,
  scheduled time, a snapshot of the plan window, and the sealed case that
  fulfilled it.
- AuditEvent: an append-only trace of meaningful case and device changes.

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

### 6. Create Calibration Plan

The steward sends `POST /plans` with a device ID, calibration basis, basis
expiry, plan window, and owner. The service verifies that the device exists,
is active, and has no unfinished plan. The window must be well formed and must
close on or before the basis expires. A valid request creates a plan in
`pending`.

### 7. Revise Calibration Plan

The steward sends `POST /plans/{id}/revise` with a replacement basis, basis
expiry, window, and owner. Completed plans cannot be revised. Existing
schedules keep the window they snapshot at creation, so a revision never
rewrites a historical on-time arrangement into an overdue one.

### 8. Create Schedule

The steward sends `POST /plans/{id}/schedules` with an assignee and a
scheduled time. The plan must be unfinished and have no active schedule, the
device must be active with no unfinished case, and the basis must not have
expired. The scheduled time must not be in the past and must fall inside the
plan window. A valid request snapshots the plan window onto the schedule and
moves the plan to `scheduled`.

### 9. Complete Schedule

The steward sends `POST /schedules/{id}/complete` with a case ID. The device
must be active, the basis must still be valid, and the referenced case must
belong to the plan device and be `sealed`. The service freezes whether the
completion landed inside the snapshot window, moves the schedule to
`completed`, and completes the plan.

### 10. Cancel Schedule

The steward sends `POST /schedules/{id}/cancel` with an actor and reason. Only
an arranged schedule can be cancelled; the plan returns to `pending` and can
be scheduled again.

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
- `scheduled -> pending` (schedule cancelled)
- `pending|scheduled -> completed`
- `completed` is terminal.

CalibrationSchedule state:

- `arranged -> completed`
- `arranged -> cancelled`
- `completed` and `cancelled` are terminal.

Derived plan status (explained, never stored):

- `completed`: the plan is fulfilled.
- `overdue`: the plan is unfinished and its window has closed.
- `scheduled`: the plan is unfinished, inside its window, with an active
  schedule.
- `pending`: the plan is unfinished, inside its window, with no schedule.

Required invariants:

- Asset tags are unique after case-insensitive normalization.
- A device has at most one unfinished verification case.
- Measurements are accepted only while a case is awaiting measurement.
- A tolerance must be positive.
- A case can have at most one evidence seal.
- A sealed case cannot accept measurements, reopen, or seal again.
- A device has at most one unfinished calibration plan.
- A plan window must close on or before the calibration basis expires.
- A plan has at most one active schedule.
- A schedule is created only while the device has no unfinished case and the
  calibration basis has not expired.
- A schedule is completed only against a sealed case of the same device while
  the device is active and the basis is still valid.
- A schedule keeps the plan window snapshot taken at creation; revising a plan
  never changes whether a historical arrangement was on time.
- Every mutation that changes a case also appends an audit event in the same
  repository commit.
- Every mutation that changes a plan or schedule also appends an audit event
  in the same repository commit.

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
- `POST /plans`
- `GET /plans` (optional `device_id` and `status` filters)
- `GET /plans/{id}`
- `POST /plans/{id}/revise`
- `POST /plans/{id}/schedules`
- `GET /plans/{id}/events`
- `POST /schedules/{id}/complete`
- `POST /schedules/{id}/cancel`

## Validation Plan

The baseline build command is `go build ./...`. Each declared workflow has a
bounded production smoke command under `cmd/workflowcheck`. The command starts
an HTTP test server in the same process, calls the public routes, and verifies
both success and failure behavior. The seal check also races two concurrent
seal requests to prove that the repository mutation lock permits exactly one
seal.

Tests are intentionally deferred for this initialization baseline. The later
engineering-task stage will add unit and integration tests, including red/green
verification for the workflows and their failure paths.
