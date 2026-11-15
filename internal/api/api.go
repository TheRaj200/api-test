// Package api wires the HTTP routes to the store.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/TheRaj200/api-test/internal/store"
)

const queryTimeout = 3 * time.Second

// New returns the HTTP handler for the order API.
func New(st *store.Store) http.Handler {
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
