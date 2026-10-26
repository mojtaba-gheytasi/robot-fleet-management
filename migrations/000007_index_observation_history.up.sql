CREATE INDEX telemetry_observations_history_idx
    ON telemetry_observations (robot_id, observed_at DESC);
