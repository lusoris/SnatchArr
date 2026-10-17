// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A connected client receives each new record as a default message event with id = seq and
// the redacted record, level as its name, as data.
func TestStreamPublishesRedactedRecords(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	srv := httptest.NewServer(r.StreamHandler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	// The hub flushes the headers after it registered the client, so this record reaches it.
	slog.New(r.Wrap(debugHandler(), SourceAPI)).Warn("probe", "api_key", plantedHex)

	frame := readFrame(t, bufio.NewScanner(resp.Body))
	// The SPA's SSE client listens to the default "message" type only; a named event never reaches it.
	if frame["id"] != "1" || frame["event"] != "message" {
		t.Fatalf("frame = %v, want id 1, event message", frame)
	}
	var got struct {
		Seq     int64  `json:"seq"`
		Level   string `json:"level"`
		Source  string `json:"source"`
		Message string `json:"message"`
		Attrs   []Attr `json:"attrs"`
	}
	if err := json.Unmarshal([]byte(frame["data"]), &got); err != nil {
		t.Fatalf("data %q: %v", frame["data"], err)
	}
	want := []Attr{{"api_key", Redacted}}
	if got.Seq != 1 || got.Level != "warn" || got.Source != SourceAPI || got.Message != "probe" || len(got.Attrs) != 1 || got.Attrs[0] != want[0] {
		t.Fatalf("data = %+v, want seq 1 warn api probe %v", got, want)
	}
}

// readFrame reads one SSE frame (up to the blank line) into field -> value.
func readFrame(t *testing.T, sc *bufio.Scanner) map[string]string {
	t.Helper()
	frame := map[string]string{}
	for range 16 {
		if !sc.Scan() {
			t.Fatalf("stream ended before a full frame: %v (read %v)", sc.Err(), frame)
		}
		line := sc.Text()
		if line == "" {
			return frame
		}
		k, v, _ := strings.Cut(line, ": ")
		frame[k] = v
	}
	t.Fatalf("frame longer than 16 lines: %v", frame)
	return nil
}
