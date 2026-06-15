//go:build e2e

// Package spillway e2e test. Exercises the SDK against a LIVE spillway stack to
// prove the new quota model wiring (quota-status read + event ingest + quota
// enforcement) works end-to-end.
//
// Run:
//
//	SPILLWAY_E2E_URL=http://localhost:8080 go test -tags e2e -run TestE2E -v .
//
// Bootstrap mirrors the spillway-api e2e harness: POST /auth/register to mint an
// org+token, flip enforcement_override in the stack's Postgres to bypass the
// subscription gate, mint an API key, then create a meter+quota so quota-status
// has something to report. After that the SDK does the real work.
package spillway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func e2eURL(t *testing.T) string {
	u := os.Getenv("SPILLWAY_E2E_URL")
	if u == "" {
		t.Skip("SPILLWAY_E2E_URL not set; skipping live e2e")
	}
	return u
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// httpJSON does a JSON request and returns status + raw body.
func httpJSON(t *testing.T, method, url string, body any, token, orgID string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if orgID != "" {
		req.Header.Set("X-Organization-ID", orgID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// seedOrg registers a fresh org+user and returns (token, orgID).
func seedOrg(t *testing.T, base string) (string, string) {
	t.Helper()
	uniq := uuid.New().String()[:8]
	payload := map[string]any{
		"name":     "SDK E2E",
		"email":    fmt.Sprintf("sdk-e2e-%s@test.invalid", uniq),
		"password": "sdk-e2e-password-" + uniq,
		"org_name": "SDK E2E Org " + uniq,
		"price_id": "price_e2e_placeholder",
	}
	st, body := httpJSON(t, "POST", base+"/auth/register", payload, "", "")
	if st != http.StatusCreated {
		t.Fatalf("register: %d: %s", st, body)
	}
	var reg struct {
		AccessToken string `json:"access_token"`
		User        struct {
			OrganizationID *string `json:"organization_id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &reg); err != nil {
		t.Fatalf("decode register: %v: %s", err, body)
	}
	if reg.AccessToken == "" || reg.User.OrganizationID == nil {
		t.Fatalf("register missing token/org: %s", body)
	}
	return reg.AccessToken, *reg.User.OrganizationID
}

// enableOrg bypasses the subscription gate for a test org (same trick the
// spillway e2e harness uses): flip enforcement_override in the stack Postgres.
func enableOrg(t *testing.T, orgID string) {
	t.Helper()
	container := envOr("SPILLWAY_E2E_PG_CONTAINER", "spillway-brod-postgres-1")
	db := envOr("SPILLWAY_E2E_PG_DB", "mydb")
	user := envOr("SPILLWAY_E2E_PG_USER", "postgres")
	sql := fmt.Sprintf("UPDATE organizations SET enforcement_override=true WHERE id='%s';", orgID)
	out, err := exec.Command("docker", "exec", container, "psql", "-U", user, "-d", db, "-c", sql).CombinedOutput()
	if err != nil {
		t.Fatalf("enableOrg: docker exec psql: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "UPDATE 1") {
		t.Fatalf("enableOrg: expected UPDATE 1, got: %s", out)
	}
}

func mintAPIKey(t *testing.T, base, token, orgID string) string {
	t.Helper()
	st, body := httpJSON(t, "POST", base+"/v1/api-keys",
		map[string]any{"name": "sdk-e2e-" + uuid.New().String()[:8]}, token, orgID)
	if st != http.StatusCreated {
		t.Fatalf("mint api key: %d: %s", st, body)
	}
	var res struct {
		Key string `json:"key"`
	}
	json.Unmarshal(body, &res)
	if res.Key == "" {
		t.Fatalf("empty api key: %s", body)
	}
	return res.Key
}

// createMeterAndQuota wires a COUNT meter on eventName and a HARD_CUTOFF quota
// with the given limit and a known quota name. Returns the quota name.
func createMeterAndQuota(t *testing.T, base, token, orgID, eventName, quotaName string, limit float64) {
	t.Helper()
	uniq := uuid.New().String()[:8]
	st, body := httpJSON(t, "POST", base+"/v1/meters", map[string]any{
		"name":             "meter-" + uniq,
		"slug":             "meter-" + uniq,
		"event_name":       eventName,
		"aggregation_type": "COUNT",
		"value_field":      "value",
	}, token, orgID)
	if st != http.StatusCreated {
		t.Fatalf("create meter: %d: %s", st, body)
	}
	var m struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &m)

	st, body = httpJSON(t, "POST", base+"/v1/quotas", map[string]any{
		"name":                     quotaName,
		"meter_id":                 m.ID,
		"default_limit":            limit,
		"default_overage_behavior": "HARD_CUTOFF",
		"reset_period":             "MONTHLY",
	}, token, orgID)
	if st != http.StatusCreated {
		t.Fatalf("create quota: %d: %s", st, body)
	}
}

// TestE2EQuotaFlow is the full live round-trip through the SDK.
func TestE2EQuotaFlow(t *testing.T) {
	base := e2eURL(t)

	token, orgID := seedOrg(t, base)
	enableOrg(t, orgID)
	apiKey := mintAPIKey(t, base, token, orgID)
	t.Logf("org=%s apiKey=%s…", orgID, apiKey[:min(10, len(apiKey))])

	const (
		eventName = "e2e.usage"
		quotaName = "e2e_quota"
		limit     = 3.0
	)
	createMeterAndQuota(t, base, token, orgID, eventName, quotaName, limit)

	userID := "sdk-e2e-user-" + uuid.New().String()[:8]

	// Default options (use_rules=true) — the meter-quota enforcement path this
	// stack actually wires, and exactly what issuehive-go uses in production.
	client := New(base, apiKey, WithStdLogger(testLogger()))
	if client == nil {
		t.Fatal("nil client")
	}
	client.Start()

	// 1) Fresh customer: quota present, fully available, not exhausted.
	if err := client.CheckQuota(context.Background(), userID); err != nil {
		t.Fatalf("CheckQuota (fresh) expected nil, got %v", err)
	}
	status, err := client.CheckQuotaByName(context.Background(), userID, quotaName)
	if err != nil {
		t.Fatalf("CheckQuotaByName (fresh) error: %v", err)
	}
	if status == nil {
		t.Fatalf("CheckQuotaByName (fresh): quota %q not found", quotaName)
	}
	t.Logf("fresh quota: name=%s meter=%s limit=%.0f usage=%.0f remaining=%.0f overage=%s",
		status.QuotaName, status.MeterName, status.Limit, status.Usage, status.Remaining, status.OverageBehavior)
	if status.Limit != limit {
		t.Fatalf("expected limit %.0f, got %.0f", limit, status.Limit)
	}
	if status.Remaining != limit {
		t.Fatalf("expected remaining %.0f (unused), got %.0f", limit, status.Remaining)
	}

	// 2) Consume the quota. COUNT meter → each event is +1; send limit+2 so the
	// last two are rejected server-side (429, logged + dropped by the SDK).
	for i := 0; i < int(limit)+2; i++ {
		client.TrackEvent(userID, eventName, 1, map[string]any{"i": i})
	}

	// Drain the async send loop so every POST has completed before we re-check.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client.Shutdown(ctx)

	// 3) Quota should now be exhausted (HARD_CUTOFF, remaining <= 0).
	// Usage is tracked in Redis at ingest, so this reflects immediately; poll a
	// few times to absorb any tiny lag.
	var lastErr error
	var final *QuotaStatus
	for attempt := 0; attempt < 10; attempt++ {
		final, lastErr = client.CheckQuotaByName(context.Background(), userID, quotaName)
		if lastErr == ErrQuotaExhausted {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if final != nil {
		t.Logf("post-usage quota: usage=%.0f remaining=%.0f err=%v", final.Usage, final.Remaining, lastErr)
	}
	if lastErr != ErrQuotaExhausted {
		t.Fatalf("expected ErrQuotaExhausted after consuming quota, got %v (status=%+v)", lastErr, final)
	}
	if err := client.CheckQuota(context.Background(), userID); err != ErrQuotaExhausted {
		t.Fatalf("CheckQuota expected ErrQuotaExhausted, got %v", err)
	}

	// 4) Verify the allowed events actually landed in ClickHouse (billable truth).
	// Only the within-limit events are produced; the 429'd ones are not. The
	// processor writes CH asynchronously, so poll until the count settles.
	count := chDistinctEventCountE2E(t, orgID, int(limit))
	t.Logf("clickhouse distinct events for org: %d", count)
	if count < int(limit) {
		t.Fatalf("expected >= %d events in clickhouse, got %d", int(limit), count)
	}
}

// chDistinctEventCountE2E reads the deduped event count for an org from the
// stack's ClickHouse, polling until it reaches want (the processor writes
// asynchronously) or the deadline elapses. Returns the last observed count.
func chDistinctEventCountE2E(t *testing.T, orgID string, want int) int {
	t.Helper()
	container := envOr("SPILLWAY_E2E_CH_CONTAINER", "spillway-brod-clickhouse-1")
	q := fmt.Sprintf("SELECT uniqExact(event_id) FROM default.usage_events WHERE organization_id='%s'", orgID)
	deadline := time.Now().Add(30 * time.Second)
	var last int
	for {
		out, err := exec.Command("docker", "exec", container, "clickhouse-client", "--query", q).CombinedOutput()
		if err == nil {
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &last)
		}
		if last >= want || time.Now().After(deadline) {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
}
