package server

import (
	"net/http"
	"sync"

	"github.com/farshadfighter/taktaplus-lic/pkg/apidoc"

	"github.com/farshadfighter/taktaplus-lic/internal/buildinfo"
)

// Tag descriptions shown in the API documentation.
var apiTags = map[string]string{
	"auth": "ورود به پنل", "me": "کاربر فعلی", "audit": "لاگ ممیزی", "tokens": "توکن‌های API", "orgs": "کاربران سازمان‌ها",
	"customers": "مشتریان", "resellers": "نمایندگان", "licenses": "لایسنس‌ها", "activations": "فعال‌سازی آفلاین",
	"installations": "نصب‌ها", "packages": "مخزن پکیج", "repo": "مخزن (برای نصب‌ها)", "portal": "پورتال مشتری و نماینده",
	"activate": "فعال‌سازی آنلاین (برای نصب‌ها)", "heartbeat": "Heartbeat (برای نصب‌ها)", "system": "بکاپ و ارتقای سیستم",
	"version": "نسخه", "openapi": "همین مستند",
}
var (
	openAPIOnce sync.Once
	openAPIDoc  map[string]any
)

// openAPI serves the OpenAPI document of this version.
func (s *srv) openAPI(w http.ResponseWriter, _ *http.Request) {
	openAPIOnce.Do(func() {
		openAPIDoc = apidoc.Build(apidoc.Info{
			Title:   "TaktaPlus License Server API",
			Version: buildinfo.Version,
			Description: "REST API of the TaktaPlus license server. Authenticate with an API token (Authorization: Bearer tpk_...) " +
				"created by an owner on the Tokens page; a token has a role, may be read-only (GET only) and may be limited to source addresses. " +
				"Actions marked panel_only need the password and authenticator code again and are refused for tokens.",
			Base: "/api/v1",
			Tags: apiTags,
		}, apiRoutes)
	})
	writeJSON(w, http.StatusOK, openAPIDoc)
}
