package httpmw

import (
	"encoding/json"
	"errors"
	"net/http"

	spillway "github.com/thomas-cabral/spillway-go"
)

// UserIDFunc extracts a user ID from a stdlib request.
type UserIDFunc func(r *http.Request) string

// RequireQuota returns net/http middleware that checks the user's remaining
// quota for the given rule name before allowing the request to proceed.
// Returns 429 with usage details when quota is exhausted. Fails open on all errors.
func RequireQuota(client *spillway.Client, ruleName string, userID UserIDFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := userID(r)
			if uid == "" {
				next.ServeHTTP(w, r)
				return
			}

			usage, err := client.CheckQuotaByRule(r.Context(), uid, ruleName)
			if errors.Is(err, spillway.ErrQuotaExhausted) && usage != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":         "quota exhausted",
					"rule_name":     usage.RuleName,
					"current_usage": usage.CurrentUsage,
					"limit":         usage.Limit,
					"remaining":     usage.Remaining,
					"reset_period":  usage.ResetPeriod,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
