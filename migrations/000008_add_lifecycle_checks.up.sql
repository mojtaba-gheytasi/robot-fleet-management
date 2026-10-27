-- These mirror the Alert and Robot validation in the domain, so a direct SQL write
-- cannot store a state the domain would reject. Comparisons with NULL pass.
ALTER TABLE alerts
    ADD CONSTRAINT alerts_lifecycle_shape CHECK (
        CASE status
            WHEN 'OPEN'         THEN acknowledged_at IS NULL AND ended_at IS NULL
            WHEN 'ACKNOWLEDGED' THEN acknowledged_at IS NOT NULL AND ended_at IS NULL
            ELSE ended_at IS NOT NULL
        END
    ),
    ADD CONSTRAINT alerts_chronology CHECK (
        acknowledged_at >= opened_at
        AND ended_at >= opened_at
        AND acknowledged_at <= ended_at
    );

ALTER TABLE robots
    ADD CONSTRAINT robots_activation_after_registration CHECK (activated_at >= created_at);
