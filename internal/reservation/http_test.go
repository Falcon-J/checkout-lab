package reservation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "local-test-token-01234567890123456789"

func TestStoreRejectsInvalidInputBeforeDatabase(t *testing.T) {
	s := NewStore(nil, time.Minute)
	for _, tc := range []struct {
		key, sku string
		q        int
	}{
		{"short", "book-go", 1}, {"valid-key-12345678", "Book Go", 1},
		{"valid-key-12345678", "book-go", 0}, {"valid-key-12345678", "book-go", 11},
	} {
		_, _, err := s.Create(context.Background(), tc.key, tc.sku, tc.q)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid input %+v returned %v", tc, err)
		}
	}
}

func TestHTTPRequiresOperatorToken(t *testing.T) {
	handler := Handler(nil, testToken)
	for _, token := range []string{"", "Bearer wrong"} {
		r := httptest.NewRequest(http.MethodGet, "/reservations/0123456789abcdef0123456789abcdef", nil)
		r.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestHTTPRejectsMalformedInputBeforeDatabase(t *testing.T) {
	handler := Handler(nil, testToken)
	for _, tc := range []struct {
		name, body, key, content string
		want                     int
	}{
		{"fraction", `{"sku":"book-go","quantity":1.5}`, "valid-key-12345678", "application/json", 400},
		{"duplicate", `{"sku":"book-go","sku":"other","quantity":1}`, "valid-key-12345678", "application/json", 400},
		{"unknown", `{"sku":"book-go","quantity":1,"price":0}`, "valid-key-12345678", "application/json", 400},
		{"trailing", `{"sku":"book-go","quantity":1} {}`, "valid-key-12345678", "application/json", 400},
		{"zero", `{"sku":"book-go","quantity":0}`, "valid-key-12345678", "application/json", 400},
		{"missing key", `{"sku":"book-go","quantity":1}`, "", "application/json", 400},
		{"oversize", strings.Repeat(" ", 4097), "valid-key-12345678", "application/json", 413},
		{"wrong content", `{"sku":"book-go","quantity":1}`, "valid-key-12345678", "text/plain", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/reservations", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("Idempotency-Key", tc.key)
			r.Header.Set("Content-Type", tc.content)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("code=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestHTTPReservationRoundTrip(t *testing.T) {
	s := testStore(t)
	h := Handler(s, testToken)
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// Discard the first response to model a lost acknowledgement.
	first := request("POST", "/reservations", `{"sku":"book-go","quantity":1}`, "http-key-123456789")
	if first.Code != 201 {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	replay := request("POST", "/reservations", `{"sku":"book-go","quantity":1}`, "http-key-123456789")
	if replay.Code != 200 || replay.Header().Get("Location") != first.Header().Get("Location") {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	read := request("GET", first.Header().Get("Location"), "", "")
	if read.Code != 200 || read.Body.String() != replay.Body.String() {
		t.Fatalf("read: %d %s", read.Code, read.Body.String())
	}
	conflict := request("POST", "/reservations", `{"sku":"book-go","quantity":2}`, "http-key-123456789")
	if conflict.Code != 409 {
		t.Fatalf("conflict=%d", conflict.Code)
	}
	missing := request("GET", "/reservations/00000000000000000000000000000000", "", "")
	if missing.Code != 404 {
		t.Fatalf("missing=%d", missing.Code)
	}
}
