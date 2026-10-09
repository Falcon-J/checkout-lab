package reservation

import (
	"context"
	"time"
)

// Repository owns database atomicity; Service owns input and request policy.
type Repository interface {
	Create(context.Context, string, string, int, time.Duration) (Reservation, bool, error)
	Get(context.Context, string) (Reservation, error)
	Transition(context.Context, string, string) (Reservation, error)
	Expire(context.Context, int) (int, error)
}
type Service struct {
	repo Repository
	ttl  time.Duration
}

func NewService(repo Repository, ttl time.Duration) *Service { return &Service{repo: repo, ttl: ttl} }
func (s *Service) Create(ctx context.Context, key, sku string, q int) (Reservation, bool, error) {
	if !ValidInput(key, sku, q) {
		return Reservation{}, false, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.repo.Create(ctx, key, sku, q, s.ttl)
}
func (s *Service) Get(ctx context.Context, id string) (Reservation, error) {
	if !ValidID(id) {
		return Reservation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.repo.Get(ctx, id)
}
func (s *Service) Confirm(ctx context.Context, id string) (Reservation, error) {
	return s.transition(ctx, id, "confirmed")
}
func (s *Service) Cancel(ctx context.Context, id string) (Reservation, error) {
	return s.transition(ctx, id, "cancelled")
}
func (s *Service) transition(ctx context.Context, id, to string) (Reservation, error) {
	if !ValidID(id) {
		return Reservation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.repo.Transition(ctx, id, to)
}
func (s *Service) Expire(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.repo.Expire(ctx, limit)
}
