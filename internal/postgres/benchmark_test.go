package postgres

import (
	"checkoutlab/internal/reservation"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

// This measures committed replay, excluding setup. ns/op is serial latency,
// not a throughput or production capacity claim. Run on a disposable test DB.
func BenchmarkCommittedReplay(b *testing.B) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		b.Skip("TEST_DATABASE_URL required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		b.Fatal(err)
	}
	if !strings.HasPrefix(cfg.ConnConfig.Database, "checkout_test") {
		b.Fatal("disposable checkout_test database required")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(context.Background(), "TRUNCATE reservations,inventory;INSERT INTO inventory(sku,available) VALUES ('book-go',10)"); err != nil {
		b.Fatal(err)
	}
	s := reservation.NewService(NewStore(pool), time.Minute)
	key := fmt.Sprintf("bench-key-%d", time.Now().UnixNano())
	first, _, err := s.Create(context.Background(), key, "book-go", 1)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, replay, err := s.Create(context.Background(), key, "book-go", 1)
		if err != nil || !replay || r.ID != first.ID {
			b.Fatalf("replay=%v row=%+v err=%v", replay, r, err)
		}
	}
	b.StopTimer()
	var stock, rows int
	if err = pool.QueryRow(context.Background(), "SELECT available,(SELECT count(*) FROM reservations) FROM inventory WHERE sku='book-go'").Scan(&stock, &rows); err != nil || stock != 9 || rows != 1 {
		b.Fatalf("stock=%d rows=%d err=%v", stock, rows, err)
	}
}
