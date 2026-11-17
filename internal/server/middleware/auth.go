// Package middleware holds the gin middleware of the REST API: token
// checks that store the claims of the caller, and request limits.
// Controllers read the caller with Actor and end a failed request with
// Abort.
package middleware

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/errs"
	"github.com/austiecodes/solax/internal/server/model"
)

const claimsKey = "solax.claims"

// Authenticate verifies the bearer token and stores its claims. Machine
// tokens are valid only on the daemon endpoint, not on the REST API.
func Authenticate(tokens *auth.Tokens) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || token == "" {
			Abort(c, errs.ErrUnauthorized)
			return
		}
		cl, err := tokens.Verify(c.Request.Context(), token)
		if err != nil {
			Abort(c, err)
			return
		}
		if cl.Kind == auth.KindMachine {
			Abort(c, errs.ErrForbidden)
			return
		}
		c.Set(claimsKey, cl)
		c.Next()
	}
}

// RequireKind rejects tokens of other kinds.
func RequireKind(kinds ...auth.Kind) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !slices.Contains(kinds, Claims(c).Kind) {
			Abort(c, errs.ErrForbidden)
			return
		}
		c.Next()
	}
}

// RequireSuper rejects tokens other than the Super Agent token.
func RequireSuper() gin.HandlerFunc {
	return func(c *gin.Context) {
		if cl := Claims(c); cl.Kind != auth.KindAgent || !cl.Super {
			Abort(c, errs.ErrForbidden)
			return
		}
		c.Next()
	}
}

// RequireClientOrSuper accepts a person (client token) or the Super Agent
// (an agent token with Super). Plain worker agent tokens are rejected with
// 403. It guards the job routes.
func RequireClientOrSuper() gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := Claims(c)
		if cl.Kind != auth.KindClient && !(cl.Kind == auth.KindAgent && cl.Super) {
			Abort(c, errs.ErrForbidden)
			return
		}
		c.Next()
	}
}

// Claims returns the claims that Authenticate stored.
func Claims(c *gin.Context) auth.Claims {
	v, _ := c.Get(claimsKey)
	cl, _ := v.(auth.Claims)
	return cl
}

// Actor returns the identity of the caller.
func Actor(c *gin.Context) model.Actor { return Claims(c).Actor() }
