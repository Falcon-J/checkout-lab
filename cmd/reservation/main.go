package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"checkoutlab/internal/httpapi"
	"checkoutlab/internal/postgres"
	"checkoutlab/internal/reservation"
	"github.com/jackc/pgx/v5/pgxpool"
)

func validateLocalConfig(mode, addr, token string) error {
	if mode != "all" && mode != "api" && mode != "worker" {
		return errors.New("mode must be all, api or worker")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("local learning API must bind a loopback address")
	}
	if mode != "worker" && len(token) < 32 {
		return errors.New("API_TOKEN must contain at least 32 characters")
	}
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "all", "all, api, or worker")
	addr := flag.String("addr", "127.0.0.1:8080", "loopback HTTP address")
	ttl := flag.Duration("ttl", 15*time.Minute, "reservation lifetime")
	interval := flag.Duration("interval", 5*time.Second, "expiry sweep interval")
	flag.Parse()
	token := os.Getenv("API_TOKEN")
	if err := validateLocalConfig(*mode, *addr, token); err != nil {
		return err
	}
	if *ttl < time.Second || *ttl > 24*time.Hour || *interval < time.Millisecond || *interval > time.Hour {
		return errors.New("ttl must be 1s..24h and interval 1ms..1h")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	cfg.MaxConns = 10
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database pool: %w", err)
	}
	defer pool.Close()
	startup, stop := context.WithTimeout(ctx, 5*time.Second)
	var ready bool
	err = pool.QueryRow(startup, "SELECT to_regclass('inventory') IS NOT NULL AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid=to_regclass('reservations') AND conname='reservations_status_check' AND pg_get_constraintdef(oid) LIKE '%confirmed%' AND pg_get_constraintdef(oid) LIKE '%cancelled%')").Scan(&ready)
	stop()
	if err != nil {
		return fmt.Errorf("database startup: %w", err)
	}
	if !ready {
		return errors.New("schema missing or outdated; apply all migrations in numeric order")
	}
	store := reservation.NewService(postgres.NewStore(pool), *ttl)
	if *mode == "worker" {
		return reservation.RunExpiry(ctx, store, *interval)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	if *mode == "all" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := reservation.RunExpiry(ctx, store, *interval); err != nil {
				failures <- err
			}
		}()
	}
	srv := &http.Server{Addr: *addr, Handler: httpapi.Handler(store, pool.Ping, token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		err := srv.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			failures <- err
		}
	}()
	slog.Info("local reservation API listening", "address", *addr, "mode", *mode)
	var result error
	select {
	case <-ctx.Done():
	case result = <-failures:
	}
	cancel()
	shutdownCtx, stopShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		if result == nil {
			result = err
		}
	}
	wg.Wait()
	return result
}
