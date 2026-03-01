package ginmw

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	spillway "github.com/thomas-cabral/spillway-go"
)

// GinUserIDFunc extracts a user ID from a Gin context.
// Needed because Gin stores c.Set() values in its own Keys map,
// NOT in r.Context() — so func(*http.Request) can't reach them.
type GinUserIDFunc func(c *gin.Context) string

// RequireQuota returns Gin middleware that checks the user's remaining quota
// for the given rule name before allowing the request to proceed.
// Returns 429 with usage details when quota is exhausted. Fails open on all errors.
func RequireQuota(client *spillway.Client, ruleName string, userID GinUserIDFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := userID(c)
		if uid == "" {
			c.Next()
			return
		}

		usage, err := client.CheckQuotaByRule(c.Request.Context(), uid, ruleName)
		if errors.Is(err, spillway.ErrQuotaExhausted) && usage != nil {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":         "quota exhausted",
				"rule_name":     usage.RuleName,
				"current_usage": usage.CurrentUsage,
				"limit":         usage.Limit,
				"remaining":     usage.Remaining,
				"reset_period":  usage.ResetPeriod,
			})
			return
		}

		c.Next()
	}
}
