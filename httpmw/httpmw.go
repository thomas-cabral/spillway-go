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
// quota for the named quota before allowing the request to proceed.
// Returns 429 with quota details when the quota is exhausted. Fails open on all errors.
func RequireQuota(client *spillway.Client, quotaName string, userID UserIDFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := userID(r)
			if uid == "" {
				next.ServeHTTP(w, r)
				return
			}

			status, err := client.CheckQuotaByName(r.Context(), uid, quotaName)
			if errors.Is(err, spillway.ErrQuotaExhausted) && status != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
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

			next.ServeHTTP(w, r)
		})
	}
}
