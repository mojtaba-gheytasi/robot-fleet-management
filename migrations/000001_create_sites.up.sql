-- Sites are reference data: provisioned here, never through the API.
CREATE TABLE sites (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL
);

INSERT INTO sites (id, name, created_at) VALUES
    ('site-north', 'North Warehouse', now()),
    ('site-south', 'South Warehouse', now());
