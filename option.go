package spillway

import (
	"fmt"
	"log"
	"net/http"
)

// Logger is the interface used by Client for diagnostic output.
type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

// Option configures a Client.
type Option func(*options)

type options struct {
	httpClient          *http.Client
	logger              Logger
	channelSize         int
	useRules            bool
	autoCreateCustomer  bool
	customerEmailFunc   func(externalID string) string
	failClosed          bool
}

func defaults() options {
	return options{
		channelSize:        1000,
		useRules:           true,
		autoCreateCustomer: true,
		customerEmailFunc: func(externalID string) string {
			return fmt.Sprintf("%s@spillway.local", externalID)
		},
	}
}

// WithHTTPClient sets a custom HTTP client. Default: 10s timeout.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.httpClient = c }
}

// WithLogger sets a custom logger. Default: log.Default().
func WithLogger(l Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithStdLogger wraps a *log.Logger to satisfy the Logger interface.
func WithStdLogger(l *log.Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithChannelSize sets the event buffer capacity. Default: 1000.
func WithChannelSize(size int) Option {
	return func(o *options) { o.channelSize = size }
}

// WithUseRules controls whether ?use_rules=true is appended to event POSTs.
// Default: true.
func WithUseRules(v bool) Option {
	return func(o *options) { o.useRules = v }
}

// WithAutoCreateCustomer controls whether customers are auto-created on first
// event. Default: true.
func WithAutoCreateCustomer(v bool) Option {
	return func(o *options) { o.autoCreateCustomer = v }
}

// WithCustomerEmail sets the function used to generate an email address for
// auto-created customers. Default: "{id}@spillway.local".
func WithCustomerEmail(fn func(externalID string) string) Option {
	return func(o *options) { o.customerEmailFunc = fn }
}

// WithFailClosed controls whether quota check errors cause requests to be
// rejected instead of allowed. Default: false (fail open).
func WithFailClosed(v bool) Option {
	return func(o *options) { o.failClosed = v }
}
