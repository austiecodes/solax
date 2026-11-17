package middleware

import "github.com/gin-gonic/gin"

// RateLimit limits requests per client address.
func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: token bucket per address.
		c.Next()
	}
}
