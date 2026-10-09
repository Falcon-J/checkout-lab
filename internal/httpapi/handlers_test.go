package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "local-test-token-01234567890123456789"

func TestHTTPRequiresOperatorToken(t *testing.T) {
	handler := Handler(nil, nil, testToken)
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
	handler := Handler(nil, nil, testToken)
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
