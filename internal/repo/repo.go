// Package repo is the central package repository: uploaded signature
// packages and firmware images, and the signed manifest that lists them.
package repo

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/farshadfighter/taktaplus-lic/internal/signer"
	"github.com/farshadfighter/taktaplus-lic/pkg/licformat"
	"github.com/farshadfighter/taktaplus-lic/pkg/manifest"
	"github.com/farshadfighter/taktaplus-lic/pkg/repoauth"
)

// MaxFileSize bounds an upload (firmware images are the largest files).
const MaxFileSize = 4 << 30

var (
	// ErrNotFound means the package or manifest does not exist.
	ErrNotFound = errors.New("not found")
	// ErrNoKey means no package signing key is configured.
	ErrNoKey = errors.New("package signing key is not configured (TPLIC_PACKAGE_KEY_FILE)")
	// ErrUnauthorized means an installation may not use the repository.
	ErrUnauthorized = errors.New("this installation is not entitled to the package repository")
)

// InputError is a validation failure to show to the user.
type InputError struct{ msg string }

func (e InputError) Error() string { return e.msg }

func invalidf(format string, a ...any) error { return InputError{fmt.Sprintf(format, a...)} }

// Meta is the editable description of a package.
type Meta struct {
	File        string    `json:"file"`
	Platform    string    `json:"platform"`
	Type        string    `json:"type"`
	Version     string    `json:"version"`
	Models      []string  `json:"models"`
	FwMin       string    `json:"fw_min"`
	FwMax       string    `json:"fw_max"`
	UpgradeFrom []string  `json:"upgrade_from"`
	Notes       string    `json:"notes"`
	ReleasedAt  time.Time `json:"released_at"`
}

// Package is a stored package.
type Package struct {
	ID int64 `json:"id"`
	Meta
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	Status     string    `json:"status"`
	UploadedBy string    `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
	// InManifest reports whether the current signed manifest lists it.
	InManifest bool `json:"in_manifest"`
}

var safeFile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,199}$`)

func clean(list []string) []string {
	out := []string{}
	for _, v := range list {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func (m *Meta) normalize() error {
	m.File = strings.TrimSpace(m.File)
	m.Version = strings.TrimSpace(m.Version)
	m.Type = strings.TrimSpace(m.Type)
	m.FwMin, m.FwMax = strings.TrimSpace(m.FwMin), strings.TrimSpace(m.FwMax)
	m.Models = clean(m.Models)
	m.UpgradeFrom = clean(m.UpgradeFrom)
	if len(m.Models) == 0 {
		m.Models = []string{"*"}
	}
	if m.ReleasedAt.IsZero() {
		m.ReleasedAt = time.Now().UTC()
	}
	if !safeFile.MatchString(m.File) {
		return invalidf("file name may contain only letters, digits, dot, dash, underscore and plus")
	}
	// Reuse the manifest's own validation with a placeholder hash and size.
	mf := manifest.Manifest{Format: manifest.FormatVersion, PublishedAt: time.Now(), Packages: []manifest.Package{m.pkg(strings.Repeat("0", 64), 1)}}
	if err := mf.Validate(); err != nil {
		msg := strings.TrimPrefix(err.Error(), "manifest: invalid: ")
		return invalidf("%s", strings.ReplaceAll(msg, "packages[0]: ", ""))
	}
	return nil
}

func (m Meta) pkg(sha string, size int64) manifest.Package {
	return manifest.Package{
		File: m.File, SHA256: sha, Size: size, Platform: licformat.Platform(m.Platform), Type: manifest.PackageType(m.Type),
		Version: m.Version, Models: m.Models, Firmware: manifest.FirmwareRange{Min: m.FwMin, Max: m.FwMax},
		ReleasedAt: m.ReleasedAt.UTC().Truncate(time.Second), UpgradeFrom: m.UpgradeFrom, Notes: m.Notes,
	}
}

// Service manages the repository.
type Service struct {
	db  *pgxpool.Pool
	dir string
	key *signer.Key
	now func() time.Time
}

// New returns the service. Files live in dir/files; key may be nil, which
// disables publishing.
func New(db *pgxpool.Pool, dir string, key *signer.Key) *Service {
	return &Service{db: db, dir: dir, key: key, now: time.Now}
}

// KeyID returns the signing key id, or "".
func (s *Service) KeyID() string {
	if s.key == nil {
		return ""
	}
	return s.key.ID
}

func (s *Service) filePath(sha string) string { return filepath.Join(s.dir, "files", sha[:2], sha) }

// Upload stores a file with its description as a draft package.
func (s *Service) Upload(ctx context.Context, r io.Reader, m Meta, userID int64) (*Package, error) {
	if err := m.normalize(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "tmp"), 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "upload-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, MaxFileSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, invalidf("the file is empty")
	}
	if n > MaxFileSize {
		return nil, invalidf("the file is larger than %d GB", MaxFileSize>>30)
	}
	sha := hex.EncodeToString(h.Sum(nil))
	var existing string
	err = s.db.QueryRow(ctx, `SELECT file FROM packages WHERE sha256 = $1`, sha).Scan(&existing)
	if err == nil {
		return nil, invalidf("this file is already in the repository as %s", existing)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	dst := s.filePath(sha)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return nil, err
	}
	var id int64
	err = s.db.QueryRow(ctx, `INSERT INTO packages (file, sha256, size, platform, type, version, models, fw_min, fw_max, upgrade_from, notes, released_at, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
		m.File, sha, n, m.Platform, m.Type, m.Version, m.Models, m.FwMin, m.FwMax, m.UpgradeFrom, m.Notes, m.ReleasedAt, nullID(userID)).Scan(&id)
	if err != nil {
		_ = os.Remove(dst)
		return nil, mapErr(err)
	}
	return s.Get(ctx, id)
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

func mapErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "packages_file_key" {
		return invalidf("another package already uses this file name")
	}
	return err
}

// Update changes a package's description.
func (s *Service) Update(ctx context.Context, id int64, m Meta) error {
	if err := m.normalize(); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE packages SET file = $2, platform = $3, type = $4, version = $5, models = $6, fw_min = $7,
		fw_max = $8, upgrade_from = $9, notes = $10, released_at = $11 WHERE id = $1`,
		id, m.File, m.Platform, m.Type, m.Version, m.Models, m.FwMin, m.FwMax, m.UpgradeFrom, m.Notes, m.ReleasedAt)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetStatus marks a package draft, published or withdrawn. The change
// reaches installations with the next Publish.
func (s *Service) SetStatus(ctx context.Context, id int64, status string) error {
	if status != "draft" && status != "published" && status != "withdrawn" {
		return invalidf("unknown status")
	}
	tag, err := s.db.Exec(ctx, `UPDATE packages SET status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const pkgCols = `p.id, p.file, p.platform, p.type, p.version, p.models, p.fw_min, p.fw_max, p.upgrade_from, p.notes, p.released_at,
	p.sha256, p.size, p.status, coalesce(u.email, ''), p.created_at`

func scanPkg(row pgx.Row) (*Package, error) {
	var p Package
	err := row.Scan(&p.ID, &p.File, &p.Platform, &p.Type, &p.Version, &p.Models, &p.FwMin, &p.FwMax, &p.UpgradeFrom, &p.Notes,
		&p.ReleasedAt, &p.SHA256, &p.Size, &p.Status, &p.UploadedBy, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// Get returns one package.
func (s *Service) Get(ctx context.Context, id int64) (*Package, error) {
	p, err := scanPkg(s.db.QueryRow(ctx, `SELECT `+pkgCols+` FROM packages p LEFT JOIN users u ON u.id = p.uploaded_by WHERE p.id = $1`, id))
	if err != nil {
		return nil, err
	}
	cur, _ := s.currentSet(ctx)
	p.InManifest = cur[p.SHA256]
	return p, nil
}

// List returns packages, newest first.
func (s *Service) List(ctx context.Context, platform, typ string) ([]*Package, error) {
	rows, err := s.db.Query(ctx, `SELECT `+pkgCols+` FROM packages p LEFT JOIN users u ON u.id = p.uploaded_by
		WHERE ($1 = '' OR p.platform = $1) AND ($2 = '' OR p.type = $2) ORDER BY p.released_at DESC, p.id DESC`, platform, typ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cur, err := s.currentSet(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Package{}
	for rows.Next() {
		p, err := scanPkg(rows)
		if err != nil {
			return nil, err
		}
		p.InManifest = cur[p.SHA256]
		out = append(out, p)
	}
	return out, rows.Err()
}

// currentSet returns the hashes listed in the current manifest.
func (s *Service) currentSet(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	body, _, _, err := s.Current(ctx)
	if errors.Is(err, ErrNotFound) {
		return out, nil
	} else if err != nil {
		return nil, err
	}
	var m manifest.Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	for _, p := range m.Packages {
		out[p.SHA256] = true
	}
	return out, nil
}

// ManifestInfo describes a published manifest.
type ManifestInfo struct {
	ID           int64     `json:"id"`
	PublishedAt  time.Time `json:"published_at"`
	KID          string    `json:"kid"`
	PackageCount int       `json:"package_count"`
	PublishedBy  string    `json:"published_by"`
}

// Publish signs a new manifest listing every published package.
func (s *Service) Publish(ctx context.Context, userID int64) (*ManifestInfo, error) {
	if s.key == nil {
		return nil, ErrNoKey
	}
	rows, err := s.db.Query(ctx, `SELECT `+pkgCols+` FROM packages p LEFT JOIN users u ON u.id = p.uploaded_by
		WHERE p.status = 'published' ORDER BY p.platform, p.type, p.released_at`)
	if err != nil {
		return nil, err
	}
	pkgs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Package, error) { return scanPkg(r) })
	if err != nil {
		return nil, err
	}
	mf := manifest.Manifest{Format: manifest.FormatVersion, PublishedAt: s.now().UTC().Truncate(time.Second), Packages: []manifest.Package{}}
	for _, p := range pkgs {
		mf.Packages = append(mf.Packages, p.Meta.pkg(p.SHA256, p.Size))
	}
	body, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return nil, err
	}
	sig, err := manifest.Sign(body, s.key.ID, s.key.Private)
	if err != nil {
		return nil, err
	}
	var id int64
	if err := s.db.QueryRow(ctx, `INSERT INTO manifests (published_at, body, signature, kid, package_count, published_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, mf.PublishedAt, body, sig, s.key.ID, len(mf.Packages), nullID(userID)).Scan(&id); err != nil {
		return nil, err
	}
	return &ManifestInfo{ID: id, PublishedAt: mf.PublishedAt, KID: s.key.ID, PackageCount: len(mf.Packages)}, nil
}

// Current returns the newest manifest and its signature.
func (s *Service) Current(ctx context.Context) (body []byte, sig string, at time.Time, err error) {
	err = s.db.QueryRow(ctx, `SELECT body, signature, published_at FROM manifests ORDER BY id DESC LIMIT 1`).Scan(&body, &sig, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// Manifests lists published manifests, newest first.
func (s *Service) Manifests(ctx context.Context) ([]ManifestInfo, error) {
	rows, err := s.db.Query(ctx, `SELECT m.id, m.published_at, m.kid, m.package_count, coalesce(u.email, '')
		FROM manifests m LEFT JOIN users u ON u.id = m.published_by ORDER BY m.id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ManifestInfo])
}

// Open returns a package file for download. Only files listed in the current
// manifest are served to installations.
func (s *Service) Open(ctx context.Context, sha string, published bool) (*os.File, *Package, error) {
	if len(sha) != 64 {
		return nil, nil, ErrNotFound
	}
	p, err := scanPkg(s.db.QueryRow(ctx, `SELECT `+pkgCols+` FROM packages p LEFT JOIN users u ON u.id = p.uploaded_by WHERE p.sha256 = $1`, sha))
	if err != nil {
		return nil, nil, err
	}
	if published {
		cur, err := s.currentSet(ctx)
		if err != nil {
			return nil, nil, err
		}
		if !cur[sha] {
			return nil, nil, ErrNotFound
		}
	}
	f, err := os.Open(s.filePath(sha))
	if err != nil {
		return nil, nil, err
	}
	return f, p, nil
}

// WriteBundle writes a zip with the current manifest, its signature and the
// package files under files/ (only those of platform, when given). The
// manifest stays complete so its signature remains valid.
func (s *Service) WriteBundle(ctx context.Context, w io.Writer, platform string) error {
	body, sig, _, err := s.Current(ctx)
	if err != nil {
		return err
	}
	var m manifest.Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	add := func(name string, r io.Reader) error {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: s.now()})
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, r)
		return err
	}
	if err := add("manifest.json", strings.NewReader(string(body))); err != nil {
		return err
	}
	if err := add("manifest.sig", strings.NewReader(sig+"\n")); err != nil {
		return err
	}
	for _, p := range m.Packages {
		if platform != "" && string(p.Platform) != platform {
			continue
		}
		f, err := os.Open(s.filePath(p.SHA256))
		if err != nil {
			return err
		}
		err = add("files/"+p.File, f)
		f.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

// Authorize checks a signed installation request: the installation must hold
// the named, unrevoked license with at least one device subscription that
// is active or in grace. It returns the license's database id.
func (s *Service) Authorize(ctx context.Context, header, method, path string) (int64, string, error) {
	c, err := repoauth.Parse(header)
	if err != nil {
		return 0, "", ErrUnauthorized
	}
	var pub []byte
	var licID int64
	var entitled bool
	err = s.db.QueryRow(ctx, `
		SELECT i.pubkey, l.id, EXISTS (SELECT 1 FROM license_items it WHERE it.license_id = l.id AND it.kind = 'device' AND it.active
		                                AND it.expires_on + l.grace_days >= current_date)
		FROM licenses l JOIN installations i ON i.id = l.installation_id
		WHERE l.code = $1 AND l.installation_id = $2 AND l.status = 'active' AND i.status = 'active'`,
		c.LicenseID, c.InstallationID).Scan(&pub, &licID, &entitled)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrUnauthorized
	} else if err != nil {
		return 0, "", err
	}
	if err := c.Verify(pub, method, path, s.now()); err != nil || !entitled {
		return 0, "", ErrUnauthorized
	}
	return licID, c.InstallationID, nil
}

// LogDownload records an installation download.
func (s *Service) LogDownload(ctx context.Context, licID int64, installationID, object, ip string) {
	_, _ = s.db.Exec(ctx, `INSERT INTO repo_downloads (installation_id, license_id, object, ip) VALUES ($1, $2, $3, $4)`, installationID, licID, object, ip)
}
