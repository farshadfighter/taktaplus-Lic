package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
)

// actor describes the signed-in user for audit records.
func actor(r *http.Request) licensing.Actor {
	sess, _ := sessionFrom(r.Context())
	id := sess.User.ID
	return licensing.Actor{UserID: &id, Name: sess.User.Email, IP: clientIP(r), Source: portalSource(sess.User.Role)}
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not found")
		return 0, false
	}
	return id, true
}

func queryID(r *http.Request, name string) *int64 {
	v, err := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

// licenseError maps service errors to HTTP responses.
func (s *srv) licenseError(w http.ResponseWriter, r *http.Request, err error) {
	var in licensing.InputError
	switch {
	case errors.As(err, &in):
		writeError(w, http.StatusBadRequest, in.Error())
	case errors.Is(err, licensing.ErrNotFound), errors.Is(err, orgs.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, licensing.ErrSerialInUse), errors.Is(err, licensing.ErrRevoked), errors.Is(err, licensing.ErrNoInstallation):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, licensing.ErrNoSigningKey):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		internalError(s.log, w, r, err)
	}
}

// --- organizations ---

func (s *srv) listOrgs(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := orgs.List(r.Context(), s.db, orgs.Filter{Kind: kind, Query: r.URL.Query().Get("q"), ParentID: queryID(r, "reseller_id")})
		if err != nil {
			internalError(s.log, w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

func (s *srv) createOrg(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in orgs.Input
		if !decodeJSON(w, r, &in) {
			return
		}
		o, err := orgs.Create(r.Context(), s.db, kind, in)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sess, _ := sessionFrom(r.Context())
		s.audit(r, sess, kind+".create", kind, o.Code, map[string]any{"name": o.Name})
		writeJSON(w, http.StatusCreated, o)
	}
}

func (s *srv) getOrg(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	o, err := orgs.Get(r.Context(), s.db, id)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *srv) updateOrg(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in orgs.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	o, err := orgs.Update(r.Context(), s.db, id, in)
	if errors.Is(err, orgs.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, o.Kind+".update", o.Kind, o.Code, nil)
	writeJSON(w, http.StatusOK, o)
}

// --- licenses ---

func (s *srv) listLicenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("expiring_in"))
	list, err := s.lic.List(r.Context(), licensing.ListFilter{
		Query: q.Get("q"), Status: q.Get("status"), CustomerID: queryID(r, "customer_id"), ResellerID: queryID(r, "reseller_id"), ExpiringIn: days,
	})
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *srv) createLicense(w http.ResponseWriter, r *http.Request) {
	var in licensing.CreateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	d, key, err := s.lic.Create(r.Context(), actor(r), in)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"license": d, "activation_key": key})
}

func (s *srv) getLicense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := s.lic.Get(r.Context(), id)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// licenseAction runs a mutating operation and answers with the fresh detail.
func (s *srv) licenseAction(do func(r *http.Request, id int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		if err := do(r, id); err != nil {
			s.licenseError(w, r, err)
			return
		}
		s.getLicense(w, r)
	}
}

func (s *srv) addItems(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []licensing.ItemInput `json:"items"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	s.licenseAction(func(r *http.Request, id int64) error { return s.lic.AddItems(r.Context(), actor(r), id, req.Items) })(w, r)
}

func (s *srv) renewItems(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemIDs   []int64 `json:"item_ids"`
		ExpiresOn string  `json:"expires_on"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	s.licenseAction(func(r *http.Request, id int64) error {
		return s.lic.Renew(r.Context(), actor(r), id, req.ItemIDs, req.ExpiresOn)
	})(w, r)
}

func (s *srv) replaceSerial(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemID    int64  `json:"item_id"`
		NewSerial string `json:"new_serial"`
		Reason    string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	s.licenseAction(func(r *http.Request, id int64) error {
		return s.lic.ReplaceSerial(r.Context(), actor(r), id, req.ItemID, req.NewSerial, req.Reason)
	})(w, r)
}

func (s *srv) revokeLicense(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	s.licenseAction(func(r *http.Request, id int64) error { return s.lic.Revoke(r.Context(), actor(r), id, req.Reason) })(w, r)
}

func (s *srv) releaseInstallation(w http.ResponseWriter, r *http.Request) {
	s.licenseAction(func(r *http.Request, id int64) error { return s.lic.ReleaseInstallation(r.Context(), actor(r), id) })(w, r)
}

func (s *srv) revealKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	key, err := s.lic.RevealKey(r.Context(), actor(r), id)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"activation_key": key})
}

func (s *srv) downloadLicense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	token, err := s.lic.IssueFile(r.Context(), actor(r), id)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="license.tplic"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(token + "\n"))
}
