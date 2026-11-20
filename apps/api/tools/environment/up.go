package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// upAskTimeout bounds one request to /up.
	upAskTimeout = 10 * time.Second
	// upRetryInterval is the pause between two requests to /up.
	upRetryInterval = 5 * time.Second
)

// upOverHTTPS waits for the app of a server from where the tool runs: it
// asks for /up over HTTPS, through Caddy, trusting the certificate
// authorities of the system, until /up answers 200. That proves the whole
// way in to staging: the host name, the firewall, the certificate, Caddy and
// the readiness of the API.
type upOverHTTPS struct {
	client   *http.Client
	interval time.Duration
}

// WaitUntilUp asks until /up answers 200 or the context ends. Giving up, it
// says what the last answer was.
func (u upOverHTTPS) WaitUntilUp(ctx context.Context, url string) error {
	reason := u.ask(ctx, url)
	for reason != nil && u.pause(ctx) {
		reason = unlessCutShort(ctx, u.ask(ctx, url), reason)
	}
	if reason != nil {
		return fmt.Errorf("%s did not answer 200 in time: %w", url, reason)
	}
	return nil
}

// pause waits one interval and tells whether the context allows another
// request.
func (u upOverHTTPS) pause(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(u.interval):
		return true
	}
}

// ask asks once, and says why /up is not up yet, or nil when it is.
func (u upOverHTTPS) ask(ctx context.Context, url string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := u.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("it answered %s", response.Status)
	}
	return nil
}

// unlessCutShort keeps the reason before when the end of the context cut
// the request short, which says nothing about the app.
func unlessCutShort(ctx context.Context, reason, before error) error {
	if reason != nil && ctx.Err() != nil {
		return before
	}
	return reason
}
