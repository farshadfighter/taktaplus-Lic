package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/farshadfighter/taktaplus-lic/internal/repo"
)

func (s *srv) repoError(w http.ResponseWriter, r *http.Request, err error) {
	var ie repo.InputError
	switch {
	case errors.As(err, &ie):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, repo.ErrNoKey):
		writeError(w, http.StatusConflict, err.Error())
	default:
		internalError(s.log, w, r, err)
	}
}

func (s *srv) listPackages(w http.ResponseWriter, r *http.Request) {
	list, err := s.repo.List(r.Context(), r.URL.Query().Get("platform"), r.URL.Query().Get("type"))
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	manifests, err := s.repo.Manifests(r.Context())
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "manifests": manifests, "kid": s.repo.KeyID()})
}

// uploadPackage reads a multipart body whose "meta" part (JSON) precedes the
// "file" part, streaming the file to disk.
func (s *srv) uploadPackage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, repo.MaxFileSize+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}
	var meta *repo.Meta
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "upload failed: "+err.Error())
			return
		}
		switch part.FormName() {
		case "meta":
			var m repo.Meta
			if err := json.NewDecoder(io.LimitReader(part, 64<<10)).Decode(&m); err != nil {
				writeError(w, http.StatusBadRequest, "invalid package description")
				return
			}
			meta = &m
		case "file":
			if meta == nil {
				writeError(w, http.StatusBadRequest, "the meta part must come before the file")
				return
			}
			if meta.File == "" {
				meta.File = part.FileName()
			}
			sess, _ := sessionFrom(r.Context())
			p, err := s.repo.Upload(r.Context(), part, *meta, sess.User.ID)
			if err != nil {
				s.repoError(w, r, err)
				return
			}
			s.audit(r, sess, "package.upload", "package", strconv.FormatInt(p.ID, 10), map[string]any{"file": p.File, "sha256": p.SHA256, "type": p.Type, "version": p.Version})
			writeJSON(w, http.StatusCreated, p)
			return
		}
	}
	writeError(w, http.StatusBadRequest, "no file in the upload")
}

func (s *srv) updatePackage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var m repo.Meta
	if !decodeJSON(w, r, &m) {
		return
	}
	if err := s.repo.Update(r.Context(), id, m); err != nil {
		s.repoError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "package.update", "package", strconv.FormatInt(id, 10), map[string]any{"file": m.File, "version": m.Version})
	w.WriteHeader(http.StatusNoContent)
}

func (s *srv) setPackageStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.repo.SetStatus(r.Context(), id, req.Status); err != nil {
		s.repoError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "package.status", "package", strconv.FormatInt(id, 10), map[string]any{"status": req.Status})
	w.WriteHeader(http.StatusNoContent)
}

func (s *srv) publishManifest(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	info, err := s.repo.Publish(r.Context(), sess.User.ID)
	if err != nil {
		s.repoError(w, r, err)
		return
	}
	s.audit(r, sess, "manifest.publish", "manifest", strconv.FormatInt(info.ID, 10), map[string]any{"packages": info.PackageCount, "kid": info.KID})
	writeJSON(w, http.StatusOK, info)
}

// downloadPackageAdmin serves any stored file to staff.
func (s *srv) downloadPackageAdmin(w http.ResponseWriter, r *http.Request) {
	f, p, err := s.repo.Open(r.Context(), chi.URLParam(r, "sha"), false)
	if err != nil {
		s.repoError(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", `attachment; filename="`+p.File+`"`)
	http.ServeContent(w, r, p.File, p.CreatedAt, f)
}

func (s *srv) downloadBundle(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	if _, _, _, err := s.repo.Current(r.Context()); err != nil {
		s.repoError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "repo.bundle", "manifest", "", map[string]any{"platform": platform})
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="taktaplus-packages-`+time.Now().UTC().Format("20060102")+`.zip"`)
	if err := s.repo.WriteBundle(r.Context(), w, platform); err != nil {
		s.log.Error("write bundle", "err", err) // headers are sent; the client sees a truncated zip
	}
}

// --- installation endpoints (signed with the installation key) ---

func (s *srv) repoAuth(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	licID, inst, err := s.repo.Authorize(r.Context(), r.Header.Get("Authorization"), r.Method, r.URL.Path)
	if err != nil {
		if errors.Is(err, repo.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, err.Error())
		} else {
			internalError(s.log, w, r, err)
		}
		return 0, "", false
	}
	return licID, inst, true
}

func (s *srv) repoManifest(sig bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		licID, inst, ok := s.repoAuth(w, r)
		if !ok {
			return
		}
		body, signature, at, err := s.repo.Current(r.Context())
		if err != nil {
			s.repoError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Last-Modified", at.UTC().Format(http.TimeFormat))
		if sig {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, signature+"\n")
			return
		}
		s.repo.LogDownload(r.Context(), licID, inst, "manifest", clientIP(r))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func (s *srv) repoFile(w http.ResponseWriter, r *http.Request) {
	licID, inst, ok := s.repoAuth(w, r)
	if !ok {
		return
	}
	sha := chi.URLParam(r, "sha")
	f, p, err := s.repo.Open(r.Context(), sha, true)
	if err != nil {
		s.repoError(w, r, err)
		return
	}
	defer f.Close()
	s.repo.LogDownload(r.Context(), licID, inst, sha, clientIP(r))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, p.File, p.CreatedAt, f)
}
