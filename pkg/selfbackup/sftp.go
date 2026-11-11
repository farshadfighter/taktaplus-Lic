package selfbackup

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTP is a remote copy destination.
type SFTP struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Dir        string `json:"dir"`
	HostKey    string `json:"host_key"` // "<type> <base64>", pinned on first use
}

// HostKeyLine formats a host key for pinning.
func HostKeyLine(k ssh.PublicKey) string {
	return k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal())
}

func (s SFTP) addr() string {
	port := s.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(s.Host, strconv.Itoa(port))
}

// connect opens a session; with an empty HostKey it accepts the key and
// returns it so the caller can pin it.
func (s SFTP) connect(ctx context.Context) (*sftp.Client, *ssh.Client, string, error) {
	var methods []ssh.AuthMethod
	if s.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(s.PrivateKey))
		if err != nil {
			return nil, nil, "", fmt.Errorf("SFTP private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if s.Password != "" {
		methods = append(methods, ssh.Password(s.Password))
	}
	if len(methods) == 0 {
		return nil, nil, "", errors.New("SFTP needs a password or a private key")
	}
	var seen string
	cfg := &ssh.ClientConfig{User: s.User, Auth: methods, Timeout: 20 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = HostKeyLine(key)
			if s.HostKey != "" && s.HostKey != seen {
				return errors.New("the SFTP server's host key has changed")
			}
			return nil
		}}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr())
	if err != nil {
		return nil, nil, "", err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c, chans, reqs, err := ssh.NewClientConn(conn, s.addr(), cfg)
	if err != nil {
		conn.Close()
		return nil, nil, "", err
	}
	client := ssh.NewClient(c, chans, reqs)
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, nil, "", err
	}
	return sc, client, seen, nil
}

// Test connects and checks the directory is writable; it returns the
// server's host key.
func (s SFTP) Test(ctx context.Context) (string, error) {
	sc, client, key, err := s.connect(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()
	defer sc.Close()
	p := path.Join(s.Dir, ".taktaplus-test")
	f, err := sc.Create(p)
	if err != nil {
		return key, fmt.Errorf("cannot write to %s: %w", s.Dir, err)
	}
	f.Close()
	_ = sc.Remove(p)
	return key, nil
}

// Upload copies r to name in the directory and keeps the newest keep files
// whose names start with prefix (keep 0 keeps all).
func (s SFTP) Upload(ctx context.Context, name string, r io.Reader, prefix string, keep int) error {
	sc, client, _, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sc.Close()
	tmp := path.Join(s.Dir, "."+name+".part")
	f, err := sc.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		_ = sc.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := sc.PosixRename(tmp, path.Join(s.Dir, name)); err != nil {
		if err := sc.Rename(tmp, path.Join(s.Dir, name)); err != nil {
			return err
		}
	}
	if keep <= 0 {
		return nil
	}
	list, err := sc.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, fi := range list {
		if !fi.IsDir() && strings.HasPrefix(fi.Name(), prefix) && strings.HasSuffix(fi.Name(), ".tpbak") {
			names = append(names, fi.Name())
		}
	}
	sort.Strings(names) // names carry a sortable timestamp
	for i := 0; i < len(names)-keep; i++ {
		_ = sc.Remove(path.Join(s.Dir, names[i]))
	}
	return nil
}
