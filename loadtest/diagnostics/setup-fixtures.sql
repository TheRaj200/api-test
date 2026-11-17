-- Isolated diagnostic fixtures. Demo product 1 is never touched.
-- 9001-9004: spread-load products, stock large enough for every measured run.
-- 9201: burst product, always reset to exactly 100 units.
DELETE FROM orders WHERE product_id BETWEEN 9000 AND 9999;

INSERT INTO products (id, stock) VALUES
    (9001, 5000000),
    (9002, 5000000),
    (9003, 5000000),
    (9004, 5000000),
    (9201, 100)
ON CONFLICT (id) DO UPDATE SET stock = EXCLUDED.stock;
