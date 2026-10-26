# CLAUDE.md

Guidance for AI coding agents working in this repository.

## What this repository is

A take-home for Olivaw Engineering: the **server side only** of a robot fleet management system, in Go. It ingests robot telemetry, keeps each robot's current state and history, and tracks low-battery alerts. No simulator, no UI. Reviewers care most about strategic DDD: how the business maps to the domain, boundaries and decoupling. Every line must be easy to defend.

## The documents are the spec

Read them before changing behaviour. Each has one job; do not copy content from one into another.

| Document | Defines |
|---|---|
| [ROBOT_FLEET_PRD.md](./ROBOT_FLEET_PRD.md) | Observable behaviour; requirement IDs (`REG-`, `TEL-`, `HLT-`, `ALT-`, `VIS-`) |
| [ROBOT_FLEET_DOMAIN_DESIGN.md](./ROBOT_FLEET_DOMAIN_DESIGN.md) | Subdomains, boundaries, language, aggregates, invariants, trade-offs |
| [ROBOT_FLEET_IMPLEMENTATION_PLAN.md](./ROBOT_FLEET_IMPLEMENTATION_PLAN.md) | Package layout, tables, API, error codes, algorithms, tests, backlog |
| [README.md](./README.md) | Reviewer-facing: choices, what was faked, production notes. Draft; `_TBD_` sections fill in as slices land |

If code and documents disagree, stop and ask; do not quietly change either. A behaviour change is not done until the matching sentence in the docs is correct. Never call something "planned" that exists, or "built" that is missing.

## How work proceeds

Follow the backlog in the implementation plan §9: ten vertical slices, **one at a time, one commit each**. Each slice goes through domain, application, Postgres, HTTP and tests, leaves `make test` passing, and adds only the code, columns and endpoints it needs. Stop after each slice so the user can review before the next one.

Do not build ahead of the current slice, and do not add anything the documents leave out (auth, broker, simulator, site API, extra alert types).

## Stack and commands

Go 1.25 · standard-library `net/http` (`http.ServeMux` with method patterns, `Request.PathValue`; no Gin, no third-party router) · PostgreSQL · golang-migrate · testcontainers-go.

Makefile targets: `help`, `up`, `down`, `test`, `test-integration`, `migrate-up`; `demo` arrives in slice 10. Integration tests use the `integration` build tag and need Docker.

Config is environment variables only (Compose reads `.env`; `.env.example` lists them): `DATABASE_URL`, `HTTP_SERVER_ADDRESS` (default `127.0.0.1:8080`, loopback on purpose: the API has no auth). `ALERT_CHECK_INTERVAL` (default `5s`) arrives in slice 7. Compose publishes ports on `127.0.0.1` only.

## Architecture

One bounded context (Fleet Management), one `fleet` module, one Go service, ports and adapters. Layout mirrors `../go-order-system-showcase/order-service`:

```text
cmd/fleet/main.go             # calls bootstrap.Run
internal/bootstrap/           # builds the process: config, logger, DB, HTTP server, alert check, shutdown
internal/fleet/domain/        # Robot, Alert, value objects, domain errors; standard library only
internal/fleet/application/   # use cases, queries, ports.go, errors.go
internal/fleet/adapter/inbound/httpapi/     # routes, DTOs, error → status mapping
internal/fleet/adapter/inbound/alertcheck/  # ticker running ReconcileAlerts
internal/fleet/adapter/outbound/postgres/   # repositories, transactions, read queries, reconciliation SQL
internal/fleet/wiring/        # composition root
internal/platform/{config,database,httpserver}/  # httpserver also holds /healthz and /readyz
migrations/                   # golang-migrate; 000001 creates and seeds sites
```

- `domain` imports the standard library only. It does not know HTTP or SQL.
- Only `wiring` and `bootstrap` import both concrete adapters and the application layer. Pass dependencies explicitly; no container.
- Error codes are application-level. Only `httpapi` maps them to HTTP statuses.
- Inject the clock (`func() time.Time`) into the application layer; never call `time.Now()` in domain or application code.
- Supporting parts stay thin: Site is reference data, not an aggregate; registration is a constructor and an insert.

## Rules that are easy to get wrong

- **Two aggregates: Robot and Alert.** Alerts are not inside Robot. Alerts are a *projection* of robot state, reconciled by a background check every 5 s with set-based SQL. Ingestion never touches alerts. `Acknowledge` is the only alert behaviour in the aggregate.
- **Four separate facts:** lifecycle, connectivity, operating mode, battery health. None is derived from another.
- **Connectivity is computed on read**, never stored: `UNKNOWN` with no applied observation, `ONLINE` if the last *applied* one was *received* ≤ 10 s ago, else `STALE`. Uses `received_at`, not `observed_at`.
- **Battery health has hysteresis:** low below 20%, back to normal at 25% or more. Stored in `robots.battery_health` and updated only when it changes. `CHARGING` and `ERROR` do not affect it.
- **Measurements live only in `telemetry_observations`.** `robots` holds registry fields, lifecycle, `activated_at` and battery health. Observation rows are immutable, one insert each.
- **Fetch a robot's current observation only as** `WHERE robot_id = … AND outcome = 'APPLIED' ORDER BY observed_at DESC LIMIT 1`, answered by the partial index. Never `MAX()` or `EXISTS (… received_at > …)`. An `EXPLAIN` test guards this.
- **RecordTelemetry order matters:** lock robot row → duplicate/conflict check → active check and clock skew → compare with latest applied → insert `SUPERSEDED` or `APPLIED`. The duplicate check comes before the active check, so a resend after deactivation is still `duplicate`.
- **Outcomes:** `applied`, `superseded`, `duplicate` are all successes. Same observation ID with different content is `OBSERVATION_CONFLICT`. More than 5 s in the future is `CLOCK_SKEW_EXCEEDED`.
- **Battery is a two-decimal percentage.** Reject extra precision; do not round, or duplicate detection breaks.
- **At most one unresolved alert per robot** is enforced by the partial unique index, not by code alone.
- **Deactivation closes the unresolved alert immediately** with `ROBOT_DEACTIVATED` (`CLOSED`, not `RESOLVED`). Reactivation sets `activated_at`; a new alert waits for an applied observation received after it.
- **Unknown values are `null`**, never made-up defaults.
- **Summary:** `online`, `stale`, `low_battery` count active robots only; `online + stale` may be less than `active`.

## Style

- **Plain words, short sentences**, in code, docs and chat. The user often asks for simpler wording; write it simple the first time.
- **Straightforward code.** No abstraction without a second caller. No speculative interfaces or config.
- **Comments are sparse:** explain what is surprising, never narrate what the code says.
- **Doc changes:** when asked to suggest or rewrite wording, reply in chat only. Edit the documents only when the user says to apply it. Keep edits minimal.
- **Tests:** table-driven domain tests; application tests with fakes; Postgres behaviour with testcontainers; HTTP with `httptest`. See plan §8.

## Commits

A title only: `Area: Imperative summary`, for example `Fleet: Record telemetry with duplicate detection`, `Docs: …`, `Build: …`. No body, no trailers, no `Co-Authored-By`. Commit only when asked, only the files named, on the current branch. Never push.
