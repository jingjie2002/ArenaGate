package protocol

import "testing"

func TestDecodeRequiresType(t *testing.T) {
	_, err := Decode([]byte(`{"player_id":"p1"}`))
	if err == nil {
		t.Fatal("expected missing type to fail")
	}
}

func TestNormalizeEnqueueDefaults(t *testing.T) {
	msg := NormalizeEnqueue(Message{Type: TypeEnqueue})
	if msg.MatchMode != DefaultMode {
		t.Fatalf("unexpected default match mode: %s", msg.MatchMode)
	}
	if msg.MaxWaitMS != DefaultMaxWait {
		t.Fatalf("unexpected default max wait: %d", msg.MaxWaitMS)
	}
	if msg.MMRScore != DefaultMMRScore {
		t.Fatalf("unexpected default mmr: %d", msg.MMRScore)
	}
}
