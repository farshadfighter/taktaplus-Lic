package apidoc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuild(t *testing.T) {
	routes := []Route{
		{Method: "GET", Path: "/devices/{id}", Tag: "devices", Summary: "Get a device", Perm: "devices.read", Access: Session},
		{Method: "POST", Path: "/devices/{id}/restore", Tag: "backups", Summary: "Restore", Perm: "backups.restore", Access: PanelOnly},
		{Method: "GET", Path: "/version", Tag: "system", Summary: "Version", Access: Public},
	}
	doc := Build(Info{Title: "T", Version: "1", Base: "/api/v1"}, routes)
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"openapi":"3.1.0"`, `"getDevicesId"`, `"x-permission":"devices.read"`, `"x-access":"panel_only"`, `"in":"path"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	missing, stale := Diff(routes, []string{"GET /devices/{id}", "GET /version", "DELETE /devices/{id}"})
	if len(missing) != 1 || missing[0] != "DELETE /devices/{id}" || len(stale) != 1 || stale[0] != "POST /devices/{id}/restore" {
		t.Fatalf("diff %v %v", missing, stale)
	}
}
