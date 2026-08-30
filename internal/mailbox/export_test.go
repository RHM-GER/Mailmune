package mailbox

import (
	"crypto/tls"
	"time"
)

// NewTestClient returns a client trusting the provided TLS template, which
// tests use to install a self-signed test CA. Certificate verification,
// hostname checking and TLS 1.2 minimum stay enforced by tlsConfig.
// Production code must always use NewClient.
func NewTestClient(tlsTemplate *tls.Config, timeout time.Duration) *Client {
	return &Client{timeout: timeout, tlsTemplate: tlsTemplate}
}
