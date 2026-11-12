// Package auth verifies the three token kinds of Solax.
//
//	Kind     Created by                      Server keeps        Used by
//	client   solax-server token create       SHA-256 hash        CLI client commands, desktop app
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

	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
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
	Subject   core.ID // client token ID, machine ID or agent ID
	OwnerID   core.ID
	Super     bool    // agent token of the Super Agent
	Workspace core.ID // agent tokens only
	Task      core.ID // agent tokens only
	SessionID core.ID // agent tokens only
	Expires   time.Time
}

// Actor converts the claims to the identity that the Engine uses.
func (c Claims) Actor() core.Actor {
	a := core.Actor{ID: c.Subject, OwnerID: c.OwnerID, Workspace: c.Workspace, Task: c.Task}
	switch {
	case c.Kind == KindClient:
		a.Kind = core.AuthorHuman
	case c.Super:
		a.Kind = core.AuthorSuper
	default:
		a.Kind = core.AuthorAgent
	}
	return a
}

// ClientToken is the stored record of a client token.
type ClientToken struct {
	ID        core.ID
	OwnerID   core.ID
	Name      string // for example "macbook-cli", "desktop"
	Hash      string
	CreatedAt time.Time
	LastUsed  time.Time
	Revoked   bool
}

// Repo is the storage port of auth. internal/store/sqlite implements it.
// Machine token hashes live on the machine record (core.Machine.TokenHash).
type Repo interface {
	// DefaultOwner returns the owner of a single-owner installation, or
	// errs.ErrNotFound before "solax-server init".
	DefaultOwner(ctx context.Context) (core.ID, error)
	ClientByHash(ctx context.Context, hash string) (ClientToken, error)
	ClientByID(ctx context.Context, id core.ID) (ClientToken, error)
	SaveClient(ctx context.Context, t ClientToken) error
	Clients(ctx context.Context, ownerID core.ID) ([]ClientToken, error)
	MachineByTokenHash(ctx context.Context, hash string) (core.Machine, error)
}

// Verifier verifies tokens and issues agent tokens.
type Verifier interface {
	// Verify returns the claims of a token. A machine that is not approved
	// gives errs.ErrForbidden.
	Verify(ctx context.Context, token string) (Claims, error)
	// IssueAgent signs a short-lived agent token for one session.
	IssueAgent(ctx context.Context, c Claims) (string, error)
}

// Service is the v1 Verifier: hash lookups for client and machine tokens,
// HMAC-signed agent tokens.
type Service struct {
	repo   Repo
	secret []byte // signs agent tokens
}

// NewService returns a Service.
func NewService(repo Repo, secret []byte) *Service { return &Service{repo: repo, secret: secret} }

func (s *Service) Verify(ctx context.Context, token string) (Claims, error) {
	kind, ok := KindOf(token)
	if !ok {
		return Claims{}, errs.ErrUnauthorized
	}
	switch kind {
	case KindClient:
		return s.verifyClient(ctx, token)
	case KindMachine:
		return s.verifyMachine(ctx, token)
	default:
		return s.verifyAgent(token)
	}
}

// CreateClient creates a client token. It returns the token once; the
// server keeps only its hash. "solax-server token create" calls it.
// An empty ownerID means the default owner.
func (s *Service) CreateClient(ctx context.Context, ownerID core.ID, name string) (string, ClientToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ClientToken{}, fmt.Errorf("%w: a token needs a name", errs.ErrInvalid)
	}
	if ownerID == "" {
		var err error
		if ownerID, err = s.repo.DefaultOwner(ctx); err != nil {
			return "", ClientToken{}, fmt.Errorf("no owner (run solax-server init): %w", err)
		}
	}
	token, err := NewToken(PrefixClient)
	if err != nil {
		return "", ClientToken{}, err
	}
	t := ClientToken{
		ID:        core.NewID(core.PrefixClient),
		OwnerID:   ownerID,
		Name:      name,
		Hash:      Hash(token),
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.repo.SaveClient(ctx, t); err != nil {
		return "", ClientToken{}, err
	}
	return token, t, nil
}

// RevokeClient makes a client token invalid.
func (s *Service) RevokeClient(ctx context.Context, id core.ID) error {
	t, err := s.repo.ClientByID(ctx, id)
	if err != nil {
		return err
	}
	if t.Revoked {
		return nil
	}
	t.Revoked = true
	return s.repo.SaveClient(ctx, t)
}

// Clients lists the client tokens of an owner. An empty ownerID means the
// default owner.
func (s *Service) Clients(ctx context.Context, ownerID core.ID) ([]ClientToken, error) {
	if ownerID == "" {
		var err error
		if ownerID, err = s.repo.DefaultOwner(ctx); err != nil {
			return nil, err
		}
	}
	return s.repo.Clients(ctx, ownerID)
}

// sessionTTL is the default lifetime of an agent token. A session that
// lives longer gets a fresh token with its next ensure.
const sessionTTL = 6 * time.Hour

func (s *Service) IssueAgent(ctx context.Context, c Claims) (string, error) {
	if s.secret == nil || len(s.secret) == 0 {
		return "", errs.ErrUnavailable
	}
	c.Kind = KindAgent
	if c.Expires.IsZero() {
		c.Expires = time.Now().Add(sessionTTL)
	}
	return s.signAgent(c)
}

// IssueToken issues the agent token of one session. The engine reaches it
// through core.AgentTokens.
func (s *Service) IssueToken(ctx context.Context, r core.TokenRequest) (string, error) {
	ttl := r.TTL
	if ttl <= 0 {
		ttl = sessionTTL
	}
	return s.IssueAgent(ctx, Claims{
		Kind:      KindAgent,
		Subject:   r.AgentID,
		OwnerID:   r.OwnerID,
		Super:     r.Super,
		Workspace: r.Workspace,
		Task:      r.Task,
		SessionID: r.SessionID,
		Expires:   time.Now().Add(ttl),
	})
}

// agentClaims is the wire form of an agent token. The JSON is compact, so
// a token stays short on the command line.
type agentClaims struct {
	AgentID   core.ID   `json:"a"`
	OwnerID   core.ID   `json:"o"`
	Super     bool      `json:"s,omitempty"`
	Workspace core.ID   `json:"w,omitempty"`
	Task      core.ID   `json:"t,omitempty"`
	SessionID core.ID   `json:"e,omitempty"`
	Expires   time.Time `json:"x"`
}

// signAgent returns "sxa_<claims>.<hmac>", both parts base64url without
// padding. The HMAC covers the claims part as it appears in the token.
func (s *Service) signAgent(c Claims) (string, error) {
	if len(s.secret) == 0 {
		return "", errs.ErrUnavailable
	}
	body, err := json.Marshal(agentClaims{
		AgentID: c.Subject, OwnerID: c.OwnerID, Super: c.Super,
		Workspace: c.Workspace, Task: c.Task, SessionID: c.SessionID, Expires: c.Expires,
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return PrefixAgent + payload + "." + s.mac(payload), nil
}

func (s *Service) mac(payload string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// lastUsedEvery bounds how often a client token's last use is written, so
// reads do not turn into a write per request.
const lastUsedEvery = time.Minute

// verifyClient looks up the hash and rejects revoked tokens.
func (s *Service) verifyClient(ctx context.Context, token string) (Claims, error) {
	t, err := s.repo.ClientByHash(ctx, Hash(token))
	if errors.Is(err, errs.ErrNotFound) || (err == nil && t.Revoked) {
		return Claims{}, errs.ErrUnauthorized
	}
	if err != nil {
		return Claims{}, err
	}
	if now := time.Now().UTC(); now.Sub(t.LastUsed) > lastUsedEvery {
		t.LastUsed = now.Truncate(time.Millisecond)
		_ = s.repo.SaveClient(ctx, t) // best effort; never fails a request
	}
	return Claims{Kind: KindClient, Subject: t.ID, OwnerID: t.OwnerID}, nil
}

// verifyMachine looks up the hash on the machine record. Pending and
// revoked machines give errs.ErrForbidden.
func (s *Service) verifyMachine(ctx context.Context, token string) (Claims, error) {
	m, err := s.repo.MachineByTokenHash(ctx, Hash(token))
	if errors.Is(err, errs.ErrNotFound) {
		return Claims{}, errs.ErrUnauthorized
	}
	if err != nil {
		return Claims{}, err
	}
	switch m.State {
	case core.MachineApproved:
		return Claims{Kind: KindMachine, Subject: m.ID, OwnerID: m.OwnerID}, nil
	case core.MachinePending:
		return Claims{}, fmt.Errorf("%w: pending approval", errs.ErrForbidden)
	default:
		return Claims{}, fmt.Errorf("%w: machine revoked", errs.ErrForbidden)
	}
}

// verifyAgent checks the HMAC and the expiry.
func (s *Service) verifyAgent(token string) (Claims, error) {
	if len(s.secret) == 0 {
		return Claims{}, errs.ErrUnauthorized
	}
	payload, sig, ok := strings.Cut(strings.TrimPrefix(token, PrefixAgent), ".")
	if !ok || payload == "" || sig == "" {
		return Claims{}, errs.ErrUnauthorized
	}
	if !hmac.Equal([]byte(sig), []byte(s.mac(payload))) {
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
		Workspace: c.Workspace, Task: c.Task, SessionID: c.SessionID, Expires: c.Expires,
	}, nil
}

var (
	_ Verifier         = (*Service)(nil)
	_ core.AgentTokens = (*Service)(nil)
)
