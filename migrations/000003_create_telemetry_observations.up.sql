-- Every observation a robot sent, applied or not. Rows are never updated.
CREATE TABLE telemetry_observations (
    robot_id        text             NOT NULL REFERENCES robots (id),
    observation_id  text             NOT NULL,
    observed_at     timestamptz      NOT NULL,
    received_at     timestamptz      NOT NULL,
    battery_percent numeric(5, 2)    NOT NULL CHECK (battery_percent BETWEEN 0 AND 100),
    position_x      double precision NOT NULL,
    position_y      double precision NOT NULL,
    operating_mode  text             NOT NULL CHECK (operating_mode IN ('IDLE', 'WORKING', 'CHARGING', 'ERROR')),
    outcome         text             NOT NULL CHECK (outcome IN ('APPLIED', 'SUPERSEDED')),
    PRIMARY KEY (robot_id, observation_id)
);

-- Finds a robot's current observation in one lookup, however large the table
-- grows: only APPLIED rows, newest first per robot.
CREATE INDEX telemetry_observations_current_idx
    ON telemetry_observations (robot_id, observed_at DESC)
    WHERE outcome = 'APPLIED';
