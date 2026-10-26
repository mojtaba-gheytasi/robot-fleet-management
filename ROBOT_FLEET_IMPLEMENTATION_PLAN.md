# Robot Fleet Management — Technical Design & Implementation Plan

**Inputs:** [PRD](./ROBOT_FLEET_PRD.md) · [Domain Design](./ROBOT_FLEET_DOMAIN_DESIGN.md)

The PRD defines *observable behavior*; the Domain Design defines *models and invariants*; this document defines *structure, storage, API, execution and tests*.

**Stack:** Go 1.25 · standard-library `net/http` · PostgreSQL · testcontainers for integration tests.

## 1. Boundaries and deployment

- One Go executable, one Fleet Management bounded context, one `fleet` module, ports and adapters.
- HTTP requests are translated to application commands/queries; domain code does not depend on HTTP or SQL.
- PostgreSQL is the only infrastructure dependency.
- Ingestion stores the observation and updates robot state in one transaction. **Alerts are reconciled separately** by an in-process background check every 5 seconds.
- No simulator, frontend, authentication, authorization, message broker or microservices. `scripts/demo.sh` exercises the API with curl.
- Bind to loopback by default; unauthenticated mutation endpoints must not be exposed publicly.

### Package layout

```text
cmd/fleet/main.go                         # entry point: calls bootstrap.Run
internal/
  bootstrap/                              # load config, logger, DB; start HTTP server and alert check; graceful shutdown
  fleet/
    domain/                               # Robot, Alert, Observation, BatteryLevel, Position, OperatingMode, errors
    application/                          # use cases, queries, ports.go, errors.go
    adapter/
      inbound/
        httpapi/                          # http.ServeMux routes, handlers, request/response DTOs, error mapping
        alertcheck/                       # ticker that runs ReconcileAlerts every 5 s
      outbound/
        postgres/                         # repositories, transactions, read queries, reconciliation SQL
    wiring/                               # composition root: the only place that knows concrete adapters
  platform/
    config/  database/  httpserver/  observability/
migrations/                               # golang-migrate SQL files, including seeded sites
scripts/demo.sh
docker-compose.yml  Makefile  .env.example
```

`domain` and `application` are technical layers inside one module, not separate bounded contexts. Supporting parts (sites, registration) stay thin: no aggregate for Site, plain SQL for reads.

## 2. Domain implementation detail

- `Robot`: ID, SiteID, Lifecycle, `ActivatedAt`, BatteryHealth, and its latest applied observation (with `received_at`). The repository loads the `robots` row plus the latest applied observation; measurements are not copied into `robots`. Behaviour: `Activate(now)`, `Deactivate()`, `ApplyObservation(obs, receivedAt)`, `ConnectivityAt(now)`.
- `Observation` value object: ObservationID, ObservedAt, BatteryLevel, Position, OperatingMode.
- `Alert`: ID, RobotID, kind `LOW_BATTERY`, status, timestamps, closure reason. Behaviour: `Acknowledge(now)`, the only user-driven transition. Open, resolve and close are applied by set-based reconciliation SQL.
- `BatteryLevel`: canonical **two-decimal percentage** so duplicate detection compares reliably; reject excess precision rather than rounding.
- `Position`: finite `float64` X/Y in site-local metres.
- `OperatingMode`: `IDLE | WORKING | CHARGING | ERROR`.
- `Clock` (`func() time.Time`) injected into the application layer for deterministic tests.

## 3. Relational data design

### `sites`

| Field | Type / constraint |
|---|---|
| `id` | `text PRIMARY KEY` |
| `name` | `text NOT NULL` |
| `created_at` | `timestamptz NOT NULL` |

Seeded by a migration. No site API.

### `robots`

| Field | Type / constraint |
|---|---|
| `id` | `text PRIMARY KEY` |
| `site_id` | `text NOT NULL REFERENCES sites(id)` |
| `lifecycle` | `text NOT NULL CHECK IN ('ACTIVE','INACTIVE')` |
| `activated_at` | `timestamptz NOT NULL` — set at registration and on each `INACTIVE → ACTIVE` |
| `battery_health` | `text NOT NULL DEFAULT 'UNKNOWN' CHECK IN ('UNKNOWN','NORMAL','LOW')` |
| `created_at`, `updated_at` | `timestamptz NOT NULL` |

Only data that changes rarely: registry fields, lifecycle, and battery health, which is a classification of the robot and changes only on a transition (a few times a day). Battery percentage, position, mode and timestamps change every second and live **only** in `telemetry_observations`. Telemetry locks this row on every observation but updates it only when battery health changes. Connectivity is computed on read; no `ONLINE` flag is stored.

### `telemetry_observations`

| Field | Type / constraint |
|---|---|
| `robot_id` | `text NOT NULL REFERENCES robots(id)` |
| `observation_id` | `text NOT NULL` |
| `observed_at`, `received_at` | `timestamptz NOT NULL` |
| `battery_percent` | `numeric(5,2) NOT NULL CHECK [0,100]` |
| `position_x`, `position_y` | `double precision NOT NULL` |
| `operating_mode` | `text NOT NULL` with enum CHECK |
| `outcome` | `text NOT NULL CHECK IN ('APPLIED','SUPERSEDED')` |

Primary key `(robot_id, observation_id)`; index `(robot_id, observed_at DESC)` for history; **partial index `(robot_id, observed_at DESC) WHERE outcome = 'APPLIED'`** to find each robot's current observation in one lookup. Rows are immutable: each observation is a single insert.

**Rule for this table:** always fetch a robot's current observation with `WHERE robot_id = … AND outcome = 'APPLIED' ORDER BY observed_at DESC LIMIT 1`, which the partial index answers in one lookup however large the table grows. Never use `MAX()` or a condition that has to walk a robot's history. Applied `observed_at` values strictly increase per robot, so the latest one is unique.

### `alerts`

| Field | Type / constraint |
|---|---|
| `id` | `uuid PRIMARY KEY DEFAULT gen_random_uuid()` |
| `robot_id` | `text NOT NULL REFERENCES robots(id)` |
| `kind` | `text NOT NULL CHECK IN ('LOW_BATTERY')` |
| `status` | `text NOT NULL CHECK IN ('OPEN','ACKNOWLEDGED','RESOLVED','CLOSED')` |
| `opened_at` | `timestamptz NOT NULL` |
| `acknowledged_at`, `ended_at` | `timestamptz` nullable |
| `closure_reason` | `text` nullable; `ROBOT_DEACTIVATED` only for `CLOSED` |

**Partial unique index** `(robot_id, kind) WHERE status IN ('OPEN','ACKNOWLEDGED')` guarantees at most one unresolved alert per robot, even if two checks overlap. CHECK constraints tie `closure_reason` to `CLOSED`. No `acknowledged_by` without authentication.

## 4. HTTP API contract

Standard-library `http.ServeMux` with method patterns and `Request.PathValue()`; no third-party router. Strict JSON decoding (unknown fields rejected); bounded body size; RFC 3339 UTC timestamps.

| Method | Route | Behavior |
|---|---|---|
| `POST` | `/api/robots` | Register with ID and existing site ID; an identical retry is idempotent. |
| `POST` | `/api/robots/{id}/activate` | Idempotently set ACTIVE. |
| `POST` | `/api/robots/{id}/deactivate` | Idempotently set INACTIVE; close the unresolved alert immediately. |
| `POST` | `/api/robots/{id}/telemetry` | Record observation; return `applied`, `superseded` or `duplicate`. |
| `GET` | `/api/robots` | Fleet list; optional `site_id`. |
| `GET` | `/api/robots/{id}` | Current state. |
| `GET` | `/api/robots/{id}/telemetry` | Bounded recent history with outcome. |
| `GET` | `/api/fleet/summary` | Counts; optional `site_id`. |
| `GET` | `/api/alerts` | Unresolved or terminal alerts. |
| `POST` | `/api/alerts/{id}/acknowledge` | Acknowledge; repeat is idempotent. |
| `GET` | `/healthz`, `/readyz` | Liveness; readiness checks the database. |

### Ingest payload and responses

```json
{
  "observation_id": "obs-102",
  "observed_at": "2026-10-25T14:05:00Z",
  "battery_percent": 18.50,
  "position": {"x": 12.5, "y": 24.8},
  "operating_mode": "WORKING"
}
```

```json
{"outcome":"applied","robot_id":"robot-101","observation_id":"obs-102"}
```

### Error contract

Every error body carries a stable `code`. Codes are application-level errors; only `httpapi` maps them to HTTP statuses.

| Code | Status | Circumstance |
|---|---|---|
| `INVALID_REQUEST` | 400 | Malformed JSON or unsupported request shape |
| `ROBOT_NOT_FOUND` | 404 | Unknown robot |
| `SITE_NOT_FOUND` | 404 | Registration references an unknown site |
| `ALERT_NOT_FOUND` | 404 | Unknown alert |
| `ROBOT_REGISTRATION_CONFLICT` | 409 | Existing robot ID with different registration data |
| `ROBOT_INACTIVE` | 409 | New observation for an `INACTIVE` robot |
| `OBSERVATION_CONFLICT` | 409 | Observation ID reused with different content |
| `ALERT_TERMINAL` | 409 | Acknowledging a `RESOLVED` or `CLOSED` alert |
| `INVALID_OBSERVATION` | 422 | Battery, position, mode or precision invalid |
| `CLOCK_SKEW_EXCEEDED` | 422 | Observation time more than 5 s ahead of server time |
| `INTERNAL` | 500 | Unexpected failure; no internals exposed |

Success: `201` for a new registration, `200` otherwise. Transient database errors map to `500`, never to a business conflict.

## 5. Use-case algorithms

### RecordTelemetry (no alert work)

1. Validate the request; build a canonical Observation.
2. Begin a transaction; `SELECT … FOR UPDATE` the robot row (`ROBOT_NOT_FOUND` if missing). The lock makes observations for one robot take turns; it is held for milliseconds and never blocks other robots.
3. Look up `(robot_id, observation_id)`: same content → `duplicate`, no write; different content → `OBSERVATION_CONFLICT`. This runs before the active check, so a resend after deactivation is still `duplicate`.
4. New ID: require `ACTIVE` (`ROBOT_INACTIVE`); reject `observed_at` more than 5 s ahead of the clock (`CLOCK_SKEW_EXCEEDED`).
5. Read the latest applied observation (partial index). If one exists and `observed_at <= its observed_at`: insert as `SUPERSEDED`, commit, return `superseded`.
6. Otherwise, including a robot's first observation: `Robot.ApplyObservation` computes the new battery health; insert as `APPLIED`; **update `robots.battery_health` only if it changed**; commit; return `applied`.

Typical cost per observation: one row lock, two index lookups, one insert. An update to `robots` happens only on a health transition.

### DeactivateRobot / ActivateRobot

- **Deactivate:** lock robot, set `INACTIVE` (idempotent), close any unresolved alert with reason `ROBOT_DEACTIVATED` in the same transaction. History and last readings stay.
- **Activate:** lock robot; if `INACTIVE`, set `ACTIVE` and `activated_at = now`. Already `ACTIVE` is a no-op and keeps `activated_at`.

### AcknowledgeAlert

Lock the alert row, apply Alert.Acknowledge, commit.

- OPEN → becomes ACKNOWLEDGED; acknowledged_at is set.
- ACKNOWLEDGED → nothing changes; still a success.
- RESOLVED or CLOSED → rejected with ALERT_TERMINAL.

The alert check locks the same row when it resolves or closes an alert, so an acknowledgement and a resolution never overwrite each other; one waits for the other.

### ReconcileAlerts (every 5 seconds)

Runs every 5 seconds from the alertcheck ticker, in one transaction. If another copy of the service is already running the check, this run is skipped (a Postgres advisory lock), so two copies never do the same work.

```sql
-- open: active, LOW, reported since last activation, no unresolved alert
INSERT INTO alerts (robot_id, kind, status, opened_at)
SELECT r.id, 'LOW_BATTERY', 'OPEN', $now
FROM robots r
WHERE r.lifecycle = 'ACTIVE'
  AND r.battery_health = 'LOW'
  AND (SELECT o.received_at
       FROM telemetry_observations o
       WHERE o.robot_id = r.id AND o.outcome = 'APPLIED'
       ORDER BY o.observed_at DESC
       LIMIT 1) > r.activated_at
ON CONFLICT (robot_id, kind) WHERE status IN ('OPEN','ACKNOWLEDGED') DO NOTHING
RETURNING id;

-- resolve: robot back to NORMAL
UPDATE alerts a SET status = 'RESOLVED', ended_at = $now
FROM robots r
WHERE a.robot_id = r.id AND a.status IN ('OPEN','ACKNOWLEDGED') AND r.battery_health = 'NORMAL';

-- close: safety net for INACTIVE robots (deactivation normally closes immediately)
UPDATE alerts a SET status = 'CLOSED', closure_reason = 'ROBOT_DEACTIVATED', ended_at = $now
FROM robots r
WHERE a.robot_id = r.id AND a.status IN ('OPEN','ACKNOWLEDGED') AND r.lifecycle = 'INACTIVE';
```

- **Speed:** it reads the small `robots` table and does one quick index lookup for each low-battery robot. The size of the observations table makes no difference. (see README).
- **Timestamps:** `opened_at` and `ended_at` record when the check ran, not when the observation arrived, so they can be up to 5 seconds later than the reading that caused them.
- **Deactivation at the same moment:** if an operator deactivates a robot while the check is running, the check may still open an alert for it. The close step fixes this on the next run, at most 5 seconds later.
- **Shutdown:** the check stops before the database connection is closed.
- **Logging:** each run logs how many alerts it opened, resolved and closed.

## 6. Fleet queries (read side)

Kept deliberately simple. Reads that map to one aggregate reuse it; reports that don't get a small read repository.

| Read | Served by |
|---|---|
| Robot detail, fleet list | Robot repository: loads Robot aggregates (the `robots` row plus its current observation via `LEFT JOIN LATERAL (… ORDER BY observed_at DESC LIMIT 1)`), mapped to JSON by the HTTP adapter |
| Alert list | Alert repository: loads Alert aggregates |
| Fleet summary, observation history | Read repository returning plain structs (`FleetSummary`, `ObservationRecord`); no aggregate fits these |

- **Summary** is one SQL query with counts, not robots loaded into memory. `registered`, `active`, `inactive` and unresolved-alert counts cover all robots (or one site); `online`, `stale` and `low_battery` count **active robots only**. `online + stale` can be less than `active` because of `UNKNOWN`.
- **Connectivity** uses the domain rule `ConnectivityAt(now)`: no applied observation → `UNKNOWN`; received ≤ 10 s ago → `ONLINE`; otherwise `STALE`. The summary query applies the same 10-second rule in SQL.
- **History** returns 50 rows by default, at most 200, each marked `APPLIED` or `SUPERSEDED`.
- **Unknown values** are returned as `null`, never filled with made-up defaults.

## 7. Runtime and operations

- `bootstrap` loads config (`.env` / `.env.example`), builds the logger, opens the database, wires the module, starts the HTTP server and the alert check, and shuts both down on SIGINT/SIGTERM with a timeout.
- HTTP server with explicit read/write/idle timeouts.
- `docker-compose.yml`: PostgreSQL, a golang-migrate service, and the app.
- Makefile: `help`, `up`, `down`, `test`, `test-integration`, `migrate-up`, `demo`.
- Config: `DATABASE_URL`, `HTTP_SERVER_ADDRESS` (default `127.0.0.1:8080`), `ALERT_CHECK_INTERVAL` (default `5s`).

## 8. Test strategy

**Domain (table-driven, unit)**

- Battery hysteresis at `0`, `19.99`, `20.00`, `24.99`, `25.00`, `100`, starting from each health.
- Alert acknowledgement from every status.
- Robot lifecycle, `activated_at` on reactivation only, strictly advancing observed time, value-object validation.

**Application (unit, fakes)**

- RecordTelemetry outcomes and every rejection, including duplicate-after-deactivation and the first observation.

**PostgreSQL (integration, testcontainers)**

- Registration: identical retry idempotent and does not reactivate; different site is a conflict; unknown site.
- Applied observation becomes current state; `robots` is updated only when battery health changes; superseded is stored without affecting current state or health.
- Concurrent reports for one robot cannot regress current state.
- ReconcileAlerts: opens once for `LOW`; does not reopen while unresolved; resolves on `NORMAL`; closes for `INACTIVE`; after reactivation waits for a newer applied observation; overlapping runs never create two unresolved alerts.
- Acknowledge racing a resolve: one wins, no lost update.
- `EXPLAIN` of the open query and the fleet list shows an index scan on the `APPLIED` partial index, not a scan of observations.

**HTTP (`httptest`)**

- Status codes and error codes, strict input rejection, site filter, history bound, summary and `STALE` classification with a controllable clock.

## 9. Backlog

Ten vertical slices. Each delivers one working capability end to end (domain, application, Postgres, HTTP, tests), is one commit, and leaves `make test` passing. Each slice adds only the domain code, columns and endpoints it needs.

**1. Service runs locally**
As a developer, I can start the service and its database with one command.
- `go.mod`, package layout, compose with PostgreSQL, migrations runner, Makefile, `.env.example`.
- Bootstrap: config, logger, database, HTTP server, graceful shutdown.
- **Done when:** `make up` starts everything; `/healthz` returns 200; `/readyz` returns 200 only when the database is reachable.

**2. Register a robot** (REG-01, REG-02, REG-03)
As an operator, I can register a robot at a known site.
- `sites` (seeded) and `robots` tables; Robot registration; `POST /api/robots`.
- **Done when:** a new robot is created `ACTIVE`; an identical retry succeeds without change; same ID with different data → `ROBOT_REGISTRATION_CONFLICT`; unknown site → `SITE_NOT_FOUND`.

**3. See the fleet** (VIS-01, VIS-02, VIS-06)
As an operator, I can list robots and open one robot's details.
- `GET /api/robots` (optional `site_id`), `GET /api/robots/{id}`.
- **Done when:** a robot without telemetry shows battery health and connectivity `UNKNOWN` and its readings as `null`; the site filter works; an unknown robot → `ROBOT_NOT_FOUND`.

**4. Send telemetry** (TEL-01–TEL-04, TEL-08, HLT-04, HLT-05)
As a robot, I can report a snapshot, and operators see it as current state.
- Value objects (`BatteryLevel`, `Position`, `OperatingMode`, `Observation`); `telemetry_observations` with the partial index; `ApplyObservation`; `POST /api/robots/{id}/telemetry`.
- **Done when:** a valid snapshot returns `applied` and appears in robot details; the robot is `ONLINE`, and `STALE` after 10 s without a new snapshot; invalid values → `INVALID_OBSERVATION`.

**5. Reliable delivery** (TEL-05–TEL-07, TEL-09–TEL-11)
As a robot on an unreliable network, I can resend and send late without corrupting state.
- Per-robot row lock; duplicate, conflict, superseded and clock-skew handling.
- **Done when:** a resend → `duplicate`; same ID with new content → `OBSERVATION_CONFLICT`; an older snapshot → `superseded` and current state unchanged; a timestamp > 5 s ahead → `CLOCK_SKEW_EXCEEDED`; concurrent reports for one robot never regress current state.

**6. Battery health** (HLT-01–HLT-03, HLT-06–HLT-08)
As an operator, I can see which robots have low battery.
- Hysteresis in the Robot aggregate; `robots.battery_health` updated only when it changes.
- **Done when:** `60 → 18 → 22 → 25%` gives `NORMAL → LOW → LOW → NORMAL`; boundary values `19.99`, `20`, `24.99`, `25` are covered; `CHARGING` and `ERROR` do not affect health.

**7. Low-battery alerts** (ALT-01, ALT-02, ALT-05, ALT-07)
As an operator, I see an alert when a robot's battery is low, and it resolves when the robot recovers.
- `alerts` table with the partial unique index; Alert aggregate; `alertcheck` ticker running ReconcileAlerts every 5 s; `GET /api/alerts`.
- **Done when:** one alert opens within 5 s of `LOW`; no second one while unresolved; it resolves within 5 s of `NORMAL`; overlapping runs never create two unresolved alerts.

**8. Acknowledge an alert** (ALT-03, ALT-04, ALT-08)
As an operator, I can acknowledge an alert to show someone is on it.
- `Alert.Acknowledge`; `POST /api/alerts/{id}/acknowledge`.
- **Done when:** `OPEN` → `ACKNOWLEDGED`; repeating it succeeds with no change; a resolved or closed alert → `ALERT_TERMINAL`; an acknowledgement racing a resolution loses no update.

**9. Take a robot out of service and back** (REG-04–REG-07, ALT-06)
As an operator, I can deactivate and reactivate a robot.
- `activated_at` column; `POST /api/robots/{id}/deactivate` and `/activate`; close step in ReconcileAlerts.
- **Done when:** deactivation closes the unresolved alert immediately with `ROBOT_DEACTIVATED`; new telemetry → `ROBOT_INACTIVE`, but a resend → `duplicate`; after reactivation, an alert opens only after a fresh `LOW` snapshot.

**10. Fleet summary, history and demo** (VIS-02–VIS-05)
As a fleet manager, I can see fleet counts and a robot's recent history, and a reviewer can watch every workflow run.
- Read repository; `GET /api/fleet/summary`, `GET /api/robots/{id}/telemetry`; `scripts/demo.sh`; README filled in.
- **Done when:** summary counts match VIS-03 (online, stale and low-battery among active robots only); history is bounded and marks `APPLIED`/`SUPERSEDED`; `make demo` runs workflows A–E end to end.
