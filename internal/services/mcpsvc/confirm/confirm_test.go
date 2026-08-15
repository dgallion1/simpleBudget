package confirm

import (
	"errors"
	"testing"
	"time"
)

type args struct {
	Name string `json:"name"`
}

func TestRedeemAcceptsAFreshToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, expires, err := r.Mint("shutdown_server", args{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if tok == "" {
		t.Fatal("Mint returned an empty token")
	}
	if !expires.After(time.Now()) {
		t.Errorf("expiry %v is not in the future", expires)
	}
	if err := r.Redeem(tok, "shutdown_server", args{}); err != nil {
		t.Fatalf("Redeem of a fresh token: %v", err)
	}
}

// The whole point of single-use: a token that worked once must not work
// again, or a guarded operation can be repeated from one confirmation.
func TestRedeemRefusesAReplay(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("shutdown_server", args{})
	if err := r.Redeem(tok, "shutdown_server", args{}); err != nil {
		t.Fatalf("first Redeem: %v", err)
	}
	if err := r.Redeem(tok, "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("replayed Redeem returned %v, want ErrBadToken", err)
	}
}

// Without tool binding, a token minted for a harmless preview could be spent
// on a different, destructive tool.
func TestRedeemRefusesAnotherTool(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("shutdown_server", args{})
	if err := r.Redeem(tok, "restore_backup", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("cross-tool Redeem returned %v, want ErrBadToken", err)
	}
}

// Without args binding, a preview of one operation could confirm a different
// one -- the model previews restoring archive A and then restores archive B.
func TestRedeemRefusesDifferentArguments(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("restore_backup", args{Name: "a.zip"})
	if err := r.Redeem(tok, "restore_backup", args{Name: "b.zip"}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("mismatched-args Redeem returned %v, want ErrBadToken", err)
	}
}

func TestRedeemRefusesAnExpiredToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return base }
	tok, _, _ := r.Mint("shutdown_server", args{})

	r.now = func() time.Time { return base.Add(time.Minute + time.Second) }
	if err := r.Redeem(tok, "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("expired Redeem returned %v, want ErrBadToken", err)
	}
}

func TestRedeemRefusesAnUnknownToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	if err := r.Redeem("deadbeef", "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("unknown Redeem returned %v, want ErrBadToken", err)
	}
}

// A token presented for the wrong tool must be SPENT, not merely refused --
// otherwise a caller can probe tool names until one sticks.
func TestAWrongToolRedeemSpendsTheToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("shutdown_server", args{})
	if err := r.Redeem(tok, "restore_backup", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("cross-tool Redeem returned %v, want ErrBadToken", err)
	}
	if err := r.Redeem(tok, "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("Redeem after a wrong-tool attempt returned %v, want ErrBadToken (the token should have been consumed)", err)
	}
}

func TestMintReturnsDistinctTokens(t *testing.T) {
	r := NewRegistry(time.Minute)
	a, _, _ := r.Mint("shutdown_server", args{})
	b, _, _ := r.Mint("shutdown_server", args{})
	if a == b {
		t.Fatal("two Mint calls returned the same token")
	}
}
