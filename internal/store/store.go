package store

import (
	"context"
	"database/sql"
	"errors"
)

// Errors returned by the store so the HTTP layer does not need to know about SQL.
var (
	ErrNoStock  = errors.New("product unavailable or insufficient stock")
	ErrNotFound = errors.New("order not found")
)

// Order is a single line of an order: one product and a quantity.
type Order struct {
	ID        int64 `json:"id,omitempty"`
	ProductID int64 `json:"product_id"`
	Quantity  int64 `json:"quantity"`
}

// Store talks to PostgreSQL.
type Store struct {
	db *sql.DB
}

// New wraps an existing connection pool.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateOrder reserves stock and records the order in one statement.
//
// The stock check, the decrement and the insert happen inside a single
// statement, so two concurrent requests can never both see the same stock and
// there is no BEGIN/COMMIT round trip.
func (s *Store) CreateOrder(ctx context.Context, productID, quantity int64) (Order, error) {
	var result Order
	err := s.db.QueryRowContext(ctx, `
		WITH updated AS (
			UPDATE products SET stock = stock - $1
			WHERE id = $2 AND stock >= $1
			RETURNING id
		)
		INSERT INTO orders (product_id, quantity)
		SELECT id, $1 FROM updated
		RETURNING id, product_id, quantity
	`, quantity, productID).Scan(&result.ID, &result.ProductID, &result.Quantity)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNoStock
	}
	if err != nil {
		return Order{}, err
	}
	return result, nil
}

// Order returns a single order by id.
func (s *Store) Order(ctx context.Context, id int64) (Order, error) {
	var result Order
	err := s.db.QueryRowContext(ctx,
		`SELECT id, product_id, quantity FROM orders WHERE id = $1`, id).
		Scan(&result.ID, &result.ProductID, &result.Quantity)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	return result, nil
}

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
