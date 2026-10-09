package reservation

import (
	"context"
	"log/slog"
	"time"
)

func RunExpiry(ctx context.Context, store *Service, interval time.Duration) error {
	if interval <= 0 {
		return ErrInvalid
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		n, err := store.Expire(ctx, 100)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			slog.Error("expiry sweep failed; due rows remain recoverable", "error", err)
		} else if n > 0 {
			slog.Info("reservations expired", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
