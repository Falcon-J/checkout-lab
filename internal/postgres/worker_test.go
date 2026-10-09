package postgres

import (
	"checkoutlab/internal/reservation"
	"context"
	"testing"
	"time"
)

func TestWorkerSweepOnStartupAndCancellation(t *testing.T) {
	s := testStore(t)
	r, _, err := s.Create(context.Background(), "worker-key-1234567", "book-go", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(context.Background(), "UPDATE reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- reservation.RunExpiry(ctx, reservation.NewService(NewStore(s.pool), time.Minute), time.Hour)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := s.Get(context.Background(), r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "expired" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup sweep did not release overdue reservation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
