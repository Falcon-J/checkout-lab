package postgres

import (
	"checkoutlab/internal/reservation"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func scan(row pgx.Row) (reservation.Reservation, error) {
	var r reservation.Reservation
	err := row.Scan(&r.ID, &r.SKU, &r.Quantity, &r.Status, &r.ExpiresAt)
	r.ExpiresAt = r.ExpiresAt.UTC()
	return r, err
}

const columns = "id, sku, quantity, status, expires_at"

// Create serializes equal keys before touching stock. The unique constraint
// remains the durable guard; hash collisions only serialize unrelated requests.
func (s *Store) Create(ctx context.Context, key, sku string, quantity int, ttl time.Duration) (reservation.Reservation, bool, error) {
	// A committed key needs only a snapshot read. New keys still lock and
	// recheck inside their transaction, so racing first requests remain safe.
	existing, lookupErr := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM reservations WHERE request_key=$1", key))
	if lookupErr == nil {
		if existing.SKU != sku || existing.Quantity != quantity {
			return reservation.Reservation{}, false, reservation.ErrConflict
		}
		return existing, true, nil
	}
	if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return reservation.Reservation{}, false, fmt.Errorf("replay lookup: %w", lookupErr)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return reservation.Reservation{}, false, fmt.Errorf("begin: %w", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); err != nil {
		return reservation.Reservation{}, false, fmt.Errorf("key lock: %w", err)
	}
	r, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM reservations WHERE request_key=$1", key))
	if err == nil {
		if r.SKU != sku || r.Quantity != quantity {
			return reservation.Reservation{}, false, reservation.ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return reservation.Reservation{}, false, fmt.Errorf("replay commit: %w", err)
		}
		return r, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return reservation.Reservation{}, false, fmt.Errorf("key lookup: %w", err)
	}
	result, err := tx.Exec(ctx, "UPDATE inventory SET available=available-$2 WHERE sku=$1 AND available>=$2", sku, quantity)
	if err != nil {
		return reservation.Reservation{}, false, fmt.Errorf("reserve stock: %w", err)
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM inventory WHERE sku=$1)", sku).Scan(&exists); err != nil {
			return reservation.Reservation{}, false, fmt.Errorf("stock lookup: %w", err)
		}
		if !exists {
			return reservation.Reservation{}, false, reservation.ErrNotFound
		}
		return reservation.Reservation{}, false, reservation.ErrOutOfStock
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return reservation.Reservation{}, false, fmt.Errorf("identifier: %w", err)
	}
	id := hex.EncodeToString(random[:])
	r, err = scan(tx.QueryRow(ctx, "INSERT INTO reservations(id,request_key,sku,quantity,status,expires_at) VALUES($1,$2,$3,$4,'held',clock_timestamp()+$5*interval '1 millisecond') RETURNING "+columns, id, key, sku, quantity, ttl.Milliseconds()))
	if err != nil {
		return reservation.Reservation{}, false, fmt.Errorf("insert reservation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return reservation.Reservation{}, false, fmt.Errorf("commit: %w", err)
	}
	return r, false, nil
}

func (s *Store) Get(ctx context.Context, id string) (reservation.Reservation, error) {
	r, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM reservations WHERE id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return reservation.Reservation{}, reservation.ErrNotFound
	}
	if err != nil {
		return reservation.Reservation{}, fmt.Errorf("reservation lookup: %w", err)
	}
	return r, nil
}

// Expire has only local side effects: row locks and one commit are enough.
// A crash before commit rolls back; a restart discovers the same due rows.
func (s *Store) Expire(ctx context.Context, limit int) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("expiry begin: %w", err)
	}
	defer rollback(tx)
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

// Rollback uses an independent bounded context when a request is cancelled.
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (s *Store) Transition(ctx context.Context, id, to string) (reservation.Reservation, error) {
	if to != "confirmed" && to != "cancelled" {
		return reservation.Reservation{}, reservation.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return reservation.Reservation{}, fmt.Errorf("transition begin: %w", err)
	}
	defer rollback(tx)
	var row reservation.Reservation
	var due bool
	err = tx.QueryRow(ctx, "SELECT "+columns+" FROM reservations WHERE id=$1 FOR UPDATE", id).Scan(&row.ID, &row.SKU, &row.Quantity, &row.Status, &row.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, reservation.ErrNotFound
	}
	if err != nil {
		return row, fmt.Errorf("transition lock: %w", err)
	}
	row.ExpiresAt = row.ExpiresAt.UTC()
	if row.Status == to {
		return row, nil
	}
	if row.Status != "held" {
		return row, reservation.ErrState
	}

	// Evaluate the database deadline after acquiring the row lock. A SELECT
	// projection evaluated before waiting could confirm an already-due hold.
	if err = tx.QueryRow(ctx, "SELECT expires_at<=clock_timestamp() FROM reservations WHERE id=$1", id).Scan(&due); err != nil {
		return row, fmt.Errorf("transition deadline: %w", err)
	}
	next := to
	if due {
		next = "expired"
	}
	if next != "confirmed" {
		result, err := tx.Exec(ctx, "UPDATE inventory SET available=available+$2 WHERE sku=$1", row.SKU, row.Quantity)
		if err != nil {
			return row, fmt.Errorf("transition release: %w", err)
		}
		if result.RowsAffected() != 1 {
			return row, errors.New("reservation stock row missing")
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE reservations SET status=$2 WHERE id=$1", id, next); err != nil {
		return row, fmt.Errorf("transition state: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return row, fmt.Errorf("transition commit: %w", err)
	}
	row.Status = next
	row.ExpiresAt = row.ExpiresAt.UTC()
	if due {
		return row, reservation.ErrState
	}
	return row, nil
}
