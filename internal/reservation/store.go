package reservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrOutOfStock = errors.New("out of stock")
	ErrConflict   = errors.New("idempotency conflict")
	ErrNotFound   = errors.New("not found")
	ErrInvalid    = errors.New("invalid request")
	keyPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	skuPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	idPattern     = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type Reservation struct {
	ID        string    `json:"id"`
	SKU       string    `json:"sku"`
	Quantity  int       `json:"quantity"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Store struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

func NewStore(pool *pgxpool.Pool, ttl time.Duration) *Store { return &Store{pool: pool, ttl: ttl} }

func validInput(key, sku string, quantity int) bool {
	return keyPattern.MatchString(key) && skuPattern.MatchString(sku) && quantity >= 1 && quantity <= 10
}
func scan(row pgx.Row) (Reservation, error) {
	var r Reservation
	err := row.Scan(&r.ID, &r.SKU, &r.Quantity, &r.Status, &r.ExpiresAt)
	r.ExpiresAt = r.ExpiresAt.UTC()
	return r, err
}

const columns = "id, sku, quantity, status, expires_at"

// Create serializes equal keys before touching stock. The unique constraint
// remains the durable guard; hash collisions only serialize unrelated requests.
func (s *Store) Create(ctx context.Context, key, sku string, quantity int) (Reservation, bool, error) {
	if !validInput(key, sku, quantity) {
		return Reservation{}, false, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Reservation{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); err != nil {
		return Reservation{}, false, fmt.Errorf("key lock: %w", err)
	}
	r, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM reservations WHERE request_key=$1", key))
	if err == nil {
		if r.SKU != sku || r.Quantity != quantity {
			return Reservation{}, false, ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return Reservation{}, false, fmt.Errorf("replay commit: %w", err)
		}
		return r, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, false, fmt.Errorf("key lookup: %w", err)
	}
	result, err := tx.Exec(ctx, "UPDATE inventory SET available=available-$2 WHERE sku=$1 AND available>=$2", sku, quantity)
	if err != nil {
		return Reservation{}, false, fmt.Errorf("reserve stock: %w", err)
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM inventory WHERE sku=$1)", sku).Scan(&exists); err != nil {
			return Reservation{}, false, fmt.Errorf("stock lookup: %w", err)
		}
		if !exists {
			return Reservation{}, false, ErrNotFound
		}
		return Reservation{}, false, ErrOutOfStock
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return Reservation{}, false, fmt.Errorf("identifier: %w", err)
	}
	id := hex.EncodeToString(random[:])
	r, err = scan(tx.QueryRow(ctx, "INSERT INTO reservations(id,request_key,sku,quantity,status,expires_at) VALUES($1,$2,$3,$4,'held',clock_timestamp()+$5*interval '1 millisecond') RETURNING "+columns, id, key, sku, quantity, s.ttl.Milliseconds()))
	if err != nil {
		return Reservation{}, false, fmt.Errorf("insert reservation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Reservation{}, false, fmt.Errorf("commit: %w", err)
	}
	return r, false, nil
}

func (s *Store) Get(ctx context.Context, id string) (Reservation, error) {
	if !idPattern.MatchString(id) {
		return Reservation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM reservations WHERE id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrNotFound
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("reservation lookup: %w", err)
	}
	return r, nil
}

// Expire has only local side effects: row locks and one commit are enough.
// A crash before commit rolls back; a restart discovers the same due rows.
func (s *Store) Expire(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("expiry begin: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "SELECT id,sku,quantity FROM reservations WHERE status='held' AND expires_at<=clock_timestamp() ORDER BY sku,id LIMIT $1 FOR UPDATE SKIP LOCKED", limit)
	if err != nil {
		return 0, fmt.Errorf("expiry claim: %w", err)
	}
	type due struct {
		id, sku  string
		quantity int
	}
	var batch []due
	for rows.Next() {
		var r due
		if err = rows.Scan(&r.id, &r.sku, &r.quantity); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, r := range batch {
		result, err := tx.Exec(ctx, "UPDATE inventory SET available=available+$2 WHERE sku=$1", r.sku, r.quantity)
		if err != nil {
			return 0, fmt.Errorf("release stock: %w", err)
		}
		if result.RowsAffected() != 1 {
			return 0, errors.New("reservation stock row missing")
		}
		if _, err = tx.Exec(ctx, "UPDATE reservations SET status='expired' WHERE id=$1", r.id); err != nil {
			return 0, fmt.Errorf("expire state: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("expiry commit: %w", err)
	}
	return len(batch), nil
}
