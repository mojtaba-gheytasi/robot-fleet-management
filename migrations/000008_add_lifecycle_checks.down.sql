ALTER TABLE robots DROP CONSTRAINT robots_activation_after_registration;

ALTER TABLE alerts
    DROP CONSTRAINT alerts_chronology,
    DROP CONSTRAINT alerts_lifecycle_shape;
