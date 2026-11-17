-- Before/after snapshot for stock reconciliation.
SELECT 'orders_test_total,' || count(*) FROM orders WHERE product_id BETWEEN 9000 AND 9999;
SELECT 'orders_all,' || count(*) FROM orders;
SELECT 'orders_' || id || ',' || (SELECT count(*) FROM orders o WHERE o.product_id = p.id)
FROM products p WHERE id BETWEEN 9000 AND 9999 ORDER BY id;
SELECT 'stock_' || id || ',' || stock FROM products WHERE id BETWEEN 9000 AND 9999 ORDER BY id;
SELECT 'seq_orders,' || last_value FROM orders_id_seq;
