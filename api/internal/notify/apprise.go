// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	gerr "github.com/golusoris/golusoris/core/errors"
	httpclient "github.com/golusoris/golusoris/httpx/client"
	"github.com/golusoris/golusoris/notify"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// appriseRetries is the number of retries after the first attempt: transport
// errors, 429 and 5xx are retried, 424 and other 4xx are not, because a 424
// can follow a partial delivery.
const appriseRetries = 2

// AppriseOptions configures the Apprise API sender.
type AppriseOptions struct {
	// NotifyURL is the full notify endpoint, usually {server}/notify/{config key}.
	// The config key grants sending, so the URL is a secret. Required.
	NotifyURL string
	// Username and Password, when set, are sent as HTTP Basic Auth.
	Username string
	Password string
	// Tags selects destinations of the configuration; any matching tag receives the message.
	Tags []string
	// RetryWait is the first backoff between attempts; non-positive means 500ms.
	RetryWait time.Duration
}

// AppriseSender posts notify.Messages to an Apprise API server.
type AppriseSender struct {
	opts   AppriseOptions
	target *url.URL
	hc     *http.Client
}

type appriseBody struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body"`
	Type  string `json:"type"`
	Tag   string `json:"tag,omitempty"`
}

// NewAppriseSender returns an Apprise API sender.
func NewAppriseSender(opts AppriseOptions) (*AppriseSender, error) {
	u, err := url.Parse(opts.NotifyURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: apprise needs an http or https notify URL", domain.ErrInvalid)
	}
	hc := httpclient.New(httpclient.Options{
		Name:    "snatcharr.notify.apprise",
		Timeout: 20 * time.Second,
		Retry:   httpclient.RetryOptions{Max: appriseRetries, Wait: opts.RetryWait, MaxWait: 5 * time.Second, AllowUnsafe: true},
		// The retry layer logs the request URL, which carries the config key.
		Logger: slog.New(slog.DiscardHandler),
	})
	return &AppriseSender{opts: opts, target: u, hc: hc}, nil
}

// Name implements notify.Sender.
func (s *AppriseSender) Name() string { return "apprise" }

// Send implements notify.Sender. msg.Metadata["level"] picks the Apprise message type.
func (s *AppriseSender) Send(ctx context.Context, msg notify.Message) (err error) {
	body := msg.Body
	if body == "" {
		body = msg.Text
	}
	payload, err := json.Marshal(appriseBody{
		Title: msg.Subject,
		Body:  body,
		Type:  appriseType(msg.Metadata["level"]),
		Tag:   strings.Join(s.opts.Tags, ","),
	})
	if err != nil {
		return fmt.Errorf("notify/apprise: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.target.String(), bytes.NewReader(payload))
	if err != nil {
		return s.scrub(fmt.Errorf("notify/apprise: new request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.opts.Username != "" || s.opts.Password != "" {
		req.SetBasicAuth(s.opts.Username, s.opts.Password)
	}
	resp, err := s.hc.Do(req) //nolint:bodyclose // closed via gerr.CloseInto on the deferred line below
	if err != nil {
		return s.scrub(fmt.Errorf("notify/apprise: post: %w", err))
	}
	defer gerr.CloseInto(resp.Body, &err, "notify/apprise: close response body")
	if resp.StatusCode/100 != 2 {
		detail, _ := httpclient.ReadAllBounded(resp.Body, 1<<10)
		return s.scrub(fmt.Errorf("notify/apprise: status %d: %s", resp.StatusCode, bytes.TrimSpace(detail)))
	}
	return nil
}

// appriseType maps an event level onto Apprise's info, warning and failure types.
func appriseType(level string) string {
	switch level {
	case "error":
		return "failure"
	case "warn":
		return "warning"
	default:
		return "info"
	}
}

// scrub removes the notify URL and its config key from err's text; HTTP client
// errors quote the request URL, with the password masked but the path intact.
func (s *AppriseSender) scrub(err error) error {
	msg := err.Error()
	bare := *s.target
	bare.User = nil
	for _, secret := range []string{s.target.String(), s.target.Redacted(), bare.String(), s.target.EscapedPath(), s.opts.Password} {
		if secret != "" && secret != "/" {
			msg = strings.ReplaceAll(msg, secret, "[REDACTED]")
		}
	}
	return &scrubbedError{msg: msg, err: err}
}

type scrubbedError struct {
	msg string
	err error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.err }
