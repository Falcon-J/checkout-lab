package postgres

import (
	"checkoutlab/internal/httpapi"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "local-test-token-01234567890123456789"

func TestHTTPReservationRoundTrip(t *testing.T) {
	s := testStore(t)
	h := httpapi.Handler(s.Service, s.pool.Ping, testToken)
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
