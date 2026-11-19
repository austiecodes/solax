// Package auth verifies the three token kinds of Solax.
//
//	Kind     Created by                      Server keeps        Used by
//	client   solax server token create       SHA-256 hash        CLI client commands, desktop app
//	machine  the machine (solax daemon)      SHA-256 hash        solax daemon run
//	agent    the server, per session         nothing (signed)    agent sessions (SOLAX_TOKEN)
//
// The server never stores a client or machine token, only its hash. A
// machine token is valid only after a client approved the machine.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
)

// Kind is the kind of a token.
type Kind string

const (
	KindClient  Kind = "client"
	KindMachine Kind = "machine"
	KindAgent   Kind = "agent"
)

// Token prefixes. They make a leaked token easy to identify and let the
// verifier select the check without a lookup.
const (
	PrefixClient  = "sxc_"
	PrefixMachine = "sxm_"
	PrefixAgent   = "sxa_"
)

// NewToken returns a new random token with a prefix: 32 random bytes in
// base64url.
func NewToken(prefix string) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// Hash returns the hex SHA-256 of a token. The server stores only this
// value. Tokens have 256 bits of entropy, so a fast hash is sufficient.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// KindOf returns the kind of a token from its prefix.
func KindOf(token string) (Kind, bool) {
	switch {
	case strings.HasPrefix(token, PrefixClient):
		return KindClient, true
	case strings.HasPrefix(token, PrefixMachine):
		return KindMachine, true
	case strings.HasPrefix(token, PrefixAgent):
		return KindAgent, true
	}
	return "", false
}

// Claims are the verified contents of a token.
type Claims struct {
	Kind      Kind
	Subject   model.ID // client token ID, machine ID or agent ID
	OwnerID   model.ID
	Super     bool     // agent token of the Super Agent
	Task      model.ID // agent tokens of a task session only
	SessionID model.ID // agent tokens only
	Expires   time.Time
}

// Actor converts the claims to the identity that the service uses.
func (c Claims) Actor() model.Actor {
	a := model.Actor{ID: c.Subject, OwnerID: c.OwnerID}
	switch {
	case c.Kind == KindClient:
		a.Kind = model.AuthorHuman
	case c.Super:
		a.Kind = model.AuthorSuper
	default:
		a.Kind = model.AuthorAgent
	}
	return a
}

// secret signs agent tokens. Without it, agent tokens are unavailable.
var secret []byte

// SetSecret sets the secret that signs agent tokens. The server calls it
// once at start, before it serves. Client and machine tokens need no
// secret: they are looked up by hash in the model database.
func SetSecret(s []byte) { secret = s }

// Verify returns the claims of a token. A machine that is not approved
// gives errs.ErrForbidden.
func Verify(ctx context.Context, token string) (Claims, error) {
	kind, ok := KindOf(token)
	if !ok {
		return Claims{}, errs.ErrUnauthorized
	}
	switch kind {
	case KindClient:
		return verifyClient(ctx, token)
	case KindMachine:
		return verifyMachine(ctx, token)
	default:
		return verifyAgent(token)
	}
}

// CreateClient creates a client token. It returns the token once; the
// server keeps only its hash. "solax server token create" calls it.
// An empty ownerID means the default owner.
func CreateClient(ctx context.Context, ownerID model.ID, name string) (string, model.ClientToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", model.ClientToken{}, fmt.Errorf("%w: a token needs a name", errs.ErrInvalid)
	}
	if ownerID == "" {
		var err error
		if ownerID, err = model.GetDefaultOwner(ctx, model.ReadDB); err != nil {
			return "", model.ClientToken{}, fmt.Errorf("no owner (run solax server init): %w", err)
		}
	}
	token, err := NewToken(PrefixClient)
	if err != nil {
		return "", model.ClientToken{}, err
	}
	ct := model.ClientToken{
		ID:        model.NewID(model.PrefixClient),
		OwnerID:   ownerID,
		Name:      name,
		Hash:      Hash(token),
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := model.SaveClient(ctx, model.WriteDB, ct); err != nil {
		return "", model.ClientToken{}, err
	}
	return token, ct, nil
}

// RevokeClient makes a client token invalid.
func RevokeClient(ctx context.Context, id model.ID) error {
	ct, err := model.GetClient(ctx, model.ReadDB, id)
	if err != nil {
		return err
	}
	if ct.Revoked {
		return nil
	}
	ct.Revoked = true
	return model.SaveClient(ctx, model.WriteDB, ct)
}

// ListClients lists the client tokens of an owner. An empty ownerID means the
// default owner.
func ListClients(ctx context.Context, ownerID model.ID) ([]model.ClientToken, error) {
	if ownerID == "" {
		var err error
		if ownerID, err = model.GetDefaultOwner(ctx, model.ReadDB); err != nil {
			return nil, err
		}
	}
	return model.ListClients(ctx, model.ReadDB, ownerID)
}

// sessionTTL is the default lifetime of an agent token. A session that
// lives longer gets a fresh token with its next ensure.
const sessionTTL = 6 * time.Hour

// IssueToken issues the agent token of one session. The service reaches it
// through service.IssueToken.
func IssueToken(ctx context.Context, r model.TokenRequest) (string, error) {
	ttl := r.TTL
	if ttl <= 0 {
		ttl = sessionTTL
	}
	return signAgent(Claims{
		Kind:      KindAgent,
		Subject:   r.AgentID,
		OwnerID:   r.OwnerID,
		Super:     r.Super,
		Task:      r.Task,
		SessionID: r.SessionID,
		Expires:   time.Now().Add(ttl),
	})
}

// agentClaims is the wire form of an agent token. The JSON is compact, so
// a token stays short on the command line.
type agentClaims struct {
	AgentID   model.ID  `json:"a"`
	OwnerID   model.ID  `json:"o"`
	Super     bool      `json:"s,omitempty"`
	Task      model.ID  `json:"j,omitempty"`
	SessionID model.ID  `json:"e,omitempty"`
	Expires   time.Time `json:"x"`
}

// signAgent returns "sxa_<claims>.<hmac>", both parts base64url without
// padding. The HMAC covers the claims part as it appears in the token.
func signAgent(c Claims) (string, error) {
	if len(secret) == 0 {
		return "", errs.ErrUnavailable
	}
	body, err := json.Marshal(agentClaims{
		AgentID: c.Subject, OwnerID: c.OwnerID, Super: c.Super,
		Task: c.Task, SessionID: c.SessionID, Expires: c.Expires,
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return PrefixAgent + payload + "." + mac(payload), nil
}

func mac(payload string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// lastUsedEvery bounds how often a client token's last use is written, so
// reads do not turn into a write per request.
const lastUsedEvery = time.Minute

// verifyClient looks up the hash and rejects revoked tokens.
func verifyClient(ctx context.Context, token string) (Claims, error) {
	ct, err := model.GetClientByHash(ctx, model.ReadDB, Hash(token))
	if errors.Is(err, errs.ErrNotFound) || (err == nil && ct.Revoked) {
		return Claims{}, errs.ErrUnauthorized
	}
	if err != nil {
		return Claims{}, err
	}
	if now := time.Now().UTC(); now.Sub(ct.LastUsed) > lastUsedEvery {
		ct.LastUsed = now.Truncate(time.Millisecond)
		_ = model.SaveClient(ctx, model.WriteDB, ct) // best effort; never fails a request
	}
	return Claims{Kind: KindClient, Subject: ct.ID, OwnerID: ct.OwnerID}, nil
}

// verifyMachine looks up the hash on the machine record. Pending and
// revoked machines give errs.ErrForbidden.
func verifyMachine(ctx context.Context, token string) (Claims, error) {
	m, err := model.GetMachineByTokenHash(ctx, model.ReadDB, Hash(token))
	if errors.Is(err, errs.ErrNotFound) {
		return Claims{}, errs.ErrUnauthorized
	}
	if err != nil {
		return Claims{}, err
	}
	switch m.State {
	case model.MachineApproved:
		return Claims{Kind: KindMachine, Subject: m.ID, OwnerID: m.OwnerID}, nil
	case model.MachinePending:
		return Claims{}, fmt.Errorf("%w: pending approval", errs.ErrForbidden)
	default:
		return Claims{}, fmt.Errorf("%w: machine revoked", errs.ErrForbidden)
	}
}

// verifyAgent checks the HMAC and the expiry.
func verifyAgent(token string) (Claims, error) {
	if len(secret) == 0 {
		return Claims{}, errs.ErrUnauthorized
	}
	payload, sig, ok := strings.Cut(strings.TrimPrefix(token, PrefixAgent), ".")
	if !ok || payload == "" || sig == "" {
		return Claims{}, errs.ErrUnauthorized
	}
	if !hmac.Equal([]byte(sig), []byte(mac(payload))) {
		return Claims{}, errs.ErrUnauthorized
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Claims{}, errs.ErrUnauthorized
	}
	var c agentClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return Claims{}, errs.ErrUnauthorized
	}
	if !c.Expires.IsZero() && time.Now().After(c.Expires) {
		return Claims{}, errs.ErrUnauthorized
	}
	return Claims{
		Kind: KindAgent, Subject: c.AgentID, OwnerID: c.OwnerID, Super: c.Super,
		Task: c.Task, SessionID: c.SessionID, Expires: c.Expires,
	}, nil
}
