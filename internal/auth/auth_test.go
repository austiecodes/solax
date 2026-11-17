package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
)

// withSecret sets the signing secret for one test.
func withSecret(t *testing.T, s string) {
	old := secret
	SetSecret([]byte(s))
	t.Cleanup(func() { SetSecret(old) })
}

func TestAgentTokenRoundTrip(t *testing.T) {
	withSecret(t, "test-secret")
	ctx := context.Background()

	token, err := IssueToken(ctx, model.TokenRequest{
		OwnerID:   "own_1",
		AgentID:   "agt_1",
		SessionID: "ses_1",
		Job:       "job_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, PrefixAgent) {
		t.Fatalf("token %q lacks prefix %q", token, PrefixAgent)
	}
	cl, err := Verify(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Kind != KindAgent || cl.Subject != "agt_1" || cl.Job != "job_1" || cl.SessionID != "ses_1" {
		t.Fatalf("claims = %+v", cl)
	}
	if cl.Expires.IsZero() || !cl.Expires.After(time.Now()) {
		t.Fatalf("expiry = %v, want a future time", cl.Expires)
	}
}

func TestAgentTokenTampered(t *testing.T) {
	withSecret(t, "test-secret")
	ctx := context.Background()
	token, err := IssueToken(ctx, model.TokenRequest{AgentID: "agt_1", Job: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(token, PrefixAgent)
	payload, sig, _ := strings.Cut(body, ".")

	// A rewritten payload with the old signature.
	forged := PrefixAgent + payload[:len(payload)-1] + "A." + sig
	if _, err := Verify(ctx, forged); err == nil {
		t.Fatal("a forged claims part was accepted")
	}
	// A token signed with another secret.
	SetSecret([]byte("other-secret"))
	other, err := IssueToken(ctx, model.TokenRequest{AgentID: "agt_1"})
	SetSecret([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, other); err == nil {
		t.Fatal("a token of another secret was accepted")
	}
}

func TestAgentTokenExpired(t *testing.T) {
	withSecret(t, "test-secret")
	token, err := IssueToken(context.Background(), model.TokenRequest{AgentID: "agt_1", TTL: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := Verify(context.Background(), token); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

func TestIssueWithoutSecret(t *testing.T) {
	withSecret(t, "")
	if _, err := IssueToken(context.Background(), model.TokenRequest{}); err != errs.ErrUnavailable {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
