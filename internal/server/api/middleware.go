package api

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/errs"
)

const claimsKey = "solax.claims"

// Authenticate verifies the bearer token and stores its claims. Machine
// tokens are valid only on the daemon endpoint, not on the REST API.
func Authenticate(v auth.Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || token == "" {
			fail(c, errs.ErrUnauthorized)
			return
		}
		cl, err := v.Verify(c.Request.Context(), token)
		if err != nil {
			fail(c, err)
			return
		}
		if cl.Kind == auth.KindMachine {
			fail(c, errs.ErrForbidden)
			return
		}
		c.Set(claimsKey, cl)
		c.Next()
	}
}

// RateLimit limits requests per client address.
func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: token bucket per address.
		c.Next()
	}
}

// RequireKind rejects tokens of other kinds.
func RequireKind(kinds ...auth.Kind) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !slices.Contains(kinds, claims(c).Kind) {
			fail(c, errs.ErrForbidden)
			return
		}
		c.Next()
	}
}

// RequireSuper rejects tokens other than the Super Agent token.
func RequireSuper() gin.HandlerFunc {
	return func(c *gin.Context) {
		if cl := claims(c); cl.Kind != auth.KindAgent || !cl.Super {
			fail(c, errs.ErrForbidden)
			return
		}
		c.Next()
	}
}

func claims(c *gin.Context) auth.Claims {
	v, _ := c.Get(claimsKey)
	cl, _ := v.(auth.Claims)
	return cl
}

func actor(c *gin.Context) core.Actor { return claims(c).Actor() }
