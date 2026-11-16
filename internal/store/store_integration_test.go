package store_test

import (
	"context"
	"database/sql"
	"errors"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/TheRaj200/api-test/internal/store"
)

// These tests need a real database and are skipped unless TEST_DATABASE_URL is
// set, for example:
//
//	TEST_DATABASE_URL="postgres://postgres:pw@localhost:5432/api_test?sslmode=disable" go test ./...
func openTestStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return store.New(db), db
}

func TestCreateOrderAndRead(t *testing.T) {
	st, db := openTestStore(t)
	productID := newProduct(t, db, 5)

	ctx := context.Background()
	created, err := st.CreateOrder(ctx, productID, 2)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if created.ID == 0 || created.ProductID != productID || created.Quantity != 2 {
		t.Fatalf("created = %+v", created)
	}
	if stock := productStock(t, db, productID); stock != 3 {
		t.Fatalf("stock = %d, want 3", stock)
	}

	read, err := st.Order(ctx, created.ID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if read != created {
		t.Fatalf("read = %+v, want %+v", read, created)
	}

	if _, err := st.CreateOrder(ctx, productID, 10); !errors.Is(err, store.ErrNoStock) {
		t.Fatalf("oversized order err = %v, want ErrNoStock", err)
	}
	if stock := productStock(t, db, productID); stock != 3 {
		t.Fatalf("stock after rejected order = %d, want 3", stock)
	}

	if _, err := st.Order(ctx, -1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing order err = %v, want ErrNotFound", err)
	}
}

// TestCreateOrderRace checks the invariant that the CTE statement guarantees:
// concurrent requests can never sell more units than the stock.
func TestCreateOrderRace(t *testing.T) {
	st, db := openTestStore(t)
	productID := newProduct(t, db, 10)

	const attempts = 50
	var created, outOfStock atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.CreateOrder(context.Background(), productID, 1)
			switch {
			case err == nil:
				created.Add(1)
			case errors.Is(err, store.ErrNoStock):
				outOfStock.Add(1)
			default:
				t.Errorf("CreateOrder: %v", err)
			}
		}()
	}
	wg.Wait()

	if created.Load() != 10 || outOfStock.Load() != attempts-10 {
		t.Fatalf("created = %d, out of stock = %d; want 10 and %d", created.Load(), outOfStock.Load(), attempts-10)
	}
	if stock := productStock(t, db, productID); stock != 0 {
		t.Fatalf("stock = %d, want 0", stock)
	}
}

func newProduct(t *testing.T, db *sql.DB, stock int64) int64 {
	t.Helper()
	id := rand.Int63n(1 << 40)
	if _, err := db.Exec(`INSERT INTO products (id, stock) VALUES ($1, $2)`, id, stock); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM orders WHERE product_id = $1`, id)
		_, _ = db.Exec(`DELETE FROM products WHERE id = $1`, id)
	})
	return id
}

func productStock(t *testing.T, db *sql.DB, id int64) int64 {
	t.Helper()
	var stock int64
	if err := db.QueryRow(`SELECT stock FROM products WHERE id = $1`, id).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	return stock
}
