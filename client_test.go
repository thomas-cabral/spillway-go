package spillway

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *log.Logger {
	return log.New(os.Stderr, "", log.LstdFlags)
}

func TestNilClientNoOps(t *testing.T) {
	var c *Client

	c.Start()
	c.TrackEvent("user1", "test.event", 1, nil)
	if err := c.CheckQuota(context.Background(), "user1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	usage, err := c.CheckQuotaByRule(context.Background(), "user1", "rule")
	if err != nil || usage != nil {
		t.Fatalf("expected nil/nil, got %v/%v", usage, err)
	}
	c.Shutdown(context.Background())
}

func TestNewReturnsNilWhenNoAPIKey(t *testing.T) {
	c := New("http://localhost", "", WithStdLogger(testLogger()))
	if c != nil {
		t.Fatal("expected nil client when API key is empty")
	}
}

func TestNewReturnsClientWithAPIKey(t *testing.T) {
	c := New("http://localhost", "test-key", WithStdLogger(testLogger()))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestTrackEventAndSendLoop(t *testing.T) {
	var eventsSent int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/events" && r.Method == http.MethodPost:
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["customer_id"] != "cust-uuid-1" {
				t.Errorf("expected customer_id cust-uuid-1, got %v", payload["customer_id"])
			}
			if payload["event_name"] != "issue.created" {
				t.Errorf("expected event_name issue.created, got %v", payload["event_name"])
			}
			if payload["value"] != float64(1) {
				t.Errorf("expected value 1, got %v", payload["value"])
			}
			atomic.AddInt32(&eventsSent, 1)
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	c.Start()

	c.TrackEvent("user1", "issue.created", 1, map[string]interface{}{"resource_id": "issue-123"})

	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Shutdown(ctx)

	if got := atomic.LoadInt32(&eventsSent); got != 1 {
		t.Fatalf("expected 1 event sent, got %d", got)
	}
}

func TestTrackEventWithUseRulesFalse(t *testing.T) {
	var receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/events" && r.Method == http.MethodPost:
			receivedPath = r.URL.String()
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()), WithUseRules(false))
	c.Start()
	c.TrackEvent("user1", "test.event", 1, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Shutdown(ctx)

	if receivedPath != "/v1/events" {
		t.Fatalf("expected /v1/events without query, got %s", receivedPath)
	}
}

func TestTrackEventWithUseRulesTrue(t *testing.T) {
	var receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/events" && r.Method == http.MethodPost:
			receivedPath = r.URL.String()
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger())) // default useRules=true
	c.Start()
	c.TrackEvent("user1", "test.event", 1, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Shutdown(ctx)

	if receivedPath != "/v1/events?use_rules=true" {
		t.Fatalf("expected /v1/events?use_rules=true, got %s", receivedPath)
	}
}

func TestTrackEventWithGuarantees(t *testing.T) {
	var receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/events" && r.Method == http.MethodPost:
			receivedPath = r.URL.String()
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()), WithUseRules(false), WithGuarantees(true))
	c.Start()
	c.TrackEvent("user1", "test.event", 1, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Shutdown(ctx)

	if receivedPath != "/v1/events?with_guarantees=true" {
		t.Fatalf("expected /v1/events?with_guarantees=true, got %s", receivedPath)
	}
}

func TestTrackEventWithBothParams(t *testing.T) {
	var receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/events" && r.Method == http.MethodPost:
			receivedPath = r.URL.String()
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()), WithUseRules(true), WithGuarantees(true))
	c.Start()
	c.TrackEvent("user1", "test.event", 1, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Shutdown(ctx)

	if receivedPath != "/v1/events?use_rules=true&with_guarantees=true" {
		t.Fatalf("expected /v1/events?use_rules=true&with_guarantees=true, got %s", receivedPath)
	}
}

func TestCheckQuotaAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/quota-rules/usage/cust-uuid-1":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]QuotaRuleUsage{
				{Remaining: 499000},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	if err := c.CheckQuota(context.Background(), "user1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckQuotaExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/quota-rules/usage/cust-uuid-1":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]QuotaRuleUsage{
				{Remaining: 0},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	err := c.CheckQuota(context.Background(), "user1")
	if err != ErrQuotaExhausted {
		t.Fatalf("expected ErrQuotaExhausted, got %v", err)
	}
}

func TestCheckQuotaByRuleAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/quota-rules/usage/cust-uuid-1":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]QuotaRuleUsage{
				{RuleName: "Ticket Operations", Remaining: 100, Limit: 500, CurrentUsage: 400},
				{RuleName: "Document Operations", Remaining: 50, Limit: 100, CurrentUsage: 50},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	usage, err := c.CheckQuotaByRule(context.Background(), "user1", "Ticket Operations")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.Remaining != 100 {
		t.Fatalf("expected remaining=100, got %f", usage.Remaining)
	}
}

func TestCheckQuotaByRuleExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/quota-rules/usage/cust-uuid-1":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]QuotaRuleUsage{
				{RuleName: "Ticket Operations", Remaining: 0, Limit: 500, CurrentUsage: 500},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	usage, err := c.CheckQuotaByRule(context.Background(), "user1", "Ticket Operations")
	if err != ErrQuotaExhausted {
		t.Fatalf("expected ErrQuotaExhausted, got %v", err)
	}
	if usage == nil || usage.RuleName != "Ticket Operations" {
		t.Fatal("expected usage with rule name")
	}
}

func TestCheckQuotaByRuleNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/quota-rules/usage/cust-uuid-1":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]QuotaRuleUsage{
				{RuleName: "Other Rule", Remaining: 100},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	usage, err := c.CheckQuotaByRule(context.Background(), "user1", "Nonexistent")
	if err != nil {
		t.Fatalf("expected nil error (fail open), got %v", err)
	}
	if usage != nil {
		t.Fatalf("expected nil usage, got %v", usage)
	}
}

func TestCustomerAutoCreation(t *testing.T) {
	var customerCreated int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{})
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodPost:
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			if req["external_id"] != "new-user" {
				t.Errorf("expected external_id=new-user, got %s", req["external_id"])
			}
			if req["email"] != "new-user@spillway.local" {
				t.Errorf("expected default email, got %s", req["email"])
			}
			atomic.AddInt32(&customerCreated, 1)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(customerResponse{ID: "new-cust-uuid", ExternalID: "new-user"})
		case r.URL.Path == "/v1/quota-rules/usage/new-cust-uuid":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]QuotaRuleUsage{
				{Remaining: 500000},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	if err := c.CheckQuota(context.Background(), "new-user"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := atomic.LoadInt32(&customerCreated); got != 1 {
		t.Fatalf("expected 1 customer created, got %d", got)
	}

	// Second call should use cache
	if err := c.CheckQuota(context.Background(), "new-user"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := atomic.LoadInt32(&customerCreated); got != 1 {
		t.Fatalf("expected still 1 customer created (cached), got %d", got)
	}
}

func TestCustomerEmailOption(t *testing.T) {
	var receivedEmail string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{})
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodPost:
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			receivedEmail = req["email"]
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(customerResponse{ID: "cust-1", ExternalID: "user1"})
		case r.URL.Path == "/v1/quota-rules/usage/cust-1":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]QuotaRuleUsage{{Remaining: 100}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key",
		WithStdLogger(testLogger()),
		WithCustomerEmail(func(id string) string { return id + "@ticketing.local" }),
	)
	c.CheckQuota(context.Background(), "user1")

	if receivedEmail != "user1@ticketing.local" {
		t.Fatalf("expected user1@ticketing.local, got %s", receivedEmail)
	}
}

func TestCheckQuotaFailsOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	if err := c.CheckQuota(context.Background(), "user1"); err != nil {
		t.Fatalf("expected nil (fail open), got %v", err)
	}
}

func TestCheckQuotaFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()), WithFailClosed(true))
	err := c.CheckQuota(context.Background(), "user1")
	if err != ErrQuotaCheckFailed {
		t.Fatalf("expected ErrQuotaCheckFailed, got %v", err)
	}
}

func TestCheckQuotaByRuleFailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()), WithFailClosed(true))
	_, err := c.CheckQuotaByRule(context.Background(), "user1", "some-rule")
	if err != ErrQuotaCheckFailed {
		t.Fatalf("expected ErrQuotaCheckFailed, got %v", err)
	}
}

func TestFailClosedDefaultOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	// CheckQuota should fail open by default
	if err := c.CheckQuota(context.Background(), "user1"); err != nil {
		t.Fatalf("expected nil (default fail open), got %v", err)
	}
	// CheckQuotaByRule should also fail open by default
	_, err := c.CheckQuotaByRule(context.Background(), "user1", "rule")
	if err != nil {
		t.Fatalf("expected nil (default fail open), got %v", err)
	}
}

func TestShutdownDrainsChannel(t *testing.T) {
	var eventsSent int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{
				{ID: "cust-uuid-1", ExternalID: "user1"},
			})
		case r.URL.Path == "/v1/events" && r.Method == http.MethodPost:
			atomic.AddInt32(&eventsSent, 1)
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()))
	c.Start()

	for i := 0; i < 5; i++ {
		c.TrackEvent("user1", "issue.created", 1, nil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.Shutdown(ctx)

	if got := atomic.LoadInt32(&eventsSent); got != 5 {
		t.Fatalf("expected 5 events sent, got %d", got)
	}
}

func TestAPIKeyHeader(t *testing.T) {
	var receivedKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]customerResponse{
			{ID: "cust-uuid-1", ExternalID: "user1"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "my-secret-key", WithStdLogger(testLogger()))
	c.resolveCustomerID(context.Background(), "user1")

	if receivedKey != "my-secret-key" {
		t.Fatalf("expected API key my-secret-key, got %s", receivedKey)
	}
}

func TestWithChannelSize(t *testing.T) {
	c := New("http://localhost", "test-key", WithChannelSize(50), WithStdLogger(testLogger()))
	if cap(c.eventCh) != 50 {
		t.Fatalf("expected channel size 50, got %d", cap(c.eventCh))
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 30 * time.Second}
	c := New("http://localhost", "test-key", WithHTTPClient(custom), WithStdLogger(testLogger()))
	if c.httpClient != custom {
		t.Fatal("expected custom HTTP client to be used")
	}
}

func TestAutoCreateCustomerDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]customerResponse{})
		case r.URL.Path == "/v1/customers" && r.Method == http.MethodPost:
			t.Error("should not attempt to create customer")
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", WithStdLogger(testLogger()), WithAutoCreateCustomer(false))
	// CheckQuota should fail open since customer can't be resolved
	if err := c.CheckQuota(context.Background(), "unknown-user"); err != nil {
		t.Fatalf("expected nil (fail open), got %v", err)
	}
}
