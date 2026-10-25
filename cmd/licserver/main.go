// Command licserver runs the TaktaPlus license server.
//
//	licserver [serve]   apply migrations and serve HTTP (default)
//	licserver migrate   apply migrations and exit
//	licserver version   print the version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/farshadfighter/taktaplus-lic/internal/buildinfo"
	"github.com/farshadfighter/taktaplus-lic/internal/config"
	"github.com/farshadfighter/taktaplus-lic/internal/server"
	"github.com/farshadfighter/taktaplus-lic/internal/store"
	"github.com/farshadfighter/taktaplus-lic/web"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if cmd == "version" {
		fmt.Println(buildinfo.String())
		return
	}
	if cmd != "serve" && cmd != "migrate" {
		fmt.Fprintln(os.Stderr, "usage: licserver [serve|migrate|version]")
		os.Exit(2)
	}
	if err := run(cmd); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cmd string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return fmt.Errorf("config: TPLIC_LOG_LEVEL: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool, log); err != nil {
		return err
	}
	if cmd == "migrate" {
		return nil
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.New(log, pool, web.App()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("license server listening", "addr", cfg.Listen, "version", buildinfo.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}
