package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/farshadfighter/taktaplus-lic/internal/buildinfo"
	"github.com/farshadfighter/taktaplus-lic/internal/config"
	"github.com/farshadfighter/taktaplus-lic/internal/releasekeys"
	"github.com/farshadfighter/taktaplus-lic/pkg/release"
	"github.com/farshadfighter/taktaplus-lic/pkg/selfbackup"
	"github.com/farshadfighter/taktaplus-lic/pkg/sysadmin"
)

// systemCommand runs the backup, restore and upgrade commands that the
// bundle's scripts call.
func systemCommand(ctx context.Context, cmd string, args []string, cfg config.Config, svc *sysadmin.Service) error {
	switch cmd {
	case "system-backup":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		trigger := fs.String("trigger", "manual", "manual or pre_upgrade")
		if err := fs.Parse(args); err != nil {
			return err
		}
		r, err := svc.Backup(ctx, *trigger, "cli")
		if err != nil {
			return err
		}
		fmt.Printf("backup: %s (%d bytes)\n", filepath.Join(cfg.DataDir, sysadmin.BackupDir, r.File), r.Size)
		return nil

	case "restore-backup":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		file := fs.String("file", "", "backup file (.tpbak, required)")
		keyOut := fs.String("master-key-out", "", "write the backed-up master key to this file")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *file == "" {
			return errors.New("-file is required")
		}
		pass := os.Getenv("TPLIC_BACKUP_PASSPHRASE")
		if pass == "" {
			fmt.Fprint(os.Stderr, "backup passphrase: ")
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && line == "" {
				return err
			}
			pass = strings.TrimRight(line, "\r\n")
		}
		h, mk, err := sysadmin.RestoreFile(ctx, *file, pass, release.ProductLicense, buildinfo.Version,
			selfbackup.Options{DatabaseURL: cfg.DatabaseURL, PgRestore: cfg.PgRestore, DataDir: cfg.DataDir})
		if err != nil {
			return err
		}
		if len(mk) > 0 && *keyOut != "" {
			if err := os.WriteFile(*keyOut, mk, 0o600); err != nil {
				return err
			}
		}
		fmt.Printf("restored the backup of %s (version %s, %s)\n", h.Hostname, h.Version, h.CreatedAt.Format("2006-01-02 15:04 MST"))
		return nil

	case "stage-upgrade":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		file := fs.String("file", "", "bundle (default stdin)")
		noQueue := fs.Bool("no-queue", false, "only verify and extract; do not queue for the updater")
		if err := fs.Parse(args); err != nil {
			return err
		}
		var in io.Reader = os.Stdin
		if *file != "" {
			f, err := os.Open(*file)
			if err != nil {
				return err
			}
			defer f.Close()
			in = f
		}
		u, dir, err := svc.Stage(ctx, bufio.NewReaderSize(in, 1<<20), "cli", !*noQueue)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cfg.DataDir, dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "verified %s %s (installed %s)\n", release.ProductLicense, u.Version, buildinfo.Version)
		fmt.Printf("%d %s %s\n", u.ID, u.Version, filepath.ToSlash(rel))
		return nil

	case "upgrade-snapshot":
		p, err := sysadmin.Snapshot(ctx, cfg.PgDump, cfg.DatabaseURL, cfg.DataDir)
		if err != nil {
			return err
		}
		fmt.Println(p)
		return nil

	case "upgrade-rollback":
		p, err := sysadmin.Rollback(ctx, cfg.PgRestore, cfg.DatabaseURL, cfg.DataDir)
		if err != nil {
			return err
		}
		fmt.Println("database restored from", p)
		return nil
	}
	return fmt.Errorf("unknown command %s", cmd)
}

func verifyBundleDir(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: licserver verify-bundle-dir DIR")
	}
	ring, err := releasekeys.Trusted()
	if err != nil {
		return err
	}
	m, err := sysadmin.VerifyBundleDir(args[0], ring)
	if err != nil {
		return err
	}
	fmt.Printf("valid: %s %s, %d files\n", m.Product, m.Version, len(m.Files))
	return nil
}
