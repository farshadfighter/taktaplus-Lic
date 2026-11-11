package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/farshadfighter/taktaplus-lic/internal/auth"
	"github.com/farshadfighter/taktaplus-lic/pkg/selfbackup"
	"github.com/farshadfighter/taktaplus-lic/pkg/sysadmin"
)

func (s *srv) systemError(w http.ResponseWriter, r *http.Request, err error) {
	var ie sysadmin.InputError
	switch {
	case errors.As(err, &ie):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, sysadmin.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, sysadmin.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	default:
		internalError(s.log, w, r, err)
	}
}

func (s *srv) systemOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := s.system.Settings(ctx)
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	runs, err := s.system.Runs(ctx)
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	ups, err := s.system.Upgrades(ctx)
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"info": s.system.Info(), "settings": st, "runs": runs, "upgrades": ups})
}

func (s *srv) saveSystemBackupSettings(w http.ResponseWriter, r *http.Request) {
	var in sysadmin.Settings
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.system.SaveSettings(r.Context(), in); err != nil {
		s.systemError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "system_backup.settings", "system", "backup", map[string]any{"enabled": in.Enabled, "cron": in.Cron, "keep": in.Keep, "sftp": in.SFTPEnabled, "sftp_host": in.SFTP.Host})
	w.WriteHeader(http.StatusNoContent)
}

func (s *srv) setSystemBackupPassphrase(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Passphrase string `json:"passphrase"`
		Password   string `json:"password"`
		Code       string `json:"code"`
	}
	if !decodeJSON(w, r, &in) || !s.reauth(w, r, in.Password, in.Code) {
		return
	}
	if err := s.system.SetPassphrase(r.Context(), in.Passphrase); err != nil {
		s.systemError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "system_backup.passphrase", "system", "backup", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *srv) testSystemSFTP(w http.ResponseWriter, r *http.Request) {
	var in selfbackup.SFTP
	if !decodeJSON(w, r, &in) {
		return
	}
	key, err := s.system.TestSFTP(r.Context(), in)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "host_key": key})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "host_key": key})
}

func (s *srv) startSystemBackup(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	id, err := s.system.Start(r.Context(), "manual", sess.User.Email)
	if err != nil {
		s.systemError(w, r, err)
		return
	}
	s.audit(r, sess, "system_backup.run", "system_backup", strconv.FormatInt(id, 10), nil)
	writeJSON(w, http.StatusAccepted, map[string]int64{"id": id})
}

func (s *srv) downloadSystemBackup(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("file")
	f, err := s.system.File(name)
	if err != nil {
		s.systemError(w, r, err)
		return
	}
	defer f.Close()
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "system_backup.download", "system", name, nil)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(name)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if st, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	}
	_, _ = io.Copy(w, f)
}

func (s *srv) uploadUpgrade(w http.ResponseWriter, r *http.Request) {
	var meta struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	part, _, ok := filePart(w, r, &meta)
	if !ok {
		return
	}
	if !s.reauth(w, r, meta.Password, meta.Code) {
		return
	}
	sess, _ := sessionFrom(r.Context())
	u, _, err := s.system.Stage(r.Context(), part, sess.User.Email, true)
	if err != nil {
		s.systemError(w, r, err)
		return
	}
	s.audit(r, sess, "system.upgrade_upload", "system_upgrade", strconv.FormatInt(u.ID, 10), map[string]any{"version": u.Version, "from": u.FromVersion})
	writeJSON(w, http.StatusCreated, u)
}

func (s *srv) cancelUpgrade(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.system.Cancel(r.Context(), id); err != nil {
		s.systemError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "system.upgrade_cancel", "system_upgrade", strconv.FormatInt(id, 10), nil)
	w.WriteHeader(http.StatusNoContent)
}

// reauth checks the password and TOTP code sent with a sensitive request.
func (s *srv) reauth(w http.ResponseWriter, r *http.Request, password, code string) bool {
	sess, _ := sessionFrom(r.Context())
	err := s.auth.Reauthenticate(r.Context(), sess.User.ID, password, code)
	switch {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrMFASetupState):
		writeError(w, http.StatusForbidden, "the password or code is not correct")
	case errors.Is(err, auth.ErrLocked):
		writeError(w, http.StatusForbidden, "the account is locked for a while after failed attempts")
	default:
		internalError(s.log, w, r, err)
	}
	return false
}

// filePart returns the "file" part of a multipart upload after an optional
// "meta" JSON part.
func filePart(w http.ResponseWriter, r *http.Request, meta any) (io.Reader, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a multipart upload")
		return nil, "", false
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			writeError(w, http.StatusBadRequest, "no file in the upload")
			return nil, "", false
		}
		switch part.FormName() {
		case "meta":
			if meta != nil {
				if err := json.NewDecoder(io.LimitReader(part, 64<<10)).Decode(meta); err != nil {
					writeError(w, http.StatusBadRequest, "invalid request")
					return nil, "", false
				}
			}
		case "file":
			return part, part.FileName(), true
		}
	}
}

func sanitizeFilename(s string) string {
	out := make([]rune, 0, len(s))
	for _, c := range s {
		if c == '"' || c == '\\' || c == '/' || c < 0x20 {
			c = '_'
		}
		out = append(out, c)
	}
	return string(out)
}
