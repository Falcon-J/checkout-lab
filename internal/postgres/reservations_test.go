package postgres

import (
	"checkoutlab/internal/reservation"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type testFixture struct {
	*reservation.Service
	pool *pgxpool.Pool
}

func testStore(t *testing.T) *testFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; PostgreSQL integration test")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "checkout_test") {
		t.Fatal("refusing to reset non-test database")
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// These tests exercise transactional concurrency with the same ten-connection
	// limit as the application, excluding simultaneous SCRAM connection startup.
	warmCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var warm []*pgxpool.Conn
	defer func() {
		for _, conn := range warm {
			conn.Release()
		}
	}()
	for i := 0; i < 10; i++ {
		conn, err := pool.Acquire(warmCtx)
		if err != nil {
			t.Fatalf("pool warmup: %v", err)
		}
		warm = append(warm, conn)
	}
	// Release before issuing setup SQL, which also consumes a connection.
	for _, conn := range warm {
		conn.Release()
	}
	warm = nil
	_, err = pool.Exec(context.Background(), "TRUNCATE reservations, inventory; INSERT INTO inventory(sku, available) VALUES ('book-go', 1), ('other', 10)")
	if err != nil {
		t.Fatal(err)
	}
	return &testFixture{reservation.NewService(NewStore(pool), 15*time.Minute), pool}
}

func TestLastItemConcurrency(t *testing.T) {
	s := testStore(t)
	var successes, conflicts atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, err := s.Create(context.Background(), fmt.Sprintf("buyer-key-%08d", i), "book-go", 1)
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, reservation.ErrOutOfStock) {
				conflicts.Add(1)
			} else {
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 19 {
		t.Fatalf("accepted=%d conflicts=%d", successes.Load(), conflicts.Load())
	}
	var stock, rows int
	if err := s.pool.QueryRow(context.Background(), "SELECT available FROM inventory WHERE sku='book-go'").Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM reservations").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if stock != 0 || rows != 1 {
		t.Fatalf("stock=%d rows=%d", stock, rows)
	}
}

func TestConcurrentReplay(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, err := s.Create(context.Background(), "same-request-key-123", "book-go", 1)
			if err != nil {
				t.Errorf("replay: %v", err)
				return
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("multiple reservation IDs")
		}
	}
	if first == "" {
		t.Fatal("no reservation")
	}
	_, _, err := s.Create(context.Background(), "same-request-key-123", "book-go", 2)
	if !errors.Is(err, reservation.ErrConflict) {
		t.Fatalf("conflicting reuse: %v", err)
	}
}

func TestRejectedRequestDoesNotConsumeKey(t *testing.T) {
	s := testStore(t)
	_, _, err := s.Create(context.Background(), "rejected-key-123456", "book-go", 2)
	if !errors.Is(err, reservation.ErrOutOfStock) {
		t.Fatal(err)
	}
	_, _, err = s.Create(context.Background(), "rejected-key-123456", "book-go", 1)
	if err != nil {
		t.Fatalf("key consumed on rollback: %v", err)
	}
}

func TestExpiryOnceAcrossWorkersAndRestart(t *testing.T) {
	s := testStore(t)
	r, _, err := s.Create(context.Background(), "expiry-key-1234567", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(context.Background(), "UPDATE reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh Store has no in-memory state from the creator.
	restarted := reservation.NewService(NewStore(s.pool), 15*time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := restarted.Expire(context.Background(), 100); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n, err := restarted.Expire(context.Background(), 100); err != nil || n != 0 {
		t.Fatalf("repeat release n=%d err=%v", n, err)
	}
	got, err := restarted.Get(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stock int
	if err := s.pool.QueryRow(context.Background(), "SELECT available FROM inventory WHERE sku='book-go'").Scan(&stock); err != nil {
		t.Fatal(err)
	}
	replay, replayed, err := restarted.Create(context.Background(), "expiry-key-1234567", "book-go", 1)
	if err != nil || !replayed || replay.ID != r.ID || replay.Status != "expired" {
		t.Fatalf("expired replay=%+v replayed=%v err=%v", replay, replayed, err)
	}
	if err := s.pool.QueryRow(context.Background(), "SELECT available FROM inventory WHERE sku='book-go'").Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if got.Status != "expired" || stock != 1 {
		t.Fatalf("state=%s stock=%d", got.Status, stock)
	}
}

func TestActiveReservationIsNotReleased(t *testing.T) {
	s := testStore(t)
	r, _, err := s.Create(context.Background(), "active-key-1234567", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.Expire(context.Background(), 100); err != nil || n != 0 {
		t.Fatalf("premature release n=%d err=%v", n, err)
	}
	got, err := s.Get(context.Background(), r.ID)
	if err != nil || got.Status != "held" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
