# Metrology Assurance Suite

Metrology Assurance Suite is a Go HTTP service for bounded laboratory
measurement assurance work. It registers devices, opens verification cases,
records measurements, handles failed tolerance decisions, and seals traceable
evidence records. It also plans recalibration work before a device's
calibration basis expires: per-device plan windows with responsible owners,
arrangements booked against real currently-valid bases, and completion against
sealed recalibration cases.

## Run

```bash
go run ./cmd/metrology-service --addr 127.0.0.1:18121
```

The service listens on `127.0.0.1:18121` by default.

## Build And Verify

```bash
go build ./...
go test ./...
go run ./cmd/workflowcheck --workflow register-device
go run ./cmd/workflowcheck --workflow open-verification-case
go run ./cmd/workflowcheck --workflow record-measurement
go run ./cmd/workflowcheck --workflow reopen-verification-case
go run ./cmd/workflowcheck --workflow seal-verification
go run ./cmd/workflowcheck --workflow plan-calibration-due
```

The workflow runner starts a fresh in-process HTTP service and exercises the
public API. It is a production smoke command, not a unit test.

## API

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

## Calibration planning

A calibration basis is a device's real calibration authority (certificate
number, issuer, and an `effective_at`/`expires_at` validity window). A plan
opens a recalibration window for one device with an owner. Its read-time
`status` is one of:

- `pending` — inside the window, waiting for an arrangement.
- `scheduled` — an arrangement is booked; `status_reason` says whether booking
  was inside the window, and `arrangement.on_time` freezes that decision.
- `overdue` — a pending plan whose window closed before booking; derived from
  the clock, never stored, so history is not rewritten.
- `completed` — closed against a sealed recalibration case and a valid basis.

An arrangement is accepted only for an active device with no unfinished case
and a currently valid basis owned by that device. The on-time decision is
snapshotted at booking time, so rescheduling the window later cannot turn a
historical on-time booking into a late one (or vice versa). Completion
requires a sealed case for the same device plus a currently valid basis.


## Layout

- `cmd/metrology-service`: executable HTTP server.
- `cmd/workflowcheck`: bounded workflow verification commands.
- `internal/httpapi`: route and JSON boundary.
- `internal/assurance`: workflow orchestration.
- `internal/domain`: state rules and invariants.
- `internal/repository`: copy-on-write in-memory persistence.
- `internal/contracts`: API request and response models.
- `internal/bootstrap`: concrete dependency wiring.

## Inputs And Outputs

Inputs are JSON HTTP requests. Outputs are JSON response envelopes with either a
resource view or a stable error object:

```json
{
  "error": {
    "code": "state_conflict",
    "message": "case is not awaiting measurement"
  }
}
```

The service has no environment variables and no external dependencies.
