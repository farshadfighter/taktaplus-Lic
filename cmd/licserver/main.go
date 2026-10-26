// Command licserver runs the TaktaPlus license server.
//
//	licserver [serve]                              apply migrations and serve HTTP (default)
//	licserver migrate                              apply migrations and exit
//	licserver create-admin -email E -name N        create a vendor owner account
//	licserver version                              print the version
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/farshadfighter/taktaplus-lic/internal/auth"
	"github.com/farshadfighter/taktaplus-lic/internal/buildinfo"
	"github.com/farshadfighter/taktaplus-lic/internal/config"
	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
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
	switch cmd {
	case "serve", "migrate", "create-admin":
	default:
		fmt.Fprintln(os.Stderr, "usage: licserver [serve|migrate|create-admin|version]")
		os.Exit(2)
	}
	if err := run(cmd, os.Args[min(2, len(os.Args)):]); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
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

	authSvc := auth.NewService(pool, cfg.MasterKey)
	issuer, err := licensing.LoadIssuer(cfg.LicenseKeyFile, cfg.LicenseKeyPass)
	if err != nil {
		return fmt.Errorf("load license signing key: %w", err)
	}
	if issuer.KeyID() == "" {
		log.Warn("no license signing key configured (TPLIC_LICENSE_KEY_FILE); licenses cannot be issued")
	}
	licSvc := licensing.NewService(pool, cfg.MasterKey, issuer, cfg.PublicURL)
	switch cmd {
	case "migrate":
		return nil
	case "create-admin":
		return createAdmin(ctx, pool, authSvc, args)
	}

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: server.New(server.Deps{
			Log: log, DB: pool, Auth: authSvc, Licensing: licSvc, UI: web.App(), CookieSecure: cfg.CookieSecure,
		}),
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

// createAdmin creates a vendor owner. The password comes from
// $TPLIC_ADMIN_PASSWORD or the first line of stdin.
func createAdmin(ctx context.Context, pool *pgxpool.Pool, svc *auth.Service, args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	email := fs.String("email", "", "login email (required)")
	name := fs.String("name", "Administrator", "display name")
	company := fs.String("company", "TaktaPlus", "vendor organization name (first run only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("-email is required")
	}
	password := os.Getenv("TPLIC_ADMIN_PASSWORD")
	if password == "" {
		fmt.Fprint(os.Stderr, "password: ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return err
		}
		password = strings.TrimRight(line, "\r\n")
	}
	vendorID, err := orgs.EnsureVendor(ctx, pool, *company)
	if err != nil {
		return err
	}
	u, err := svc.CreateUser(ctx, vendorID, *email, *name, auth.RoleOwner, password)
	if err != nil {
		return err
	}
	fmt.Printf("created owner %s (id %d); set up two-factor authentication at first login\n", u.Email, u.ID)
	return nil
}
