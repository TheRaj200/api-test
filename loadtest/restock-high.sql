-- Large stock so the write stress test measures DB throughput, not stock depletion.
UPDATE products SET stock = 10000000 WHERE id = 1;
