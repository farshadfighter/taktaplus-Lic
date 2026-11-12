package apitoken

import (
	"testing"
	"time"
)

func TestToken(t *testing.T) {
	id, value, hash, err := New()
	if err != nil {
		t.Fatal(err)
	}
	gid, secret, err := Parse(value)
	if err != nil || gid != id || !Match(secret, hash) {
		t.Fatalf("parse %q = %q %v", value, gid, err)
	}
	if Match(secret+"x", hash) {
		t.Fatal("matched a wrong secret")
	}
	if FromHeader("Bearer "+value) != value || FromHeader("Basic abc") != "" || FromHeader("Bearer xyz") != "" {
		t.Fatal("header parsing")
	}
	for _, bad := range []string{"", "tpk_", "tpk_zz_secret", "abc_" + value, "tpk_0011223344_short"} {
		if _, _, err := Parse(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestNets(t *testing.T) {
	nets, err := NormalizeNets([]string{" 10.1.2.3 ", "192.168.1.77/24", "2001:db8::/48", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 3 || nets[0] != "10.1.2.3/32" || nets[1] != "192.168.1.0/24" {
		t.Fatalf("nets %v", nets)
	}
	if _, err := NormalizeNets([]string{"10.0.0.0/33"}); err == nil {
		t.Fatal("bad network accepted")
	}
	for ip, want := range map[string]bool{"10.1.2.3": true, "10.1.2.4": false, "192.168.1.200:5555": true, "::ffff:192.168.1.9": true, "2001:db8:0:1::5": true, "bad": false} {
		if Allowed(nets, ip) != want {
			t.Fatalf("Allowed(%s) != %v", ip, want)
		}
	}
	if !Allowed(nil, "8.8.8.8") {
		t.Fatal("empty list must allow all")
	}
}

func TestExpiry(t *testing.T) {
	now := time.Now()
	if e, err := Expiry(0, now); err != nil || e != nil {
		t.Fatal("never")
	}
	if e, _ := Expiry(30, now); e == nil || e.Sub(now) != 30*24*time.Hour {
		t.Fatal("30 days")
	}
	if _, err := Expiry(400, now); err == nil {
		t.Fatal("400 days accepted")
	}
}
