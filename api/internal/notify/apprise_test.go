// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gonotify "github.com/golusoris/golusoris/notify"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/notify"
)

const appriseKey = "snatcharr-secret-key"

// appriseServer answers each request with the next status of statuses (the
// last one repeats) and counts the requests.
type appriseServer struct {
	*httptest.Server
	calls    atomic.Int32
	lastBody atomic.Pointer[map[string]any]
	lastAuth atomic.Pointer[string]
	lastPath atomic.Pointer[string]
}

func newAppriseServer(t *testing.T, statuses ...int) *appriseServer {
	t.Helper()
	s := &appriseServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(s.calls.Add(1))
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		auth, path := r.Header.Get("Authorization"), r.URL.Path
		s.lastBody.Store(&body)
		s.lastAuth.Store(&auth)
		s.lastPath.Store(&path)
		w.WriteHeader(statuses[min(n, len(statuses))-1])
	}))
	t.Cleanup(s.Close)
	return s
}

func newApprise(t *testing.T, opts notify.AppriseOptions) *notify.AppriseSender {
	t.Helper()
	opts.RetryWait = time.Millisecond
	s, err := notify.NewAppriseSender(opts)
	if err != nil {
		t.Fatalf("NewAppriseSender: %v", err)
	}
	return s
}

func sendApprise(t *testing.T, s *notify.AppriseSender, level string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	return s.Send(ctx, gonotify.Message{Subject: "Circuit open", Body: "radarr unreachable", Metadata: map[string]string{"level": level}})
}

func TestAppriseSend_Request(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, level, wantType string
		tags                  []string
		wantTag               any
	}{
		{name: "error is failure", level: "error", wantType: "failure", tags: []string{"ops"}, wantTag: "ops"},
		{name: "warn is warning", level: "warn", wantType: "warning", tags: []string{"ops", "home"}, wantTag: "ops,home"},
		{name: "info stays info", level: "info", wantType: "info"},
		{name: "unknown level is info", level: "", wantType: "info"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newAppriseServer(t, http.StatusOK)
			s := newApprise(t, notify.AppriseOptions{
				NotifyURL: srv.URL + "/notify/" + appriseKey, Username: "snatch", Password: "pw", Tags: tc.tags,
			})
			if err := sendApprise(t, s, tc.level); err != nil {
				t.Fatalf("Send: %v", err)
			}
			body := *srv.lastBody.Load()
			if body["type"] != tc.wantType || body["title"] != "Circuit open" || body["body"] != "radarr unreachable" {
				t.Errorf("body = %v, want type %q", body, tc.wantType)
			}
			if body["tag"] != tc.wantTag {
				t.Errorf("tag = %v, want %v", body["tag"], tc.wantTag)
			}
			if *srv.lastPath.Load() != "/notify/"+appriseKey {
				t.Errorf("path = %q", *srv.lastPath.Load())
			}
			if !strings.HasPrefix(*srv.lastAuth.Load(), "Basic ") {
				t.Errorf("Authorization = %q, want Basic", *srv.lastAuth.Load())
			}
		})
	}
}

func TestAppriseSend_Retries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		statuses  []int
		wantErr   bool
		wantCalls int32
	}{
		{name: "503 then 200 succeeds on the second attempt", statuses: []int{503, 200}, wantCalls: 2},
		{name: "429 then 200 succeeds", statuses: []int{429, 200}, wantCalls: 2},
		{name: "three 503 give up after three attempts", statuses: []int{503}, wantErr: true, wantCalls: 3},
		{name: "424 partial delivery is sent once", statuses: []int{424}, wantErr: true, wantCalls: 1},
		{name: "401 is sent once", statuses: []int{401}, wantErr: true, wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newAppriseServer(t, tc.statuses...)
			s := newApprise(t, notify.AppriseOptions{NotifyURL: srv.URL + "/notify/" + appriseKey})
			err := sendApprise(t, s, "info")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Send err = %v, wantErr %v", err, tc.wantErr)
			}
			if got := srv.calls.Load(); got != tc.wantCalls {
				t.Errorf("calls = %d, want %d", got, tc.wantCalls)
			}
			if err != nil && strings.Contains(err.Error(), appriseKey) {
				t.Errorf("error leaks the config key: %v", err)
			}
		})
	}
}

func TestAppriseSend_UnreachableHidesSecrets(t *testing.T) {
	t.Parallel()
	srv := newAppriseServer(t, http.StatusOK)
	target := strings.Replace(srv.URL, "http://", "http://snatch:url-secret-pw@", 1) + "/notify/" + appriseKey
	srv.Close()
	s := newApprise(t, notify.AppriseOptions{NotifyURL: target, Password: "opt-secret-pw"})

	err := sendApprise(t, s, "error")
	if err == nil {
		t.Fatal("Send to a closed server succeeded")
	}
	for _, secret := range []string{appriseKey, "url-secret-pw", "opt-secret-pw"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks %q: %v", secret, err)
		}
	}
}

func TestNewAppriseSender_RefusesBadURL(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "apprise:8000/notify/key", "ftp://apprise/notify/key", "http:///notify/key", "://bad"} {
		_, err := notify.NewAppriseSender(notify.AppriseOptions{NotifyURL: raw})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("NewAppriseSender(%q) err = %v, want domain.ErrInvalid", raw, err)
		}
	}
}

func TestDeliver_Apprise(t *testing.T) {
	t.Parallel()
	srv := newAppriseServer(t, http.StatusOK)
	ch := domain.NotificationChannel{
		Name: "apprise", Kind: domain.ChannelApprise,
		Config: domain.ChannelConfig{WebhookURL: srv.URL + "/notify/" + appriseKey, Tags: []string{"ops"}},
	}
	err := notify.Deliver(t.Context(), ch, notify.Payload{Title: "Stamina exhausted", Level: "warn"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	body := *srv.lastBody.Load()
	if body["type"] != "warning" || body["body"] != "Stamina exhausted" || body["tag"] != "ops" {
		t.Errorf("body = %v", body)
	}
}
