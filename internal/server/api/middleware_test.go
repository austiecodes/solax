package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/austiecodes/solax/internal/auth"
)

func serveWithClaims(t *testing.T, mw gin.HandlerFunc, cl auth.Claims) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(claimsKey, cl)
		c.Next()
	}, mw)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w.Code
}

// A plain worker agent token must not reach the management routes. This is
// the case that let any agent create workspaces and tasks.
func TestRequireClientOrSuper(t *testing.T) {
	cases := []struct {
		name string
		cl   auth.Claims
		want int
	}{
		{"client", auth.Claims{Kind: auth.KindClient}, http.StatusNoContent},
		{"super agent", auth.Claims{Kind: auth.KindAgent, Super: true}, http.StatusNoContent},
		{"worker agent", auth.Claims{Kind: auth.KindAgent}, http.StatusForbidden},
		{"machine token", auth.Claims{Kind: auth.KindMachine}, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := serveWithClaims(t, RequireClientOrSuper(), c.cl); got != c.want {
				t.Fatalf("status = %d, want %d", got, c.want)
			}
		})
	}
}
