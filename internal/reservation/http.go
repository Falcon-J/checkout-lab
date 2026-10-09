package reservation

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"
)

func Handler(store *Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "up"}) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := store.pool.Ping(ctx); err != nil {
			writeJSON(w, 503, map[string]string{"status": "not_ready"})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "up"})
	})
	mux.HandleFunc("POST /reservations", func(w http.ResponseWriter, r *http.Request) {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			problem(w, 415, "unsupported_media_type")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				problem(w, 413, "payload_too_large")
			} else {
				problem(w, 400, "invalid_request")
			}
			return
		}
		sku, quantity, err := decodeInput(body)
		key := r.Header.Get("Idempotency-Key")
		if err != nil || !validInput(key, sku, quantity) {
			problem(w, 400, "invalid_request")
			return
		}
		result, replayed, err := store.Create(r.Context(), key, sku, quantity)
		if err != nil {
			storeError(w, err)
			return
		}
		w.Header().Set("Location", "/reservations/"+result.ID)
		code := http.StatusCreated
		if replayed {
			code = http.StatusOK
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, code, result)
	})
	mux.HandleFunc("GET /reservations/{id}", func(w http.ResponseWriter, r *http.Request) {
		result, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			storeError(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/health/live" && r.URL.Path != "/health/ready" {
			if len(token) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				problem(w, 401, "unauthorized")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// Fixed two-field contract: reject duplicate keys and trailing JSON rather than
// silently accepting a last-write-wins request different from the caller's intent.
func decodeInput(body []byte) (string, int, error) {
	if !utf8.Valid(body) {
		return "", 0, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return "", 0, ErrInvalid
	}
	seen := make(map[string]bool, 2)
	var sku string
	var quantity int
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return "", 0, ErrInvalid
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return "", 0, ErrInvalid
		}
		seen[name] = true
		switch name {
		case "sku":
			err = d.Decode(&sku)
		case "quantity":
			err = d.Decode(&quantity)
		default:
			return "", 0, ErrInvalid
		}
		if err != nil {
			return "", 0, ErrInvalid
		}
	}
	if t, err = d.Token(); err != nil || t != json.Delim('}') {
		return "", 0, ErrInvalid
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return "", 0, ErrInvalid
	}
	if !seen["sku"] || !seen["quantity"] {
		return "", 0, ErrInvalid
	}
	return sku, quantity, nil
}
func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		problem(w, 400, "invalid_request")
	case errors.Is(err, ErrNotFound):
		problem(w, 404, "not_found")
	case errors.Is(err, ErrOutOfStock):
		problem(w, 409, "out_of_stock")
	case errors.Is(err, ErrConflict):
		problem(w, 409, "idempotency_conflict")
	default:
		slog.Error("reservation operation failed", "error", err)
		problem(w, 503, "temporarily_unavailable")
	}
}
func problem(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("response write failed", "error", err)
	}
}
