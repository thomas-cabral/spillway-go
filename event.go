package spillway

import (
	"context"
	"io"
	"time"
)

// event represents a usage event to be sent to spillway.
type event struct {
	UserID   string                 // resolved to spillway customer UUID in sendLoop
	Name     string
	Value    float64
	Metadata map[string]interface{}
}

// TrackEvent enqueues a usage event for async delivery. Non-blocking; drops
// the event with a warning if the channel is full.
func (c *Client) TrackEvent(userID, eventName string, value float64, metadata map[string]interface{}) {
	if c == nil {
		return
	}
	evt := event{
		UserID:   userID,
		Name:     eventName,
		Value:    value,
		Metadata: metadata,
	}
	select {
	case c.eventCh <- evt:
	default:
		c.logger.Printf("[spillway] Event channel full, dropping event %s for user %s", eventName, userID)
	}
}

// sendLoop reads events from the channel and sends them to spillway.
func (c *Client) sendLoop() {
	defer c.wg.Done()

	for evt := range c.eventCh {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		c.sendEvent(ctx, evt)
		cancel()
	}
}

// sendEvent resolves the customer ID and sends a single event to spillway.
func (c *Client) sendEvent(ctx context.Context, evt event) {
	customerID, err := c.resolveCustomerID(ctx, evt.UserID)
	if err != nil {
		c.logger.Printf("[spillway] sendEvent: failed to resolve customer for %s: %v", evt.UserID, err)
		return
	}

	payload := map[string]interface{}{
		"customer_id": customerID,
		"event_name":  evt.Name,
		"value":       evt.Value,
	}
	if evt.Metadata != nil {
		payload["metadata"] = evt.Metadata
	}

	path := "/v1/events"
	if c.opts.useRules {
		path += "?use_rules=true"
	}

	resp, err := c.doRequest(ctx, "POST", path, payload)
	if err != nil {
		c.logger.Printf("[spillway] sendEvent: failed to send %s for customer %s: %v", evt.Name, customerID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 202 && resp.StatusCode != 200 && resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Printf("[spillway] sendEvent: unexpected status %d for %s: %s", resp.StatusCode, evt.Name, string(body))
		return
	}

	c.logger.Printf("[spillway] sendEvent: sent %s (value=%.0f) for customer %s", evt.Name, evt.Value, customerID)
}
