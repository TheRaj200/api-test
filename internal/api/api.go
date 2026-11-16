// Package api wires the HTTP routes to the store.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/TheRaj200/api-test/internal/store"
)

const (
	queryTimeout = 3 * time.Second
	readyTimeout = 2 * time.Second
	maxBodyBytes = 4096
)

// Store is the persistence the handlers need. *store.Store implements it, and
// tests can substitute a stub.
type Store interface {
	CreateOrder(ctx context.Context, productID, quantity int64) (store.Order, error)
	Order(ctx context.Context, id int64) (store.Order, error)
	Ping(ctx context.Context) error
}

// New returns the HTTP handler for the order API.
func New(st Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// /readyz is for load balancers and deploys: it reports whether the
	// database is reachable, unlike /health which only proves the process runs.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := st.Ping(ctx); err != nil {
			slog.Warn("readiness check failed", "err", err)
			fail(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ProductID int64 `json:"product_id"`
			Quantity  int64 `json:"quantity"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.ProductID <= 0 || input.Quantity <= 0 {
			fail(w, http.StatusBadRequest, "invalid order")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			fail(w, http.StatusBadRequest, "expected one JSON object")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
		defer cancel()
		order, err := st.CreateOrder(ctx, input.ProductID, input.Quantity)
		if errors.Is(err, store.ErrNoStock) {
			fail(w, http.StatusConflict, store.ErrNoStock.Error())
			return
		}
		if err != nil {
			databaseError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, order)
	})

	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			fail(w, http.StatusBadRequest, "invalid order id")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
		defer cancel()
		order, err := st.Order(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, store.ErrNotFound.Error())
			return
		}
		if err != nil {
			databaseError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, order)
	})

	return mux
}

func databaseError(w http.ResponseWriter, err error) {
	slog.Error("database request failed", "err", err)
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
