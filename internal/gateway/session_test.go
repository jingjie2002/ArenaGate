package gateway

import (
	"testing"
	"time"
)

func TestRateLimiterResetsByWindow(t *testing.T) {
	limiter := newRateLimiter(2, time.Second)
	now := time.Unix(100, 0)

	if !limiter.Allow(now) || !limiter.Allow(now) {
		t.Fatal("first two messages should be allowed")
	}
	if limiter.Allow(now) {
		t.Fatal("third message in same window should be limited")
	}
	if !limiter.Allow(now.Add(time.Second)) {
		t.Fatal("message after window reset should be allowed")
	}
}

func TestSessionManagerBindAndRemove(t *testing.T) {
	manager := NewSessionManager()
	session := manager.Add("127.0.0.1:1000", 10)
	manager.BindPlayer(session, "p1")
	if count := manager.ActiveCount(); count != 1 {
		t.Fatalf("unexpected active count: %d", count)
	}
	playerID, authed := session.Player()
	if !authed || playerID != "p1" {
		t.Fatalf("unexpected session player: %s authed=%v", playerID, authed)
	}
	manager.Remove(session)
	if count := manager.ActiveCount(); count != 0 {
		t.Fatalf("unexpected active count after remove: %d", count)
	}
}
