package selfbackup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// DumpEntry is the tar entry holding the pg_dump output; data files are
// under DataPrefix.
const (
	DumpEntry  = "db.dump"
	DataPrefix = "data/"
)

// Options say what to back up or restore.
type Options struct {
	DatabaseURL string
	PgDump      string   // pg_dump binary, default "pg_dump"
	PgRestore   string   // pg_restore binary, default "pg_restore"
	DataDir     string   // "" skips data files
	Exclude     []string // top-level names under DataDir left out (and kept on restore)
}

func (o Options) bin(name, def string) string {
	if name != "" {
		return name
	}
	return def
}

// Write writes the database dump and data files as a tar stream to w.
func Write(ctx context.Context, w io.Writer, o Options) error {
	tw := tar.NewWriter(w)
	// The dump goes to a temporary file first: tar needs the size.
	tmp, err := os.CreateTemp("", "tpbak-*.dump")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, o.bin(o.PgDump, "pg_dump"), "--format=custom", "--no-owner", "--no-privileges", "--dbname="+o.DatabaseURL)
	cmd.Stdout, cmd.Stderr = tmp, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	st, err := tmp.Stat()
	if err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: DumpEntry, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := io.Copy(tw, tmp); err != nil {
		return err
	}
	if o.DataDir != "" {
		err = filepath.WalkDir(o.DataDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rel, _ := filepath.Rel(o.DataDir, p)
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if top, _, _ := strings.Cut(rel, "/"); slices.Contains(o.Exclude, top) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			switch {
			case d.IsDir():
				return tw.WriteHeader(&tar.Header{Name: DataPrefix + rel + "/", Mode: int64(info.Mode().Perm()), ModTime: info.ModTime(), Typeflag: tar.TypeDir})
			case info.Mode().IsRegular():
				f, err := os.Open(p)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := tw.WriteHeader(&tar.Header{Name: DataPrefix + rel, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
					return err
				}
				_, err = io.CopyN(tw, f, info.Size())
				return err
			}
			return nil // sockets, links: not backed up
		})
		if err != nil {
			return err
		}
	}
	return tw.Close()
}

// Restore replaces the database and the data files with the content of the
// tar stream r. Files under DataDir that are not excluded are removed first.
func Restore(ctx context.Context, r io.Reader, o Options) error {
	tr := tar.NewReader(r)
	h, err := tr.Next()
	if err != nil || h.Name != DumpEntry {
		return errors.New("the backup does not start with a database dump")
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, o.bin(o.PgRestore, "pg_restore"), "--clean", "--if-exists", "--no-owner", "--no-privileges", "--single-transaction", "--dbname="+o.DatabaseURL)
	cmd.Stdin, cmd.Stderr = io.LimitReader(tr, h.Size), &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	if o.DataDir == "" {
		return nil
	}
	entries, err := os.ReadDir(o.DataDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range entries {
		if !slices.Contains(o.Exclude, e.Name()) {
			if err := os.RemoveAll(filepath.Join(o.DataDir, e.Name())); err != nil {
				return err
			}
		}
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(h.Name, DataPrefix)
		clean := path.Clean(strings.TrimSuffix(rel, "/"))
		if !ok || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return fmt.Errorf("unexpected entry %q in the backup", h.Name)
		}
		if top, _, _ := strings.Cut(clean, "/"); slices.Contains(o.Exclude, top) {
			continue
		}
		p := filepath.Join(o.DataDir, filepath.FromSlash(clean))
		mode := os.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, mode|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.CopyN(f, tr, h.Size)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}
