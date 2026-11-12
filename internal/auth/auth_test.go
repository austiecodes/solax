package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

func TestAgentTokenRoundTrip(t *testing.T) {
	s := NewService(nil, []byte("test-secret"))
	ctx := context.Background()

	token, err := s.IssueToken(ctx, core.TokenRequest{
		OwnerID:   "own_1",
		AgentID:   "agt_1",
		SessionID: "ses_1",
		Workspace: "ws_1",
		Task:      "tsk_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, PrefixAgent) {
		t.Fatalf("token %q lacks prefix %q", token, PrefixAgent)
	}
	cl, err := s.Verify(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Kind != KindAgent || cl.Subject != "agt_1" || cl.Workspace != "ws_1" || cl.Task != "tsk_1" || cl.SessionID != "ses_1" {
		t.Fatalf("claims = %+v", cl)
	}
	if cl.Expires.IsZero() || !cl.Expires.After(time.Now()) {
		t.Fatalf("expiry = %v, want a future time", cl.Expires)
	}
}

func TestAgentTokenTampered(t *testing.T) {
	s := NewService(nil, []byte("test-secret"))
	ctx := context.Background()
	token, err := s.IssueToken(ctx, core.TokenRequest{AgentID: "agt_1", Workspace: "ws_1"})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(token, PrefixAgent)
	payload, sig, _ := strings.Cut(body, ".")

	// A rewritten payload with the old signature.
	forged := PrefixAgent + payload[:len(payload)-1] + "A." + sig
	if _, err := s.Verify(ctx, forged); err == nil {
		t.Fatal("a forged claims part was accepted")
	}
	// A token signed with another secret.
	other, err := NewService(nil, []byte("other-secret")).IssueToken(ctx, core.TokenRequest{AgentID: "agt_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, other); err == nil {
		t.Fatal("a token of another secret was accepted")
	}
}

func TestAgentTokenExpired(t *testing.T) {
	s := NewService(nil, []byte("test-secret"))
	token, err := s.IssueToken(context.Background(), core.TokenRequest{AgentID: "agt_1", TTL: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := s.Verify(context.Background(), token); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

func TestIssueWithoutSecret(t *testing.T) {
	if _, err := NewService(nil, nil).IssueToken(context.Background(), core.TokenRequest{}); err != errs.ErrUnavailable {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
