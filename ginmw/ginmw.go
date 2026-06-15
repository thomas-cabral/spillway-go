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
// for the named quota before allowing the request to proceed.
// Returns 429 with quota details when the quota is exhausted. Fails open on all errors.
func RequireQuota(client *spillway.Client, quotaName string, userID GinUserIDFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := userID(c)
		if uid == "" {
			c.Next()
			return
		}

		status, err := client.CheckQuotaByName(c.Request.Context(), uid, quotaName)
		if errors.Is(err, spillway.ErrQuotaExhausted) && status != nil {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":        "quota exhausted",
				"quota_name":   status.QuotaName,
				"meter_name":   status.MeterName,
				"usage":        status.Usage,
				"limit":        status.Limit,
				"remaining":    status.Remaining,
				"reset_period": status.ResetPeriod,
			})
			return
		}

		c.Next()
	}
}
