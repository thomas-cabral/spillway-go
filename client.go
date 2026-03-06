package spillway

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"
)

// ErrQuotaExhausted is returned when a user's quota is fully consumed.
var ErrQuotaExhausted = errors.New("spillway: quota exhausted")

// ErrQuotaCheckFailed is returned in fail-closed mode when a quota check
// encounters an error (network, server, decode). See WithFailClosed.
var ErrQuotaCheckFailed = errors.New("spillway: quota check failed")

// Client is a non-blocking spillway usage-tracking client.
// All public methods are nil-receiver safe.
type Client struct {
	baseURL string
	apiKey  string
	opts    options

	httpClient *http.Client
	logger     Logger

	customerMu    sync.RWMutex
	customerCache map[string]string // external user_id -> spillway customer UUID

	eventCh chan event
	wg      sync.WaitGroup
}

// New creates a new spillway Client. Returns nil if apiKey is empty,
// making all subsequent method calls safe no-ops.
func New(baseURL, apiKey string, opts ...Option) *Client {
	o := defaults()
	for _, fn := range opts {
		fn(&o)
	}

	logger := o.logger
	if logger == nil {
		logger = log.Default()
	}

	if apiKey == "" {
		logger.Println("[spillway] No API key configured, client disabled")
		return nil
	}

	httpClient := o.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Client{
		baseURL:       baseURL,
		apiKey:        apiKey,
		opts:          o,
		httpClient:    httpClient,
		logger:        logger,
		customerCache: make(map[string]string),
		eventCh:       make(chan event, o.channelSize),
	}
}

// Start launches the background sendLoop goroutine.
func (c *Client) Start() {
	if c == nil {
		return
	}
	c.wg.Add(1)
	go c.sendLoop()
}

// Shutdown closes the event channel and waits for the sendLoop to drain,
// respecting the provided context deadline.
func (c *Client) Shutdown(ctx context.Context) {
	if c == nil {
		return
	}
	close(c.eventCh)

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Println("[spillway] Shutdown complete, all events drained")
	case <-ctx.Done():
		c.logger.Println("[spillway] Shutdown timed out, some events may be lost")
	}
}
