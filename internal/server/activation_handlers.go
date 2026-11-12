package server

import (
	"errors"
	"net/http"

	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
)

type requestBody struct {
	Request string `json:"request"`
}

// activationError maps activation outcomes to responses an installation can
// show its administrator.
func (s *srv) activationError(w http.ResponseWriter, r *http.Request, err error) {
	var rej licensing.RejectError
	if errors.As(err, &rej) {
		writeError(w, http.StatusUnprocessableEntity, rej.Reason)
		return
	}
	s.licenseError(w, r, err)
}

// handleActivate is called by installations with a signed .tpreq.
func (s *srv) handleActivate(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	token, code, err := s.lic.Activate(r.Context(), req.Request, licensing.MethodOnline, clientIP(r), nil)
	if err != nil {
		s.activationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"license": token, "license_id": code})
}

// handleHeartbeat is the daily check-in of online installations.
func (s *srv) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.lic.Heartbeat(r.Context(), req.Request, clientIP(r))
	if err != nil {
		s.activationError(w, r, err)
		return
	}
	status := http.StatusOK
	if res.Status == "revoked" {
		status = http.StatusGone
	}
	writeJSON(w, status, res)
}

// handleInspect shows staff what an uploaded .tpreq contains.
func (s *srv) handleInspect(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := s.lic.Inspect(r.Context(), req.Request)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

// handleOfflineActivate issues a license for an uploaded .tpreq.
func (s *srv) handleOfflineActivate(w http.ResponseWriter, r *http.Request) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	a := actor(r)
	token, code, err := s.lic.Activate(r.Context(), req.Request, licensing.MethodOffline, clientIP(r), &a)
	if err != nil {
		s.activationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"license": token, "license_id": code})
}

func (s *srv) listInstallations(w http.ResponseWriter, r *http.Request) {
	list, err := s.lic.ListInstallations(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}
