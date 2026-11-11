package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/farshadfighter/taktaplus-lic/internal/auth"
	"github.com/farshadfighter/taktaplus-lic/internal/licensing"
	"github.com/farshadfighter/taktaplus-lic/internal/orgs"
)

// --- portal users, managed by vendor staff and by resellers ---

type newUser struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// portalRole is the role of users of a customer or reseller organization.
func portalRole(kind string) (auth.Role, bool) {
	switch kind {
	case orgs.KindCustomer:
		return auth.RoleCustomer, true
	case orgs.KindReseller:
		return auth.RoleReseller, true
	}
	return "", false
}

func (s *srv) createOrgUser(w http.ResponseWriter, r *http.Request, org orgs.Org) {
	var in newUser
	if !decodeJSON(w, r, &in) {
		return
	}
	role, ok := portalRole(org.Kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "portal users belong to customers or resellers")
		return
	}
	if len(in.Password) < 12 {
		writeError(w, http.StatusBadRequest, "the initial password must be at least 12 characters")
		return
	}
	u, err := s.auth.CreateUser(r.Context(), org.ID, in.Email, in.Name, role, in.Password)
	if errors.Is(err, auth.ErrEmailTaken) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "user.create", "user", u.Email, map[string]any{"org": org.Code, "role": string(role)})
	writeJSON(w, http.StatusCreated, toUserJSON(u))
}

func (s *srv) listOrgUsers(w http.ResponseWriter, r *http.Request, org orgs.Org) {
	list, err := s.auth.OrgUsers(r.Context(), org.ID)
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *srv) setOrgUserActive(w http.ResponseWriter, r *http.Request, org orgs.Org) {
	uid, ok := pathID(w, r, "uid")
	if !ok {
		return
	}
	var in struct {
		Active bool `json:"active"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.auth.SetUserActive(r.Context(), org.ID, uid, in.Active); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "user.set_active", "user", strconv.FormatInt(uid, 10), map[string]any{"org": org.Code, "active": in.Active})
	w.WriteHeader(http.StatusNoContent)
}

// adminOrgUsers wraps a user handler for vendor staff: any customer or
// reseller.
func (s *srv) adminOrgUsers(h func(http.ResponseWriter, *http.Request, orgs.Org)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		o, err := orgs.Get(r.Context(), s.db, id)
		if err != nil {
			s.licenseError(w, r, err)
			return
		}
		sess, _ := sessionFrom(r.Context())
		need := auth.PermCustomersWrite
		if o.Kind == orgs.KindReseller {
			need = auth.PermResellersWrite
		}
		if r.Method == http.MethodGet {
			need = auth.PermCustomersRead
		}
		if !sess.User.Role.Can(need) {
			writeError(w, http.StatusForbidden, "permission denied")
			return
		}
		h(w, r, o)
	}
}

// --- reseller credit (vendor staff) ---

func (s *srv) resellerCredits(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	c, txs, err := s.lic.Credits(r.Context(), id, 50)
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": c, "transactions": txs})
}

func (s *srv) allResellerCredits(w http.ResponseWriter, r *http.Request) {
	m, err := s.lic.AllCredits(r.Context())
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	out := map[string]licensing.Credits{}
	for id, c := range m {
		out[strconv.FormatInt(id, 10)] = c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *srv) addResellerCredits(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		SerialYears int    `json:"serial_years"`
		UserYears   int    `json:"user_years"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	c, err := s.lic.AddCredits(r.Context(), actor(r), id, in.SerialYears, in.UserYears, in.Description)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// --- portals ---

type portalCtx struct {
	user auth.User
	org  orgs.Org
}

func (p portalCtx) reseller() bool { return p.user.Role == auth.RoleReseller }

// scope returns the owner a license must belong to.
func (p portalCtx) scope() (customerID, resellerID *int64) {
	id := p.org.ID
	if p.reseller() {
		return nil, &id
	}
	return &id, nil
}

func (s *srv) owns(ctx context.Context, p portalCtx, licenseID int64) error {
	c, r := p.scope()
	return s.lic.Owns(ctx, licenseID, c, r)
}

// portalActor is the audit actor for a portal change, limited to the
// user's own licenses.
func (p portalCtx) actor(r *http.Request) licensing.Actor {
	a := actor(r)
	a.Check = licensing.OwnedBy(p.scope())
	return a
}

// portal admits signed-in customer and reseller users of active
// organizations.
func (s *srv) portal(resellerOnly bool, h func(http.ResponseWriter, *http.Request, portalCtx)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := sessionFrom(r.Context())
		if !ok || !sess.MFAPassed {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		role := sess.User.Role
		if role != auth.RoleReseller && (resellerOnly || role != auth.RoleCustomer) {
			writeError(w, http.StatusForbidden, "permission denied")
			return
		}
		o, err := orgs.Get(r.Context(), s.db, sess.User.OrgID)
		if err != nil {
			s.licenseError(w, r, err)
			return
		}
		if !o.Active {
			writeError(w, http.StatusForbidden, "this account is disabled; contact your vendor")
			return
		}
		h(w, r, portalCtx{user: sess.User, org: o})
	}
}

// portalError answers licenses of other organizations as not found.
func (s *srv) portalError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, licensing.ErrNotYours) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.activationError(w, r, err)
}

// redact hides staff-only fields of a license from portal users.
func redact(d licensing.Detail) licensing.Detail {
	d.Notes, d.OrderRef = "", ""
	return d
}

func (s *srv) portalOverview(w http.ResponseWriter, r *http.Request, p portalCtx) {
	ctx := r.Context()
	cust, res := p.scope()
	list, err := s.lic.List(ctx, licensing.ListFilter{CustomerID: cust, ResellerID: res})
	if err != nil {
		internalError(s.log, w, r, err)
		return
	}
	out := map[string]any{"org": p.org, "licenses": list}
	if p.reseller() {
		c, txs, err := s.lic.Credits(ctx, p.org.ID, 20)
		if err != nil {
			internalError(s.log, w, r, err)
			return
		}
		customers, err := orgs.List(ctx, s.db, orgs.Filter{Kind: orgs.KindCustomer, ParentID: &p.org.ID})
		if err != nil {
			internalError(s.log, w, r, err)
			return
		}
		out["credits"], out["transactions"], out["customers"] = c, txs, customers
	} else {
		var details []licensing.Detail
		for _, l := range list {
			d, err := s.lic.Get(ctx, l.ID)
			if err != nil {
				internalError(s.log, w, r, err)
				return
			}
			details = append(details, redact(d))
		}
		out["details"] = details
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *srv) portalLicense(w http.ResponseWriter, r *http.Request, p portalCtx) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.owns(r.Context(), p, id); err != nil {
		s.portalError(w, r, err)
		return
	}
	d, err := s.lic.Get(r.Context(), id)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, redact(d))
}

func (s *srv) portalLicenseFile(w http.ResponseWriter, r *http.Request, p portalCtx) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	token, err := s.lic.IssueFile(r.Context(), p.actor(r), id)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"license": token})
}

func (s *srv) portalInspect(w http.ResponseWriter, r *http.Request, p portalCtx) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := s.lic.Inspect(r.Context(), req.Request)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	if in.LicenseID == nil || s.owns(r.Context(), p, *in.LicenseID) != nil {
		writeError(w, http.StatusUnprocessableEntity, "this request is for a license that is not on your account")
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *srv) portalActivate(w http.ResponseWriter, r *http.Request, p portalCtx) {
	var req requestBody
	if !decodeJSON(w, r, &req) {
		return
	}
	a := p.actor(r)
	token, code, err := s.lic.Activate(r.Context(), req.Request, licensing.MethodOffline, clientIP(r), &a)
	if errors.Is(err, licensing.ErrNotYours) {
		writeError(w, http.StatusUnprocessableEntity, "this request is for a license that is not on your account")
		return
	}
	if err != nil {
		s.activationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"license": token, "license_id": code})
}

// --- reseller portal ---

func (s *srv) ownCustomer(ctx context.Context, p portalCtx, id int64) (orgs.Org, error) {
	o, err := orgs.Get(ctx, s.db, id)
	if err != nil || o.Kind != orgs.KindCustomer || o.ParentID == nil || *o.ParentID != p.org.ID {
		return orgs.Org{}, licensing.ErrNotYours
	}
	return o, nil
}

func (s *srv) resellerCreateCustomer(w http.ResponseWriter, r *http.Request, p portalCtx) {
	var in orgs.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	in.ParentID, in.Tier = &p.org.ID, ""
	o, err := orgs.Create(r.Context(), s.db, orgs.KindCustomer, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "customer.create", "customer", o.Code, map[string]any{"name": o.Name, "reseller": p.org.Code})
	writeJSON(w, http.StatusCreated, o)
}

func (s *srv) resellerUpdateCustomer(w http.ResponseWriter, r *http.Request, p portalCtx) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := s.ownCustomer(r.Context(), p, id); err != nil {
		s.portalError(w, r, err)
		return
	}
	var in orgs.Input
	if !decodeJSON(w, r, &in) {
		return
	}
	in.ParentID, in.Tier = &p.org.ID, ""
	o, err := orgs.Update(r.Context(), s.db, id, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit(r, sess, "customer.update", "customer", o.Code, nil)
	writeJSON(w, http.StatusOK, o)
}

// resellerCustomerUsers lets a reseller manage its customers' portal users.
func (s *srv) resellerCustomerUsers(h func(http.ResponseWriter, *http.Request, orgs.Org)) http.HandlerFunc {
	return s.portal(true, func(w http.ResponseWriter, r *http.Request, p portalCtx) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		o, err := s.ownCustomer(r.Context(), p, id)
		if err != nil {
			s.portalError(w, r, err)
			return
		}
		h(w, r, o)
	})
}

func (s *srv) resellerIssue(w http.ResponseWriter, r *http.Request, p portalCtx) {
	var in licensing.ResellerOrder
	if !decodeJSON(w, r, &in) {
		return
	}
	d, key, err := s.lic.ResellerCreate(r.Context(), actor(r), p.org.ID, in)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"license": redact(d), "activation_key": key})
}

func (s *srv) resellerAdd(w http.ResponseWriter, r *http.Request, p portalCtx) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in licensing.ResellerOrder
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.lic.ResellerAdd(r.Context(), actor(r), p.org.ID, id, in); err != nil {
		s.portalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *srv) resellerRenew(w http.ResponseWriter, r *http.Request, p portalCtx) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		ItemIDs []int64 `json:"item_ids"`
		Years   int     `json:"years"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.lic.ResellerRenew(r.Context(), actor(r), p.org.ID, id, in.ItemIDs, in.Years); err != nil {
		s.portalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *srv) resellerRevealKey(w http.ResponseWriter, r *http.Request, p portalCtx) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.owns(r.Context(), p, id); err != nil {
		s.portalError(w, r, err)
		return
	}
	key, err := s.lic.RevealKey(r.Context(), actor(r), id)
	if err != nil {
		s.licenseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"activation_key": strings.TrimSpace(key)})
}
