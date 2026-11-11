// Package selfbackup writes and restores encrypted backups of a TaktaPlus
// product: its PostgreSQL database and its data directory.
//
// File format (.tpbak):
//
//	"TPBAK1\n" | uint32 header length | header JSON | chunks
//
// Each chunk is uint32 length | XChaCha20-Poly1305 ciphertext of up to
// 1 MiB of a tar stream. The nonce is the header's 16-byte prefix followed by
// the big-endian chunk counter; the additional data is the header JSON and a
// final-chunk flag, so chunks cannot be reordered, dropped or truncated.
// The key is Argon2id(passphrase, salt) with the parameters in the header.
package selfbackup

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magic     = "TPBAK1\n"
	chunkSize = 1 << 20
)

// ErrPassphrase means the passphrase does not open the backup.
var ErrPassphrase = errors.New("the passphrase does not match this backup")

// KDF holds the Argon2id parameters and salt of a key.
type KDF struct {
	Salt    string `json:"salt"` // hex
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"` // KiB
	Threads uint8  `json:"threads"`
}

// Key is a derived backup key with what is needed to derive it again.
type Key struct {
	KDF      KDF
	Secret   [32]byte
	Verifier string // hex HMAC proving the passphrase without revealing it
}

func verifier(secret [32]byte) string {
	m := hmac.New(sha256.New, secret[:])
	m.Write([]byte("taktaplus-backup-verifier"))
	return hex.EncodeToString(m.Sum(nil))
}

// MinPassphrase is the shortest accepted passphrase.
const MinPassphrase = 12

// NewKey derives a key from a new passphrase with a random salt.
func NewKey(passphrase string) (*Key, error) {
	if len([]rune(passphrase)) < MinPassphrase {
		return nil, fmt.Errorf("the passphrase must be at least %d characters", MinPassphrase)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return DeriveKey(passphrase, KDF{Salt: hex.EncodeToString(salt), Time: 3, Memory: 64 * 1024, Threads: 2})
}

// DeriveKey derives the key of passphrase with the given parameters.
func DeriveKey(passphrase string, k KDF) (*Key, error) {
	salt, err := hex.DecodeString(k.Salt)
	if err != nil || len(salt) < 16 || k.Time == 0 || k.Time > 20 || k.Memory < 8*1024 || k.Memory > 1<<20 || k.Threads == 0 {
		return nil, errors.New("invalid key parameters")
	}
	var out Key
	out.KDF = k
	copy(out.Secret[:], argon2.IDKey([]byte(passphrase), salt, k.Time, k.Memory, k.Threads, 32))
	out.Verifier = verifier(out.Secret)
	return &out, nil
}

// Header is the clear-text part of a backup.
type Header struct {
	Product   string    `json:"product"`
	Version   string    `json:"version"`
	Hostname  string    `json:"hostname,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	KDF       KDF       `json:"kdf"`
	Verifier  string    `json:"verifier"`
	Nonce     string    `json:"nonce"` // hex, 16 bytes
}

type encWriter struct {
	w     io.Writer
	aead  interface{ Seal(dst, nonce, plaintext, ad []byte) []byte }
	ad    []byte
	nonce [24]byte
	n     uint64
	buf   []byte
}

// Encrypt writes the header and returns a writer that encrypts into w.
// Close must be called to write the final chunk.
func Encrypt(w io.Writer, key *Key, h Header) (io.WriteCloser, error) {
	aead, err := chacha20poly1305.NewX(key.Secret[:])
	if err != nil {
		return nil, err
	}
	var prefix [16]byte
	if _, err := rand.Read(prefix[:]); err != nil {
		return nil, err
	}
	h.KDF, h.Verifier, h.Nonce = key.KDF, key.Verifier, hex.EncodeToString(prefix[:])
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now().UTC()
	}
	hj, _ := json.Marshal(h)
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(hj)))
	for _, b := range [][]byte{[]byte(magic), l[:], hj} {
		if _, err := w.Write(b); err != nil {
			return nil, err
		}
	}
	e := &encWriter{w: w, aead: aead, ad: hj, buf: make([]byte, 0, chunkSize)}
	copy(e.nonce[:16], prefix[:])
	return e, nil
}

func (e *encWriter) seal(last bool) error {
	binary.BigEndian.PutUint64(e.nonce[16:], e.n)
	e.n++
	ad := append(append([]byte{}, e.ad...), 0)
	if last {
		ad[len(ad)-1] = 1
	}
	ct := e.aead.Seal(nil, e.nonce[:], e.buf, ad)
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(ct)))
	if _, err := e.w.Write(l[:]); err != nil {
		return err
	}
	_, err := e.w.Write(ct)
	e.buf = e.buf[:0]
	return err
}

func (e *encWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		k := min(chunkSize-len(e.buf), len(p))
		e.buf = append(e.buf, p[:k]...)
		p = p[k:]
		if len(e.buf) == chunkSize {
			if err := e.seal(false); err != nil {
				return 0, err
			}
		}
	}
	return n, nil
}

func (e *encWriter) Close() error { return e.seal(true) }

// ReadHeader reads the clear-text header of a backup.
func ReadHeader(r io.Reader) (Header, []byte, error) {
	var h Header
	m := make([]byte, len(magic))
	if _, err := io.ReadFull(r, m); err != nil || string(m) != magic {
		return h, nil, errors.New("not a TaktaPlus backup file")
	}
	var l [4]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return h, nil, err
	}
	n := binary.BigEndian.Uint32(l[:])
	if n > 64<<10 {
		return h, nil, errors.New("backup header is too large")
	}
	hj := make([]byte, n)
	if _, err := io.ReadFull(r, hj); err != nil {
		return h, nil, err
	}
	if err := json.Unmarshal(hj, &h); err != nil {
		return h, nil, fmt.Errorf("backup header: %w", err)
	}
	return h, hj, nil
}

type decReader struct {
	r     *bufio.Reader
	aead  interface{ Open(dst, nonce, ciphertext, ad []byte) ([]byte, error) }
	ad    []byte
	nonce [24]byte
	n     uint64
	buf   []byte
	done  bool
}

// Decrypt opens a backup with passphrase and returns its header and the
// decrypted tar stream. Reading returns an error if the file was altered or
// cut short.
func Decrypt(r io.Reader, passphrase string) (Header, io.Reader, error) {
	h, hj, err := ReadHeader(r)
	if err != nil {
		return h, nil, err
	}
	key, err := DeriveKey(passphrase, h.KDF)
	if err != nil {
		return h, nil, err
	}
	if !hmac.Equal([]byte(key.Verifier), []byte(h.Verifier)) {
		return h, nil, ErrPassphrase
	}
	return decryptWith(r, key, h, hj)
}

// DecryptWithKey is Decrypt with an already derived key.
func DecryptWithKey(r io.Reader, key *Key) (Header, io.Reader, error) {
	h, hj, err := ReadHeader(r)
	if err != nil {
		return h, nil, err
	}
	if !hmac.Equal([]byte(key.Verifier), []byte(h.Verifier)) {
		return h, nil, ErrPassphrase
	}
	return decryptWith(r, key, h, hj)
}

func decryptWith(r io.Reader, key *Key, h Header, hj []byte) (Header, io.Reader, error) {
	prefix, err := hex.DecodeString(h.Nonce)
	if err != nil || len(prefix) != 16 {
		return h, nil, errors.New("backup header: invalid nonce")
	}
	aead, err := chacha20poly1305.NewX(key.Secret[:])
	if err != nil {
		return h, nil, err
	}
	d := &decReader{r: bufio.NewReader(r), aead: aead, ad: hj}
	copy(d.nonce[:16], prefix)
	return h, d, nil
}

func (d *decReader) Read(p []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.done {
			return 0, io.EOF
		}
		var l [4]byte
		if _, err := io.ReadFull(d.r, l[:]); err != nil {
			return 0, errors.New("the backup file is cut short")
		}
		n := binary.BigEndian.Uint32(l[:])
		if n > chunkSize+64 {
			return 0, errors.New("the backup file is damaged")
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(d.r, ct); err != nil {
			return 0, errors.New("the backup file is cut short")
		}
		binary.BigEndian.PutUint64(d.nonce[16:], d.n)
		d.n++
		ad := append(append([]byte{}, d.ad...), 0)
		pt, err := d.aead.Open(nil, d.nonce[:], ct, ad)
		if err != nil {
			ad[len(ad)-1] = 1
			if pt, err = d.aead.Open(nil, d.nonce[:], ct, ad); err != nil {
				return 0, errors.New("the backup file is damaged or was altered")
			}
			d.done = true
			if _, err := d.r.Peek(1); err != io.EOF {
				return 0, errors.New("the backup file has data after its end")
			}
		}
		d.buf = pt
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}
