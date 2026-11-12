package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(2, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	if !l.Allow("a") || !l.Allow("a") || l.Allow("a") {
		t.Fatal("limit")
	}
	if !l.Allow("b") {
		t.Fatal("keys are separate")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allow("a") {
		t.Fatal("window resets")
	}
}
