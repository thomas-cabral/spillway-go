package spillway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// QuotaRuleUsage mirrors the spillway quota-rules usage response for a single rule.
type QuotaRuleUsage struct {
	RuleID       string  `json:"rule_id"`
	RuleName     string  `json:"rule_name"`
	RuleType     string  `json:"rule_type"`
	CurrentUsage float64 `json:"current_usage"`
	Limit        float64 `json:"limit"`
	Remaining    float64 `json:"remaining"`
	ResetPeriod  string  `json:"reset_period"`
	HasOverride  bool    `json:"has_override"`
}

// CheckQuota synchronously checks whether the given user has remaining quota.
// Returns nil if quota is available, ErrQuotaExhausted if not.
// By default fails open: network/spillway errors are logged and nil is returned.
// Use WithFailClosed to return ErrQuotaCheckFailed on errors instead.
func (c *Client) CheckQuota(ctx context.Context, userID string) error {
	if c == nil {
		return nil
	}

	customerID, err := c.resolveCustomerID(ctx, userID)
	if err != nil {
		c.logger.Printf("[spillway] CheckQuota: failed to resolve customer for %s: %v", userID, err)
		if c.opts.failClosed {
			return ErrQuotaCheckFailed
		}
		return nil
	}

	path := fmt.Sprintf("/v1/quota-rules/usage/%s", customerID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		c.logger.Printf("[spillway] CheckQuota: request failed for %s: %v", userID, err)
		if c.opts.failClosed {
			return ErrQuotaCheckFailed
		}
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Printf("[spillway] CheckQuota: unexpected status %d for %s", resp.StatusCode, userID)
		if c.opts.failClosed {
			return ErrQuotaCheckFailed
		}
		return nil
	}

	var summaries []QuotaRuleUsage
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		c.logger.Printf("[spillway] CheckQuota: failed to decode response for %s: %v", userID, err)
		if c.opts.failClosed {
			return ErrQuotaCheckFailed
		}
		return nil
	}

	for _, s := range summaries {
		if s.Remaining <= 0 {
			return ErrQuotaExhausted
		}
	}

	return nil
}

// CheckQuotaByRule checks whether the given user has remaining quota for a
// specific rule name. Returns the usage details and ErrQuotaExhausted if the
// rule's remaining quota is <= 0. By default fails open on all errors.
// Use WithFailClosed to return ErrQuotaCheckFailed on errors instead.
func (c *Client) CheckQuotaByRule(ctx context.Context, userID, ruleName string) (*QuotaRuleUsage, error) {
	if c == nil {
		return nil, nil
	}

	customerID, err := c.resolveCustomerID(ctx, userID)
	if err != nil {
		c.logger.Printf("[spillway] CheckQuotaByRule: failed to resolve customer for %s: %v", userID, err)
		if c.opts.failClosed {
			return nil, ErrQuotaCheckFailed
		}
		return nil, nil
	}

	path := fmt.Sprintf("/v1/quota-rules/usage/%s", customerID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		c.logger.Printf("[spillway] CheckQuotaByRule: request failed for %s: %v", userID, err)
		if c.opts.failClosed {
			return nil, ErrQuotaCheckFailed
		}
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.logger.Printf("[spillway] CheckQuotaByRule: unexpected status %d for %s", resp.StatusCode, userID)
		if c.opts.failClosed {
			return nil, ErrQuotaCheckFailed
		}
		return nil, nil
	}

	var summaries []QuotaRuleUsage
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		c.logger.Printf("[spillway] CheckQuotaByRule: failed to decode response for %s: %v", userID, err)
		if c.opts.failClosed {
			return nil, ErrQuotaCheckFailed
		}
		return nil, nil
	}

	for _, s := range summaries {
		if s.RuleName == ruleName {
			if s.Remaining <= 0 {
				return &s, ErrQuotaExhausted
			}
			return &s, nil
		}
	}

	c.logger.Printf("[spillway] CheckQuotaByRule: rule %q not found for %s", ruleName, userID)
	if c.opts.failClosed {
		return nil, ErrQuotaCheckFailed
	}
	return nil, nil
}
