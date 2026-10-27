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
| Low-battery episode | A run of applied observations that leave an active robot `LOW`. It starts with the first such observation and ends at recovery or deactivation. Each episode has its own alert |
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

**Valid state** (checked on construction and when loaded from storage): activation is not before registration; with no applied observation, health is `UNKNOWN`; with one, health fits its battery under the table below (below 20% always `LOW`, 25% or more always `NORMAL`, either in between). An inactive or reactivated robot keeps its last applied observation.

**Battery health transitions** (on each applied observation):

| Current health | Battery reading | New health |
|---|---|---|
| `UNKNOWN` / `NORMAL` | below 20% | `LOW` |
| `UNKNOWN` / `NORMAL` | 20% or more | `NORMAL` |
| `LOW` | below 25% | `LOW` |
| `LOW` | 25% or more | `NORMAL` |

Observation history is not part of the aggregate. It is stored separately, so the aggregate stays small however long the robot runs.

### Alert aggregate

**Owns:** robot reference, status (`OPEN`, `ACKNOWLEDGED`, `RESOLVED`, `CLOSED`), timestamps, closure reason. One alert is one low-battery episode.

**Behaviour:** `NewLowBatteryAlert` opens it, `Acknowledge` is driven by an operator, `Resolve` and `Close` end it. Every transition is a method on the aggregate; SQL only stores the result.

**Rules:**

- Acknowledge only from `OPEN`; repeating it is a no-op and keeps the first time.
- Resolve or close only an unresolved alert. Both keep any acknowledgement.
- `RESOLVED` and `CLOSED` are final: nothing changes them again.
- The fields always fit the status:

| Status | `acknowledged_at` | `ended_at` | `closure_reason` |
|---|---|---|---|
| `OPEN` | none | none | none |
| `ACKNOWLEDGED` | set | none | none |
| `RESOLVED` | optional | set | none |
| `CLOSED` | optional | set | `ROBOT_DEACTIVATED` |

- Times never run backwards: acknowledged and ended are not before opened, and acknowledged is not after ended.

**Status transitions:**

| Current status | Trigger | New status |
|---|---|---|
| none | Applied observation leaves an active robot `LOW` | `OPEN` |
| `OPEN` | Acknowledge | `ACKNOWLEDGED` |
| `ACKNOWLEDGED` | Acknowledge | `ACKNOWLEDGED` (no-op) |
| `OPEN` / `ACKNOWLEDGED` | Applied observation leaves the robot `NORMAL` | `RESOLVED` |
| `OPEN` / `ACKNOWLEDGED` | Robot deactivated | `CLOSED` |
| `RESOLVED` / `CLOSED` | Acknowledge, resolve or close | Rejected; no change |

**Timestamps** are server times from one injected clock, never the robot's `observed_at`. Each use case reads the clock once, after it holds every lock it needs, so stored times follow the order the changes were made.

- `opened_at`: the `received_at` of the observation that started the episode.
- `acknowledged_at`: when the operator's acknowledgement was recorded.
- `ended_at`: for `RESOLVED`, the `received_at` of the observation that recovered; for `CLOSED`, when the deactivation was recorded.

### Why two aggregates

Each aggregate protects rules that need only its own data. Robot: telemetry only when active, only newer observations become current, the 20% / 25% rule. Alert: its status machine and timestamps. An alert also has its own identity and history: operators list and acknowledge alerts directly, and a robot has many over its life.

In one aggregate, an operator's acknowledgement would lock the robot and wait for its telemetry, which arrives every second. Kept apart, acknowledging locks only the alert. Robot transactions touch at most one alert, the unresolved one, and never load alert history.

### The rule across them

**An active robot that is `LOW` has exactly one unresolved alert; any other robot has none.** Neither aggregate can hold this alone, so two application use cases keep it, each in one transaction under the robot's row lock:

- **RecordTelemetry**, after each applied observation. If the robot is `LOW` and has no unresolved alert, open one. If it is not `LOW` and has one, resolve it. Duplicate, superseded and rejected observations change no alert.
- **DeactivateRobot.** Close the unresolved alert with `ROBOT_DEACTIVATED`.

Robot decides the battery health and Alert decides how it changes. The use case only connects them and commits both, or neither.

**Why no episode is missed.** The rule runs on every applied observation, not on a timer, so `60 → 18 → 30` opens and resolves an alert even within one second. A recovery always resolves the alert of its own episode, so a new episode starts with a new `OPEN` alert and never inherits an earlier acknowledgement. The rule compares state, not "health changed", so the first `LOW` reading after reactivation opens a new alert even though health was already `LOW`. Reactivation alone opens nothing.

**Concurrency.** Locks are always taken robot row first, then alert row. Acknowledge takes only the alert row, so locks cannot form a cycle. Telemetry and deactivation of one robot take turns on its row lock. An acknowledgement and a resolution take turns on the alert row: whichever commits first wins, and the other sees its result (the resolution keeps the acknowledgement, or the acknowledgement gets `ALERT_TERMINAL`). A partial unique index also guarantees at most one unresolved alert per robot in the database.

### Site and value objects

**Site** is reference data (ID and name) with no behaviour, so it is not an aggregate.

**Value objects:** `Observation` (its ID is unique per robot), `BatteryLevel` (0–100), `Position` (finite x, y in metres, relative to the site), `OperatingMode` (`IDLE`, `WORKING`, `CHARGING`, `ERROR`), and typed IDs.

## 6. Trade-offs

| Decision | Alternative | Why |
|---|---|---|
| Change alerts in the transaction of the applied observation or deactivation that causes them | A background check of current health every few seconds | A periodic check sees only the latest health, so a short episode is lost, and two episodes between checks merge into one alert. Cost: one lookup on a small index per new observation, and a short alert-row lock. |
| Load the one unresolved alert and change it through the aggregate | Set-based SQL per rule | Alert owns every transition, and each transaction touches at most one alert, so loading it is cheap. |
| Keep late observations as history | Reject them | Buffered data from a robot that was offline is real data |
| Store battery health, compute connectivity on read | Store both | Hysteresis needs the previous value; connectivity depends on the current time |
| Close alerts on deactivation | Leave them open | An inactive robot sends no telemetry, so its alert could never resolve |
| Order observations by the robot's timestamp | Per-robot sequence number | Simple and needs no extra protocol. Known limitation: a robot clock set backwards freezes current state until it catches up. |
