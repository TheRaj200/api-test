-- Clean slate: used before the 500-concurrent correctness check.
TRUNCATE orders RESTART IDENTITY;
UPDATE products SET stock = 100 WHERE id = 1;
