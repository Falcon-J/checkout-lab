CREATE TABLE inventory (
 sku text PRIMARY KEY CHECK (sku ~ '^[a-z0-9][a-z0-9_-]{0,63}$'),
 available integer NOT NULL CHECK (available >= 0)
);
CREATE TABLE reservations (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 request_key text NOT NULL UNIQUE CHECK (request_key ~ '^[A-Za-z0-9_-]{16,128}$'),
 sku text NOT NULL REFERENCES inventory(sku),
 quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 10),
 status text NOT NULL CHECK (status IN ('held','expired')),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX reservations_due ON reservations(sku,id) WHERE status='held';
INSERT INTO inventory(sku,available) VALUES ('book-go',10);
