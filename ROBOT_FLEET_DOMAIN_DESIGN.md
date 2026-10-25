# Robot Fleet Management — Domain Design

The [PRD](./ROBOT_FLEET_PRD.md) defines what the system must do; this document defines how the domain is modelled to do it.

## 1. The business problem

A fleet manager needs to know, at any moment, what each robot last reported, whether that report is recent, and which robots need attention. Robots report over an unreliable network, so reports can be late, repeated or missing. The operator must still be able to trust what the system shows.

## 2. Subdomains and where the effort goes

| Subdomain | Type | Effort |
|---|---|---|
| Fleet operational awareness: current state, battery health, connectivity, low-battery alerts | Core | Rich domain model: aggregates, invariants, state transitions, thorough tests |
| Robot registry and sites | Supporting | Simplest thing that works: a site is reference data; registration is a constructor and an insert |
| Identity and access | Generic | Out of scope |

Robot lifecycle (`ACTIVE`/`INACTIVE`) belongs to the registry, but it lives in the Robot aggregate because it decides whether telemetry is accepted and closes open alerts.

## 3. Boundaries

Three separate decisions, each with its own reason:

| Level | Decision | Why |
|---|---|---|
| **Bounded context** (model) | One: Fleet Management, covering the core and supporting subdomains | One language: Robot, Observation and Alert mean the same thing in registry, telemetry, health and alerts. No part needs its own model. |
| **Module** (code) | One: `fleet` | High cohesion: alerts are derived directly from robot state, and registry, telemetry, health and alerts all work on the same robot. Splitting them into modules would add no value, only coordination across module interfaces. |
| **Deployment** (runtime) | One Go service | Nothing needs to scale or be released independently. |

**When to revisit:**

- **New bounded context:** alerting grows into incident management (escalation, technicians, SLAs, alerts from other sources), or mission planning is added.
- **Separate deployment:** telemetry volume needs independent scaling. This can splits the deployment, not the context.
- **Separate modules:** a part grows large while its link to the rest stays narrow, for example alerting with several alert types and escalation, or history with retention and analytics. Then it gets its own module with a small interface, still inside the same bounded context.

## 4. Ubiquitous language

| Term | Meaning |
|---|---|
| Observation | One snapshot from a robot: battery, position, mode, and the time it was measured |
| Applied | A new observation newer than the current one (or the robot's first); it becomes the current state |
| Superseded | A new observation at or before the current one's time; kept in history only |
| Duplicate | The same observation (same robot, ID and content) sent again; nothing changes. Same ID with different content is a conflict |
| Battery health | `UNKNOWN`, `NORMAL` or `LOW`, with a buffer: low below 20%, recovered at 25% |
| Connectivity | `UNKNOWN` before the first applied observation, then `ONLINE` if the last applied one was received within 10 seconds, otherwise `STALE`. Duplicates and superseded observations do not refresh it. It says whether current reports are arriving, not whether the robot is physically off |
| Lifecycle | `ACTIVE` or `INACTIVE`: whether an operator has the robot in service |
| Resolved | Alert ended because the battery recovered |
| Closed | Alert ended because the robot was deactivated; not a recovery |

## 5. Model

### Robot aggregate

**Owns:** site, lifecycle and the time of its last activation, current state (latest applied observation, battery health, receipt time of the latest applied observation).

**Rules:**

- Only an active robot accepts new observations.
- Only an observation newer than the current one changes the current state.
- Battery health changes only through the 20% / 25% rule.

**Behaviour:** `Activate`, `Deactivate`, `ApplyObservation`, `ConnectivityAt(now)`.

**Battery health transitions** (on each applied observation):

| Current health | Battery reading | New health |
|---|---|---|
| `UNKNOWN` / `NORMAL` | below 20% | `LOW` |
| `UNKNOWN` / `NORMAL` | 20% or more | `NORMAL` |
| `LOW` | below 25% | `LOW` |
| `LOW` | 25% or more | `NORMAL` |

Observation history is not part of the aggregate. It is stored separately, so the aggregate stays small however long the robot runs.

### Alert aggregate

**Owns:** robot reference, status (`OPEN`, `ACKNOWLEDGED`, `RESOLVED`, `CLOSED`), timestamps, closure reason.

**Rules:** acknowledge only from `OPEN` (repeating it is a no-op); `RESOLVED` and `CLOSED` are final.

**Behaviour:** `Acknowledge` is the only behaviour driven by a user. Opening, resolving and closing are applied by the alert reconciliation (below).

**Status transitions:**

| Current status | Trigger | New status |
|---|---|---|
| none | Battery `LOW`, robot active (background check) | `OPEN` |
| `OPEN` | Acknowledge | `ACKNOWLEDGED` |
| `ACKNOWLEDGED` | Acknowledge | `ACKNOWLEDGED` (no-op) |
| `OPEN` / `ACKNOWLEDGED` | Battery `NORMAL` (background check) | `RESOLVED` |
| `OPEN` / `ACKNOWLEDGED` | Robot deactivated | `CLOSED` |
| `RESOLVED` / `CLOSED` | Acknowledge | Conflict; no change |

### Why two aggregates

A robot's state changes every second from telemetry; an alert changes rarely, by a human acknowledging it. If alerts lived inside Robot, every acknowledgement would compete with telemetry and every observation would load alert history.

### The rule across them

**Alerts are a projection of robot state.** Every 5 seconds, a background check compares robot state with unresolved alerts in one transaction:

- Active, `LOW`, latest observation received after the robot's last activation, no unresolved alert → open one.
- `NORMAL`, unresolved alert → resolve it.
- `INACTIVE`, unresolved alert → close it. This is a safety net; deactivation already closes the alert immediately.

A robot has at most one unresolved low-battery alert. Neither aggregate can enforce this alone; a partial unique database index guarantees it, even if two checks overlap.

Because `LOW` lasts until the battery charges back to 25%, a low-battery episode always outlasts the 5-second interval, so none is missed. Comparing state, rather than reacting to a "battery became low" event, also covers reactivation, where health may already be `LOW`.

### Site and value objects

**Site** is reference data (ID and name) with no behaviour, so it is not an aggregate.

**Value objects:** `Observation` (its ID is unique per robot), `BatteryLevel` (0–100), `Position` (finite x, y in metres, relative to the site), `OperatingMode` (`IDLE`, `WORKING`, `CHARGING`, `ERROR`), and typed IDs.

## 6. Trade-offs

| Decision | Alternative | Why |
|---|---|---|
| Reconcile alerts in a background check every 5 s | Update alerts in the ingestion transaction | Ingestion only stores facts and answers quickly; alerts are derived and may lag a few seconds. Cost: alerts appear up to 5 s late. |
| Set-based SQL to open, resolve and close alerts | Load and save each Alert aggregate | One statement per rule, cheap at any fleet size. `Acknowledge`, the user-driven rule, stays in the aggregate. |
| Keep late observations as history | Reject them | Buffered data from a robot that was offline is real data |
| Store battery health, compute connectivity on read | Store both | Hysteresis needs the previous value; connectivity depends on the current time |
| Close alerts on deactivation | Leave them open | An inactive robot sends no telemetry, so its alert could never resolve |
| Order observations by the robot's timestamp | Per-robot sequence number | Simple and needs no extra protocol. Known limitation: a robot clock set backwards freezes current state until it catches up. |
