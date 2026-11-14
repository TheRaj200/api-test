package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

type order struct {
	ID        int64 `json:"id,omitempty"`
	ProductID int64 `json:"product_id"`
	Quantity  int64 `json:"quantity"`
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(16)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              ":8080",
		Handler:           routes(db),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      6 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func routes(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ProductID int64 `json:"product_id"`
			Quantity  int64 `json:"quantity"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.ProductID <= 0 || input.Quantity <= 0 {
			fail(w, http.StatusBadRequest, "invalid order")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			fail(w, http.StatusBadRequest, "expected one JSON object")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			databaseError(w, err)
			return
		}
		defer tx.Rollback()

		var productID int64
		err = tx.QueryRowContext(ctx, `
			UPDATE products SET stock = stock - $1
			WHERE id = $2 AND stock >= $1 RETURNING id
		`, input.Quantity, input.ProductID).Scan(&productID)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, http.StatusConflict, "product unavailable or insufficient stock")
			return
		}
		if err != nil {
			databaseError(w, err)
			return
		}
		result := order{ProductID: productID, Quantity: input.Quantity}
		err = tx.QueryRowContext(ctx, `
			INSERT INTO orders (product_id, quantity) VALUES ($1, $2) RETURNING id
		`, result.ProductID, result.Quantity).Scan(&result.ID)
		if err != nil {
			databaseError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			databaseError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			fail(w, http.StatusBadRequest, "invalid order id")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var result order
		err = db.QueryRowContext(ctx, "SELECT id, product_id, quantity FROM orders WHERE id = $1", id).
			Scan(&result.ID, &result.ProductID, &result.Quantity)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, http.StatusNotFound, "order not found")
			return
		}
		if err != nil {
			databaseError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	return mux
}

func databaseError(w http.ResponseWriter, err error) {
	log.Printf("database request failed: %v", err)
	if errors.Is(err, context.DeadlineExceeded) {
		fail(w, http.StatusServiceUnavailable, "database timeout")
		return
	}
	fail(w, http.StatusInternalServerError, "database request failed")
}

func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
