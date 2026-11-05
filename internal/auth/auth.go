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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	ClientByHash(ctx context.Context, hash string) (ClientToken, error)
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
func (s *Service) CreateClient(ctx context.Context, ownerID core.ID, name string) (string, ClientToken, error) {
	return "", ClientToken{}, errs.ErrNotImplemented
}

// RevokeClient makes a client token invalid.
func (s *Service) RevokeClient(ctx context.Context, id core.ID) error {
	return errs.ErrNotImplemented
}

// Clients lists the client tokens of an owner.
func (s *Service) Clients(ctx context.Context, ownerID core.ID) ([]ClientToken, error) {
	return s.repo.Clients(ctx, ownerID)
}

func (s *Service) IssueAgent(ctx context.Context, c Claims) (string, error) {
	return "", errs.ErrNotImplemented
}

// verifyClient looks up the hash and rejects revoked tokens.
func (s *Service) verifyClient(ctx context.Context, token string) (Claims, error) {
	return Claims{}, errs.ErrNotImplemented
}

// verifyMachine looks up the hash on the machine record. Pending and
// revoked machines give errs.ErrForbidden.
func (s *Service) verifyMachine(ctx context.Context, token string) (Claims, error) {
	return Claims{}, errs.ErrNotImplemented
}

// verifyAgent checks the HMAC and the expiry.
func (s *Service) verifyAgent(token string) (Claims, error) {
	return Claims{}, errs.ErrNotImplemented
}

var _ Verifier = (*Service)(nil)
