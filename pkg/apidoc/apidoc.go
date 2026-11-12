// Package apidoc builds the OpenAPI 3.1 document of a product's REST API
// from a route table the product keeps next to its router.
package apidoc

import (
	"regexp"
	"sort"
	"strings"
)

// Access says how a route may be called.
type Access string

// Route access levels.
const (
	Public    Access = "public"     // no sign-in
	Session   Access = "session"    // panel session or API token
	PanelOnly Access = "panel_only" // panel session with password and code again; tokens are refused
	Install   Access = "install"    // signed by an installation (license server)
)

// Route documents one endpoint.
type Route struct {
	Method  string `json:"method"`
	Path    string `json:"path"` // relative to the API base, e.g. /devices/{id}
	Tag     string `json:"tag"`
	Summary string `json:"summary"`
	Perm    string `json:"perm,omitempty"`
	Access  Access `json:"access"`
}

// Key identifies a route as "METHOD /path".
func (r Route) Key() string { return r.Method + " " + r.Path }

// Info describes the API.
type Info struct {
	Title       string
	Version     string
	Description string
	Base        string // e.g. /api/v1
	Tags        map[string]string
}

var param = regexp.MustCompile(`\{([^}]+)\}`)

// Build returns the OpenAPI document.
func Build(info Info, routes []Route) map[string]any {
	paths := map[string]any{}
	tags := map[string]bool{}
	for _, r := range routes {
		item, _ := paths[r.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[r.Path] = item
		}
		tags[r.Tag] = true
		op := map[string]any{
			"tags":        []string{r.Tag},
			"summary":     r.Summary,
			"operationId": operationID(r),
			"responses": map[string]any{
				"200": map[string]any{"description": "OK", "content": jsonContent()},
				"400": ref("Error"), "401": ref("Error"), "403": ref("Error"), "404": ref("Error"),
			},
			"x-access": string(r.Access),
		}
		if r.Perm != "" {
			op["x-permission"] = r.Perm
		}
		desc := []string{}
		if r.Perm != "" {
			desc = append(desc, "Permission: `"+r.Perm+"`.")
		}
		switch r.Access {
		case Public:
			op["security"] = []any{}
			desc = append(desc, "No sign-in needed.")
		case PanelOnly:
			op["security"] = []any{map[string]any{"session": []string{}}}
			desc = append(desc, "Panel only: needs the password and authenticator code again; API tokens are refused.")
		case Install:
			op["security"] = []any{}
			desc = append(desc, "Called by installations with their signed request.")
		}
		if len(desc) > 0 {
			op["description"] = strings.Join(desc, " ")
		}
		var params []any
		for _, m := range param.FindAllStringSubmatch(r.Path, -1) {
			schema := map[string]any{"type": "string"}
			if m[1] == "id" || strings.HasSuffix(m[1], "ID") || strings.HasSuffix(m[1], "_id") {
				schema = map[string]any{"type": "integer", "format": "int64"}
			}
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": schema})
		}
		if params != nil {
			op["parameters"] = params
		}
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			op["requestBody"] = map[string]any{"required": false, "content": jsonContent()}
		}
		item[strings.ToLower(r.Method)] = op
	}
	tagList := []any{}
	names := make([]string, 0, len(tags))
	for t := range tags {
		names = append(names, t)
	}
	sort.Strings(names)
	for _, t := range names {
		tag := map[string]any{"name": t}
		if d := info.Tags[t]; d != "" {
			tag["description"] = d
		}
		tagList = append(tagList, tag)
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": info.Title, "version": info.Version, "description": info.Description},
		"servers": []any{map[string]any{"url": info.Base}},
		"tags":    tagList,
		"paths":   paths,
		"security": []any{
			map[string]any{"bearer": []string{}},
			map[string]any{"session": []string{}},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearer":  map[string]any{"type": "http", "scheme": "bearer", "description": "API token: tpk_<id>_<secret>"},
				"session": map[string]any{"type": "apiKey", "in": "cookie", "name": "session", "description": "Panel session; state-changing requests also need the header X-Requested-With: taktaplus"},
			},
			"schemas": map[string]any{
				"Error": map[string]any{"type": "object", "properties": map[string]any{"error": map[string]any{"type": "string"}}},
			},
			"responses": map[string]any{
				"Error": map[string]any{"description": "Error", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}},
			},
		},
	}
}

func jsonContent() map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}
}

func ref(name string) map[string]any { return map[string]any{"$ref": "#/components/responses/" + name} }

func operationID(r Route) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(r.Method))
	for _, part := range strings.FieldsFunc(r.Path, func(c rune) bool { return c == '/' || c == '-' || c == '.' || c == '_' }) {
		part = strings.Trim(part, "{}")
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// Diff compares documented routes with the router's "METHOD /path" keys and
// returns the undocumented and the stale ones.
func Diff(routes []Route, router []string) (missing, stale []string) {
	doc := map[string]bool{}
	for _, r := range routes {
		doc[r.Key()] = true
	}
	have := map[string]bool{}
	for _, k := range router {
		have[k] = true
		if !doc[k] {
			missing = append(missing, k)
		}
	}
	for k := range doc {
		if !have[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return missing, stale
}
