ALTER TABLE robots
    ADD COLUMN battery_health text NOT NULL DEFAULT 'UNKNOWN'
    CHECK (battery_health IN ('UNKNOWN', 'NORMAL', 'LOW'));
