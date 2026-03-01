package spillway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// customerResponse mirrors the spillway Customer JSON.
type customerResponse struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
}

// resolveCustomerID maps an external user_id to a spillway customer UUID.
// Uses an in-memory cache; on cache miss, queries spillway and auto-creates
// the customer if not found.
func (c *Client) resolveCustomerID(ctx context.Context, externalID string) (string, error) {
	c.customerMu.RLock()
	if id, ok := c.customerCache[externalID]; ok {
		c.customerMu.RUnlock()
		return id, nil
	}
	c.customerMu.RUnlock()

	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/customers", nil)
	if err != nil {
		return "", fmt.Errorf("list customers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var customers []customerResponse
		if err := json.NewDecoder(resp.Body).Decode(&customers); err == nil {
			for _, cust := range customers {
				if cust.ExternalID == externalID {
					c.customerMu.Lock()
					c.customerCache[externalID] = cust.ID
					c.customerMu.Unlock()
					return cust.ID, nil
				}
			}
		}
	}

	if !c.opts.autoCreateCustomer {
		return "", fmt.Errorf("customer not found for external_id %s", externalID)
	}

	createReq := map[string]string{
		"name":        externalID,
		"email":       c.opts.customerEmailFunc(externalID),
		"external_id": externalID,
	}

	resp2, err := c.doRequest(ctx, http.MethodPost, "/v1/customers", createReq)
	if err != nil {
		return "", fmt.Errorf("create customer: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		return "", fmt.Errorf("create customer: status %d: %s", resp2.StatusCode, string(body))
	}

	var created customerResponse
	if err := json.NewDecoder(resp2.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode created customer: %w", err)
	}

	c.customerMu.Lock()
	c.customerCache[externalID] = created.ID
	c.customerMu.Unlock()

	return created.ID, nil
}
