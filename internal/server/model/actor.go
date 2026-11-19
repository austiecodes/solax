package model

import "time"

// Actor is the identity behind a call. The API builds it from the token.
type Actor struct {
	Kind    AuthorKind
	ID      ID
	OwnerID ID
}

// Author returns the message author of the actor.
func (a Actor) Author() Author { return Author{Kind: a.Kind, ID: a.ID} }

// TokenRequest describes the agent token of one session.
type TokenRequest struct {
	OwnerID   ID
	AgentID   ID
	SessionID ID
	Task      ID // empty for the Super Agent
	Super     bool
	TTL       time.Duration
}
