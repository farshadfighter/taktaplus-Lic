package selfbackup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StoredKey is a derived key as an installation keeps it (sealed with its
// master key): the passphrase itself is never stored.
type StoredKey struct {
	KDF      KDF       `json:"kdf"`
	Secret   []byte    `json:"secret"`
	Verifier string    `json:"verifier"`
	SetAt    time.Time `json:"set_at"`
}

// Store returns the key in its storable form.
func (k *Key) Store() StoredKey {
	return StoredKey{KDF: k.KDF, Secret: k.Secret[:], Verifier: k.Verifier, SetAt: time.Now().UTC()}
}

// Key turns a stored key back into a Key.
func (s StoredKey) Key() (*Key, error) {
	if len(s.Secret) != 32 {
		return nil, errors.New("the stored backup key is invalid")
	}
	k := &Key{KDF: s.KDF, Verifier: s.Verifier}
	copy(k.Secret[:], s.Secret)
	if verifier(k.Secret) != s.Verifier {
		return nil, errors.New("the stored backup key is invalid")
	}
	return k, nil
}

// MarshalKey and UnmarshalKey are JSON helpers for sealing.
func MarshalKey(s StoredKey) []byte { b, _ := json.Marshal(s); return b }

// UnmarshalKey decodes a stored key.
func UnmarshalKey(b []byte) (StoredKey, error) {
	var s StoredKey
	err := json.Unmarshal(b, &s)
	return s, err
}

// Job is one backup run.
type Job struct {
	Key      *Key
	Options  Options
	Header   Header // product, version, hostname
	Dir      string // local directory of .tpbak files
	Prefix   string // file name prefix, e.g. "taktaplus-"
	Keep     int    // local files to keep (0 = all)
	SFTP     *SFTP  // optional remote copy
	SFTPKeep int
}

// Result describes a finished backup.
type Result struct {
	File    string // base name in Dir
	Size    int64
	SFTPErr error // remote copy failure; the local backup is still good
}

// Run writes an encrypted backup into Dir, prunes old local files and copies
// the new one over SFTP.
func Run(ctx context.Context, j Job) (*Result, error) {
	if j.Key == nil {
		return nil, errors.New("set a backup passphrase first")
	}
	if err := os.MkdirAll(j.Dir, 0o700); err != nil {
		return nil, err
	}
	name := j.Prefix + time.Now().UTC().Format("20060102-150405") + ".tpbak"
	final := filepath.Join(j.Dir, name)
	tmp := final + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Result, error) {
		f.Close()
		os.Remove(tmp)
		return nil, err
	}
	w, err := Encrypt(f, j.Key, j.Header)
	if err != nil {
		return fail(err)
	}
	if err := Write(ctx, w, j.Options); err != nil {
		return fail(err)
	}
	if err := w.Close(); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	st, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fail(err)
	}
	res := &Result{File: name, Size: st.Size()}
	if j.Keep > 0 {
		files, _ := List(j.Dir, j.Prefix)
		for i := j.Keep; i < len(files); i++ {
			_ = os.Remove(filepath.Join(j.Dir, files[i].Name))
		}
	}
	if j.SFTP != nil {
		rf, err := os.Open(final)
		if err != nil {
			res.SFTPErr = err
		} else {
			res.SFTPErr = j.SFTP.Upload(ctx, name, rf, j.Prefix, j.SFTPKeep)
			rf.Close()
		}
		if res.SFTPErr != nil {
			res.SFTPErr = fmt.Errorf("SFTP: %w", res.SFTPErr)
		}
	}
	return res, nil
}

// FileInfo is a local backup file.
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// List returns the .tpbak files in dir with the prefix, newest first.
func List(dir, prefix string) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []FileInfo{}, nil
	} else if err != nil {
		return nil, err
	}
	out := []FileInfo{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) || !strings.HasSuffix(e.Name(), ".tpbak") {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, FileInfo{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name > out[k].Name })
	return out, nil
}
