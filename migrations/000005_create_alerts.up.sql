CREATE TABLE alerts (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    robot_id        text        NOT NULL REFERENCES robots (id),
    kind            text        NOT NULL CHECK (kind IN ('LOW_BATTERY')),
    status          text        NOT NULL CHECK (status IN ('OPEN', 'ACKNOWLEDGED', 'RESOLVED', 'CLOSED')),
    opened_at       timestamptz NOT NULL,
    acknowledged_at timestamptz,
    ended_at        timestamptz,
    closure_reason  text        CHECK (closure_reason IN ('ROBOT_DEACTIVATED')),
    CHECK ((status = 'CLOSED') = (closure_reason IS NOT NULL))
);

CREATE UNIQUE INDEX alerts_one_unresolved_idx
    ON alerts (robot_id, kind)
    WHERE status IN ('OPEN', 'ACKNOWLEDGED');
