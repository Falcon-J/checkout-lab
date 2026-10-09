package postgres

import (
	"checkoutlab/internal/reservation"
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfirmationChecksDeadlineAfterRowLock(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	row, _, err := s.Create(ctx, "deadline-lock-12345", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE reservations SET expires_at=clock_timestamp()+interval '300 milliseconds' WHERE id=$1", row.ID); err != nil {
		t.Fatal(err)
	}
	// Make the deadline visible before taking the long-lived lock.
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT id FROM reservations WHERE id=$1 FOR UPDATE", row.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Confirm(ctx, row.ID); done <- err }()
	time.Sleep(600 * time.Millisecond)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, reservation.ErrState) {
		t.Fatalf("confirmed after deadline: %v", err)
	}
	got, err := s.Get(ctx, row.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("state=%+v err=%v", got, err)
	}
}

func TestCancellationDuringRowLockWait(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	row, _, err := s.Create(ctx, "cancel-wait-1234567", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT id FROM reservations WHERE id=$1 FOR UPDATE", row.ID); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err = s.Confirm(bounded, row.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock cancellation: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, row.ID)
	if err != nil || got.Status != "held" {
		t.Fatalf("cancelled request changed state: %+v %v", got, err)
	}
	if _, err = s.Cancel(ctx, row.ID); err != nil {
		t.Fatalf("connection/row remained blocked after cancellation: %v", err)
	}
}

func TestConcurrentExpiryAndCancel(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	row, _, err := s.Create(ctx, "expiry-cancel-12345", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", row.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	start := make(chan struct{})
	go func() { <-start; _, err := s.Expire(ctx, 100); done <- err }()
	go func() {
		<-start
		_, err := s.Cancel(ctx, row.ID)
		if errors.Is(err, reservation.ErrState) {
			err = nil
		}
		done <- err
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(ctx, row.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("state=%+v err=%v", got, err)
	}
	var stock int
	if err = s.pool.QueryRow(ctx, "SELECT available FROM inventory WHERE sku='book-go'").Scan(&stock); err != nil || stock != 1 {
		t.Fatalf("stock=%d err=%v", stock, err)
	}
}
