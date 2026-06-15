package spillway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// overageHardCutoff is the OverageBehavior that blocks once a limit is reached.
// Any other behavior (e.g. OVERAGE_BILLING) allows usage past the limit, so a
// quota with remaining <= 0 is only "exhausted" when it is HARD_CUTOFF.
const overageHardCutoff = "HARD_CUTOFF"

// QuotaStatus mirrors a single item of the spillway quota-status response:
// GET /v1/customers/{customer_id}/quota-status. It is the live, resolved limit
// and usage for one meter-bound quota that applies to a customer.
type QuotaStatus struct {
	QuotaID         string  `json:"quota_id"`
	QuotaName       string  `json:"quota_name"`
	MeterName       string  `json:"meter_name"`
	AggregationType string  `json:"aggregation_type"`
	ResetPeriod     string  `json:"reset_period"`
	Limit           float64 `json:"limit"`
	Usage           float64 `json:"usage"`
	Remaining       float64 `json:"remaining"`
	LimitSource     string  `json:"limit_source"`     // "override" | "plan" | "default"
	OverageBehavior string  `json:"overage_behavior"` // "HARD_CUTOFF" | "OVERAGE_BILLING"
}

// exhausted reports whether this quota should block a request. A quota only
// blocks when it is fully consumed AND configured to hard-cut off; overage
// billing quotas are allowed past their limit (the server bills the overage).
func (q QuotaStatus) exhausted() bool {
	return q.Remaining <= 0 && q.OverageBehavior == overageHardCutoff
}

// fetchQuotaStatus resolves the external user ID to a spillway customer and
// returns that customer's live quota status for every applicable quota.
func (c *Client) fetchQuotaStatus(ctx context.Context, userID string) ([]QuotaStatus, error) {
	customerID, err := c.resolveCustomerID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve customer: %w", err)
	}

	path := fmt.Sprintf("/v1/customers/%s/quota-status", customerID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var items []QuotaStatus
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return items, nil
}

// CheckQuota synchronously checks whether the given user has remaining quota
// across all of their quotas. Returns nil if every quota has headroom (or
// allows overage), ErrQuotaExhausted if any HARD_CUTOFF quota is fully consumed.
// By default fails open: network/spillway errors are logged and nil is returned.
// Use WithFailClosed to return ErrQuotaCheckFailed on errors instead.
func (c *Client) CheckQuota(ctx context.Context, userID string) error {
	if c == nil {
		return nil
	}

	items, err := c.fetchQuotaStatus(ctx, userID)
	if err != nil {
		c.logger.Printf("[spillway] CheckQuota: %v (user %s)", err, userID)
		if c.opts.failClosed {
			return ErrQuotaCheckFailed
		}
		return nil
	}

	for _, it := range items {
		if it.exhausted() {
			return ErrQuotaExhausted
		}
	}
	return nil
}

// CheckQuotaByName checks whether the given user has remaining quota for a
// specific quota by name. Returns the quota status and ErrQuotaExhausted if that
// quota is a fully-consumed HARD_CUTOFF quota. By default fails open on all
// errors. Use WithFailClosed to return ErrQuotaCheckFailed on errors instead.
func (c *Client) CheckQuotaByName(ctx context.Context, userID, quotaName string) (*QuotaStatus, error) {
	if c == nil {
		return nil, nil
	}

	items, err := c.fetchQuotaStatus(ctx, userID)
	if err != nil {
		c.logger.Printf("[spillway] CheckQuotaByName: %v (user %s)", err, userID)
		if c.opts.failClosed {
			return nil, ErrQuotaCheckFailed
		}
		return nil, nil
	}

	for i := range items {
		if items[i].QuotaName == quotaName {
			if items[i].exhausted() {
				return &items[i], ErrQuotaExhausted
			}
			return &items[i], nil
		}
	}

	c.logger.Printf("[spillway] CheckQuotaByName: quota %q not found for %s", quotaName, userID)
	if c.opts.failClosed {
		return nil, ErrQuotaCheckFailed
	}
	return nil, nil
}
