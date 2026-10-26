CREATE TABLE robots (
    id         text PRIMARY KEY,
    site_id    text NOT NULL CONSTRAINT robots_site_id_fkey REFERENCES sites (id),
    lifecycle  text NOT NULL CHECK (lifecycle IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL
);
