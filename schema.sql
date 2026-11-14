CREATE TABLE IF NOT EXISTS products (
    id bigint PRIMARY KEY,
    stock bigint NOT NULL CHECK (stock >= 0)
);

CREATE TABLE IF NOT EXISTS orders (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id bigint NOT NULL REFERENCES products(id),
    quantity bigint NOT NULL CHECK (quantity > 0)
);

INSERT INTO products (id, stock) VALUES (1, 100) ON CONFLICT DO NOTHING;
