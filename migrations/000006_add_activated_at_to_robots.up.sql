ALTER TABLE robots ADD COLUMN activated_at timestamptz;
UPDATE robots SET activated_at = created_at;
ALTER TABLE robots ALTER COLUMN activated_at SET NOT NULL;
