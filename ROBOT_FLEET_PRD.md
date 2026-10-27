# Robot Fleet Management — Product Requirements Document

## 1. Product overview

Robot Fleet Management gives fleet operators a dependable view of robots assigned to operational sites. It receives reports describing each robot's position, battery level and operating mode; retains observation history; maintains the latest operational state; and identifies battery conditions requiring attention. Its HTTP API exposes current fleet information and alert-management operations to external clients.

The product must distinguish a robot's *registered lifecycle*, its *most recent reported operating mode*, its *battery health*, and the *freshness of its telemetry*.

## 2. Problem and product goals

Periodic device reports can be delayed, duplicated, delivered out of order, or stop arriving altogether. A fleet manager should still be able to determine what was last reported, when the information was received, which robots need attention, and whether an alert has been acknowledged or resolved.

The product must:

1. Identify robots and their operational sites.
2. Maintain reliable current robot state without losing useful historical observations.
3. Show each robot's activity, position, battery health, and whether its data is fresh.
4. Prevent duplicate or late reports from causing misleading operational changes.
5. Track low-battery alerts with a simple lifecycle and a record of each change.

## 3. Users and external interfaces

| Actor | Intended interaction |
|---|---|
| Fleet operator | Read fleet and robot state; acknowledge alerts. |
| Operations supervisor | Inspect active and historical alerts, and review robots by site. |
| Telemetry-producing client | Send observations conforming to the ingest API. |
| Fleet-management API client | Consume documented endpoints for current fleet state and historical data. |

This project implements only the server side of the challenge; the robot simulator and the user interface are not built.

## 4. Business capabilities and requirements

### 4.1 Robot registry and lifecycle

- **REG-01** — Each robot has a unique, stable identifier. Repeating a registration with the same data succeeds without changing the robot; registering an existing identifier with different data is a conflict.
- **REG-02** — Registration requires an existing operational site. A robot has exactly one site assignment; **site reassignment is not supported** in this scope.
- **REG-03** — A newly registered robot starts `ACTIVE`, with unknown measured state until its first current observation.
- **REG-04** — An operator can change lifecycle between `ACTIVE` and `INACTIVE`. Setting the already-current state is idempotent.
- **REG-05** — New observations from unregistered or `INACTIVE` robots are not recorded. Telemetry is idempotent: resending an observation that was already recorded always returns duplicate, even if the robot has been deactivated since.
- **REG-06** — Deactivation retains the robot's last known measurements and recorded history; it closes any unresolved low-battery alert with closure reason `ROBOT_DEACTIVATED`. This is **not** represented as battery recovery.
- **REG-07** — Reactivating a robot does not change its battery health or open an alert by itself. The robot's next applied observation decides: if the battery is still low, a new alert opens; if it has recovered, no alert opens.

**Business information:** robot identifier, site identifier, lifecycle state, registration time, last known observation.

### 4.2 Telemetry intake and history

An observation represents a robot's report at a specified device time; receipt time is recorded by the backend. Each observation has an ID that is unique for that robot.

- **TEL-01** — Each observation includes an observation ID, device observation time, battery percentage, site-local two-dimensional position, and reported operating mode.
- **TEL-02** — Battery percentage is within `0–100`, coordinates are finite numeric values, and operating mode is one of `IDLE`, `WORKING`, `CHARGING`, or `ERROR`.
- **TEL-03** — Position values are measured in metres relative to the robot's assigned site's local coordinate system. No map rendering or floor-plan representation is defined.
- **TEL-04** — A previously unseen valid observation newer than the current observation is **applied**: it is recorded in history, becomes latest state, and may change battery health. Alerts follow from battery health (§4.4). When a robot has no current observation, its first valid observation is applied.
- **TEL-05** — A previously unseen valid observation at or before the current observation time is **superseded**: it is recorded in history but does not change current state, connectivity's last-applied receipt time, health, or alerts. There is no limit on how old an observation can be; old observations are still recorded.
- **TEL-06** — An exact replay of a previously recorded `(robot ID, observation ID, payload)` is **duplicate**: it makes no additional change. Reuse of the same observation ID with different content is a **conflict**.
- **TEL-07** — Concurrent arrivals must never allow an older observation to overwrite newer current state. Observation ordering uses device-reported observation time.
- **TEL-08** — The system retains device `observed_at` and backend `received_at` separately.
- **TEL-09** — An observation whose time is more than 5 seconds ahead of the server's time is rejected. Otherwise one wrong future timestamp would block all later real observations.
- **TEL-10** — Each accepted observation returns one of three outcomes: applied, superseded or duplicate. All three are successful: the data is safely stored, so the robot does not need to resend.
- **TEL-11** — An observation is rejected and not saved when: the robot is not registered, the robot is INACTIVE, its ID was already used for different data, its values are invalid, or its time is more than 5 seconds in the future. Each case returns its own error code. (A resend of an already-saved observation still returns duplicate.)

**Ordering assumption:** The server orders snapshots by the robot's timestamp, assuming the robot's clock is correct. If a robot's clock is set backwards (for example after a reboot), its new snapshots look older and are not applied until its clock catches up. This is a known limitation.

**Acceptance scenario:** Observations with times `10:00:02`, then `10:00:01` and distinct IDs are both recorded. Only `10:00:02` determines latest battery, position, mode and health. Repeating the second observation yields `duplicate` rather than another history row.

### 4.3 Operational health and connectivity

- **HLT-01** — Battery health is `UNKNOWN` until a current observation is applied; after that it is `NORMAL` or `LOW`.
- **HLT-02** — When `UNKNOWN` or `NORMAL`, a current battery reading **below 20%** enters `LOW`.
- **HLT-03** — When `LOW`, battery health remains `LOW` until a current battery reading is **at least 25%**. This hysteresis prevents repeated toggling.
- **HLT-04** — Connectivity is `UNKNOWN` until current telemetry has been applied, `ONLINE` when the last *applied* observation was received no more than **10 seconds** ago, and `STALE` after that. Connectivity reflects backend receipt of current reports and does not depend on the device clock; the age of the measurement itself is conveyed by the separately exposed observation time.
- **HLT-05** — `STALE` means that current reports have not arrived recently; it does not prove physical disconnection or failure.
- **HLT-06** — A robot's lifecycle (ACTIVE/INACTIVE), connectivity (ONLINE/STALE), operating mode, and battery health are four separate facts. None is derived from another, and any combination is possible (for example, an INACTIVE robot can still show a LOW last-known battery).
- **HLT-07** — Low-battery assessment in this scope depends only on measured battery and the previous battery-health state; a reported `CHARGING` mode does not automatically suppress a low-battery condition.
- **HLT-08** — Operating mode is shown but does not trigger anything. A robot reporting ERROR appears with ERROR in its state, but no alert is created; the only alert type in this scope is low battery.

**Battery transition rules:**

| Previous health | New current battery | New health |
|---|---|---|
| UNKNOWN / NORMAL | Below 20% | LOW |
| UNKNOWN / NORMAL | 20% or above | NORMAL |
| LOW | Below 25% | LOW |
| LOW | 25% or above | NORMAL |

**Acceptance scenario:** `60% → 18% → 22% → 25%` results in `NORMAL → LOW → LOW → NORMAL`. A robot becomes `STALE` after the freshness threshold passes even if no new HTTP request arrives.

### 4.4 Low-battery alert management

- **ALT-01** — When an applied observation leaves an active robot's battery health `LOW` and the robot has no unresolved low-battery alert, an alert opens in the same transaction that applies the observation. After reactivation, this waits for the robot's next applied observation (REG-07).
- **ALT-02** — A robot can have **at most one unresolved** low-battery alert, whether `OPEN` or `ACKNOWLEDGED`.
- **ALT-03** — An alert begins in `OPEN`. Acknowledgement changes it to `ACKNOWLEDGED` and does not indicate battery recovery.
- **ALT-04** — Repeating acknowledgement of an already `ACKNOWLEDGED` alert is successful and idempotent, with no further state change.
- **ALT-05** — When an applied observation returns the robot's battery health to `NORMAL`, its unresolved alert becomes `RESOLVED` in the same transaction, regardless of whether it was acknowledged.
- **ALT-06** — Deactivating the robot changes an unresolved alert to `CLOSED` with reason `ROBOT_DEACTIVATED`. `CLOSED` is distinct from `RESOLVED` because no recovery was observed.
- **ALT-07** — `RESOLVED` and `CLOSED` are terminal alert states; they cannot be acknowledged, resolved or closed again. History is retained. A later low-battery episode opens a new `OPEN` alert; it never inherits an earlier acknowledgement.
- **ALT-08** — The acknowledgement timestamp is retained. No verified operator identity is claimed because identity and access management are outside this project's scope.

**Alert transition rules:**

| Previous status | Trigger | New status / outcome |
|---|---|---|
| No unresolved alert | Applied observation leaves health LOW, robot ACTIVE | OPEN |
| OPEN | Acknowledge | ACKNOWLEDGED |
| ACKNOWLEDGED | Acknowledge | ACKNOWLEDGED (idempotent) |
| OPEN / ACKNOWLEDGED | Applied observation returns health to NORMAL | RESOLVED |
| OPEN / ACKNOWLEDGED | Robot deactivated | CLOSED; reason ROBOT_DEACTIVATED |
| RESOLVED / CLOSED | Acknowledge | Conflict; no change |

**Acceptance scenario:** Battery `60 → 18 → 15 → 30%` opens exactly one alert after the 18% reading, keeps it unresolved at 15%, and resolves it after the 30% reading. A short episode is not lost: `60 → 18 → 30` sent within a second leaves one alert that opened and resolved. After an acknowledged alert, `30 → 15` resolves it and opens a new, unacknowledged alert. Deactivation while the alert is unresolved closes it with the explicit deactivation reason rather than calling it recovered.

### 4.5 Fleet visibility

- **VIS-01** — Clients can list registered robots and query an individual robot's current state: site, lifecycle, latest measured battery, position, operating mode, battery health, connectivity and observation/receipt times.
- **VIS-02** — Clients can filter robots by site and retrieve a fleet-level summary for all sites or a selected site.
- **VIS-03** — Summary data reports counts of registered, active and inactive robots, unresolved alerts, and, **among active robots only**, online, stale and low-battery robots. Inactive robots keep their last-known values but do not inflate operational attention counts. An active robot without an applied observation has `UNKNOWN` connectivity, so online and stale counts need not sum to the active count.
- **VIS-04** — Clients can list unresolved alerts and review terminal alert history.
- **VIS-05** — Clients can retrieve a bounded window of a robot's recorded observations, distinguishing applied from superseded observations.
- **VIS-06** — Before observations are available, unknown fields are represented explicitly rather than replaced by fabricated values.

## 5. Main business workflows

**A — Register and monitor:** A site exists; a robot is registered active; the first new observation applies current battery, position, mode, and health; subsequent reports update state.

**B — Handle low battery:** A current report crosses below 20% and an alert opens with it; a manager acknowledges it; further low-battery reports do not duplicate it; a reading of at least 25% resolves it.

**C — Buffered delivery:** The robot sends snapshot A (10:00:01), then B (10:00:02), but B arrives first. 
B is applied and becomes the current state. 
A arrives late. It is saved in history but does not change the current state or alerts.
If either snapshot is sent again, the server returns duplicate.

**D — Robot stops reporting:** No new snapshot is applied for more than 10 seconds.
The robot shows as STALE.
Its last known battery, position and mode stay visible, with the time they were reported.

**E — Deactivate and reactivate:** A robot has 15% battery and an open low-battery alert.
An operator deactivates it. The alert is closed (reason: robot deactivated). The last reading stays visible.
Later, the operator reactivates it. Nothing changes until a new snapshot arrives.
The next snapshot decides: still below 25% → a new alert opens; 25% or more → no alert.

### How robots report: real-world situations

Robots do not send a message when something happens. While powered on, each robot sends the same kind of snapshot (an observation) about once per second. The system learns what happened from what changes between snapshots, or from snapshots stopping.

| What happens to the robot | What the server sees | Result | Rules |
|---|---|---|---|
| Normal work | Snapshot every second, mode `WORKING` | Current state updates; `ONLINE` | TEL-04, HLT-04 |
| Put on the charger | Mode `CHARGING`, battery rising | Low-battery alert resolves at ≥ 25% | HLT-03, ALT-05 |
| Battery drains | Battery value dropping | Below 20% → `LOW` and an alert | HLT-02, ALT-01 |
| Something breaks | Mode `ERROR` | Visible in state; no alert | HLT-08 |
| Turned off, crashed, or lost network | Nothing | After 10 s without data → `STALE` | HLT-04, HLT-05 |
| Turned back on | Snapshots resume | Next applied snapshot → `ONLINE` | HLT-04 |
| Resends a snapshot after a lost response | Same observation ID again | `duplicate`; nothing changes | TEL-06 |
| Sends buffered snapshots late | Older observation time | Stored as `superseded`; current state unchanged | TEL-05 |
| Robot clock runs ahead by more than 5 s | Observation time in the future | Rejected | TEL-09 |
| Keeps sending after an operator deactivated it | New snapshots from an `INACTIVE` robot | Rejected | REG-05 |

**Deactivation** is an operator action through the API, not a robot message. The robot is not told and may keep sending; its new snapshots are rejected.

## 6. Business data glossary

| Concept | Business meaning |
|---|---|
| Site | Known operational location and reference frame for positions. |
| Robot | Registered entity with lifecycle, site and most recent applied report. |
| Observation | Uniquely identified report, immutable after recording. |
| Applied observation | Report that advanced the robot's current state. |
| Superseded observation | Valid historical report that arrived too late to become current. |
| Operating mode | Device-reported activity, not an independently verified operational command. |
| Battery health | Derived business classification with hysteresis. |
| Connectivity | Freshness of the last *applied* report's backend receipt time. |
| Alert | Tracked low-battery condition that may be acknowledged and later resolved or closed. |

## 7. Quality requirements

- **Consistency:** Applying an observation, its history record and the robot's new state happen together or not at all.
- **Alert consistency:** An alert opens, resolves or closes in the same transaction as the applied observation or deactivation that causes it, or not at all.
- **Idempotency:** Repeated deliveries and acknowledgements must have predictable, side-effect-free outcomes.
- **Ordering:** Concurrent reports cannot regress the current operational view.
- **Interpretability:** The API distinguishes observed time, backend receipt time, and the freshness of current state.
- **Diagnostics:** Invalid data, conflicts and failed operations return consistent, actionable API errors.
- **Operability:** The service exposes basic health checks, supports configuration and graceful shutdown, and can start from a documented local environment.
- **Scope discipline:** There are no implied guarantees of production-scale throughput or high availability without measured requirements.

## 8. Project scope and explicit exclusions

**Included:** Backend API, domain behavior, PostgreSQL persistence, deterministic tests, configuration. An API client or simple HTTP fixtures may be used to exercise telemetry ingestion.

**Excluded from implementation:**

- Robot hardware integration, device-control software and a standalone robot simulator.
- Frontend, dashboard or other graphical presentation layer.
- Identity, authentication and authorization for robots and operators (including device credentials and user sessions).
- Notification delivery, missions and route planning, realtime streaming transport, and historical prediction models.
- Administrative site-management endpoints; sites are provisioned as reference data.

The lack of authentication and authorization is a **deliberate project constraint, not a recommended security design**. The API is intended for a local or otherwise isolated development environment and must not be exposed publicly with unrestricted write access.

## 9. Policy assumptions

The business rules above use explicit assumptions: low below **20%**, recovery at **25%**, stale after **10 seconds** without applied telemetry, maximum **5 seconds** future-clock tolerance, trustworthy device ordering timestamps, and site-local two-dimensional coordinates in metres. They define the behavior of this product and are not asserted to be policies of any particular real-world robot operator.

## 10. Acceptance criteria

The product is accepted when all described workflows pass automated domain and integration tests, state-changing API operations produce the documented results, simultaneous telemetry cannot regress state, history preserves both applied and superseded observations, and alert lifecycle behavior agrees with the transition table.