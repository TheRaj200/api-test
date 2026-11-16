-- Schema for the api-test order API.
-- Apply with: psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/schema.sql

CREATE TABLE IF NOT EXISTS products (
    id bigint PRIMARY KEY,
    stock bigint NOT NULL CHECK (stock >= 0)
);

CREATE TABLE IF NOT EXISTS orders (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id bigint NOT NULL REFERENCES products(id),
    quantity bigint NOT NULL CHECK (quantity > 0)
);

-- Speeds up per-product counts such as the README concurrency check.
CREATE INDEX IF NOT EXISTS orders_product_id_idx ON orders (product_id);

INSERT INTO products (id, stock) VALUES (1, 100) ON CONFLICT DO NOTHING;
