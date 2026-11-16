package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheRaj200/api-test/internal/store"
)

// stubStore lets the handler tests run without a database.
type stubStore struct {
	createOrder func(ctx context.Context, productID, quantity int64) (store.Order, error)
	order       func(ctx context.Context, id int64) (store.Order, error)
	ping        func(ctx context.Context) error
}

func (s stubStore) CreateOrder(ctx context.Context, productID, quantity int64) (store.Order, error) {
	if s.createOrder == nil {
		return store.Order{}, errors.New("unexpected CreateOrder call")
	}
	return s.createOrder(ctx, productID, quantity)
}

func (s stubStore) Order(ctx context.Context, id int64) (store.Order, error) {
	if s.order == nil {
		return store.Order{}, errors.New("unexpected Order call")
	}
	return s.order(ctx, id)
}

func (s stubStore) Ping(ctx context.Context) error {
	if s.ping == nil {
		return nil
	}
	return s.ping(ctx)
}

func do(t *testing.T, st Store, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	New(st).ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return value
}

func TestHealth(t *testing.T) {
	rec := do(t, stubStore{}, http.MethodGet, "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := decode[map[string]string](t, rec)["status"]; got != "ok" {
		t.Fatalf("status body = %q, want ok", got)
	}
}

func TestReady(t *testing.T) {
	tests := []struct {
		name    string
		ping    func(context.Context) error
		want    int
		wantMsg string
	}{
		{name: "database up", ping: func(context.Context) error { return nil }, want: http.StatusOK, wantMsg: "ready"},
		{name: "database down", ping: func(context.Context) error { return errors.New("down") }, want: http.StatusServiceUnavailable, wantMsg: "database unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, stubStore{ping: tc.ping}, http.MethodGet, "/readyz", "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			body := decode[map[string]string](t, rec)
			if body["status"] != tc.wantMsg && body["error"] != tc.wantMsg {
				t.Fatalf("body = %v, want message %q", body, tc.wantMsg)
			}
		})
	}
}

func TestCreateOrder(t *testing.T) {
	var gotProduct, gotQuantity int64
	st := stubStore{createOrder: func(_ context.Context, productID, quantity int64) (store.Order, error) {
		gotProduct, gotQuantity = productID, quantity
		return store.Order{ID: 7, ProductID: productID, Quantity: quantity}, nil
	}}

	rec := do(t, st, http.MethodPost, "/orders", `{"product_id":1,"quantity":2}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body)
	}
	if gotProduct != 1 || gotQuantity != 2 {
		t.Fatalf("store got product %d quantity %d, want 1 and 2", gotProduct, gotQuantity)
	}
	if got := decode[store.Order](t, rec); got.ID != 7 || got.ProductID != 1 || got.Quantity != 2 {
		t.Fatalf("body = %+v", got)
	}
}

func TestCreateOrderRejectsBadInput(t *testing.T) {
	st := stubStore{createOrder: func(context.Context, int64, int64) (store.Order, error) {
		t.Fatal("store must not be called for invalid input")
		return store.Order{}, nil
	}}

	tests := map[string]string{
		"empty body":       "",
		"malformed json":   `{"product_id":`,
		"unknown field":    `{"product_id":1,"quantity":1,"extra":true}`,
		"zero quantity":    `{"product_id":1,"quantity":0}`,
		"negative id":      `{"product_id":-1,"quantity":1}`,
		"missing product":  `{"quantity":1}`,
		"json array":       `[{"product_id":1,"quantity":1}]`,
		"two objects":      `{"product_id":1,"quantity":1}{"product_id":1,"quantity":1}`,
		"wrong value type": `{"product_id":"1","quantity":1}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			rec := do(t, st, http.MethodPost, "/orders", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
			}
		})
	}
}

func TestCreateOrderErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "no stock", err: store.ErrNoStock, want: http.StatusConflict},
		{name: "timeout", err: context.DeadlineExceeded, want: http.StatusServiceUnavailable},
		{name: "database failure", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := stubStore{createOrder: func(context.Context, int64, int64) (store.Order, error) {
				return store.Order{}, tc.err
			}}
			rec := do(t, st, http.MethodPost, "/orders", `{"product_id":1,"quantity":1}`)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestGetOrder(t *testing.T) {
	tests := []struct {
		name   string
		target string
		order  func(context.Context, int64) (store.Order, error)
		want   int
	}{
		{
			name:   "found",
			target: "/orders/12",
			order: func(context.Context, int64) (store.Order, error) {
				return store.Order{ID: 12, ProductID: 1, Quantity: 3}, nil
			},
			want: http.StatusOK,
		},
		{
			name:   "not found",
			target: "/orders/12",
			order:  func(context.Context, int64) (store.Order, error) { return store.Order{}, store.ErrNotFound },
			want:   http.StatusNotFound,
		},
		{
			name:   "database failure",
			target: "/orders/12",
			order:  func(context.Context, int64) (store.Order, error) { return store.Order{}, errors.New("boom") },
			want:   http.StatusInternalServerError,
		},
		{
			name:   "invalid id",
			target: "/orders/abc",
			want:   http.StatusBadRequest,
		},
		{
			name:   "zero id",
			target: "/orders/0",
			want:   http.StatusBadRequest,
		},
		{
			name:   "negative id",
			target: "/orders/-3",
			want:   http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, stubStore{order: tc.order}, http.MethodGet, tc.target, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			if tc.want == http.StatusOK {
				if got := decode[store.Order](t, rec); got.ID != 12 || got.Quantity != 3 {
					t.Fatalf("body = %+v", got)
				}
			}
		})
	}
}
