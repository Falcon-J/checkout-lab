package postgres

import (
	"checkoutlab/internal/httpapi"
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLifecycleHTTP(t *testing.T) {
	for _, action := range []string{"confirm", "cancel"} {
		t.Run(action, func(t *testing.T) {
			s := testStore(t)
			r, _, err := s.Create(context.Background(), "lifecycle-key-12345", "book-go", 1)
			if err != nil {
				t.Fatal(err)
			}
			h := httpapi.Handler(s.Service, s.pool.Ping, testToken)
			call := func(a string) *httptest.ResponseRecorder {
				req := httptest.NewRequest("POST", "/reservations/"+r.ID+"/"+a, nil)
				req.Header.Set("Authorization", "Bearer "+testToken)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				return w
			}
			want, stock := "confirmed", 0
			other := "cancel"
			if action == "cancel" {
				want, stock, other = "cancelled", 1, "confirm"
			}
			for i := 0; i < 2; i++ {
				w := call(action)
				if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
					t.Fatalf("transition: %d %s", w.Code, w.Body.String())
				}
			}
			if w := call(other); w.Code != 409 {
				t.Fatalf("conflicting transition: %d", w.Code)
			}
			if n, err := s.Expire(context.Background(), 100); err != nil || n != 0 {
				t.Fatalf("terminal expiry: %d %v", n, err)
			}
			var got int
			if err := s.pool.QueryRow(context.Background(), "SELECT available FROM inventory WHERE sku='book-go'").Scan(&got); err != nil || got != stock {
				t.Fatalf("stock=%d want=%d err=%v", got, stock, err)
			}
		})
	}
}

func TestOverdueConfirmation(t *testing.T) {
	s := testStore(t)
	r, _, err := s.Create(context.Background(), "overdue-key-1234567", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(context.Background(), "UPDATE reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/reservations/"+r.ID+"/confirm", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	httpapi.Handler(s.Service, s.pool.Ping, testToken).ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatalf("overdue confirm: %d %s", w.Code, w.Body.String())
	}
	got, err := s.Get(context.Background(), r.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("state=%+v err=%v", got, err)
	}
	var stock int
	if err = s.pool.QueryRow(context.Background(), "SELECT available FROM inventory WHERE sku='book-go'").Scan(&stock); err != nil || stock != 1 {
		t.Fatalf("stock=%d err=%v", stock, err)
	}
}

func TestCommittedReplayDoesNotWaitForKeyLock(t *testing.T) {
	s := testStore(t)
	key := "committed-key-123456"
	first, _, err := s.Create(context.Background(), key, "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(context.Background(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	replay, yes, err := s.Create(ctx, key, "book-go", 1)
	if err != nil || !yes || replay.ID != first.ID {
		t.Fatalf("committed replay blocked: %+v %v %v", replay, yes, err)
	}
}

func TestConcurrentConfirmCancel(t *testing.T) {
	s := testStore(t)
	r, _, err := s.Create(context.Background(), "terminal-race-12345", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	h := httpapi.Handler(s.Service, s.pool.Ping, testToken)
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make(chan int, 2)
	for _, a := range []string{"confirm", "cancel"} {
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			<-start
			req := httptest.NewRequest("POST", "/reservations/"+r.ID+"/"+a, nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			codes <- w.Code
		}(a)
	}
	close(start)
	wg.Wait()
	close(codes)
	ok, conflict := 0, 0
	for c := range codes {
		if c == 200 {
			ok++
		} else if c == 409 {
			conflict++
		} else {
			t.Fatalf("unexpected status %d", c)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
	got, err := s.Get(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stock int
	if err = s.pool.QueryRow(context.Background(), "SELECT available FROM inventory WHERE sku='book-go'").Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if (got.Status == "confirmed" && stock != 0) || (got.Status == "cancelled" && stock != 1) || (got.Status != "confirmed" && got.Status != "cancelled") {
		t.Fatalf("state=%s stock=%d", got.Status, stock)
	}
}
