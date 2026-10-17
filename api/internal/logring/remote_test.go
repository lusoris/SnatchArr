// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAddRemoteRedactsPlantedSecrets(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	r.AddRemote(t.Context(), SourceWorker("w1"), RemoteRecord{
		Time: time.Unix(1, 0), Level: slog.LevelWarn,
		Message: "GET http://sonarr:8989/api/v3/wanted/missing?apikey=" + plantedHex + "&page=3",
		Attrs: []Attr{
			{Key: "api_key", Value: plantedHex},
			{Key: "headers", Value: "Authorization: Bearer " + plantedBearer},
			{Key: "error", Value: "dial: token=" + plantedToken + " refused"},
			{Key: "instance", Value: "sonarr-main"},
		},
	})
	recs, _ := r.List(Query{Limit: 1})
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	got := recs[0]
	if got.Source != "worker:w1" || got.Level != slog.LevelWarn || !got.Time.Equal(time.Unix(1, 0)) {
		t.Errorf("record = %+v, want source worker:w1, level warn, time 1s", got)
	}
	var sb strings.Builder
	sb.WriteString(got.Message)
	for _, a := range got.Attrs {
		sb.WriteString("\n" + a.Key + "=" + a.Value)
	}
	stored := sb.String()
	for _, secret := range []string{plantedHex, plantedBearer, plantedToken} {
		if strings.Contains(stored, secret) {
			t.Errorf("stored worker record leaks %q:\n%s", secret, stored)
		}
	}
	for _, kept := range []string{"page=3", "sonarr:8989", "instance=sonarr-main", "refused"} {
		if !strings.Contains(stored, kept) {
			t.Errorf("redaction removed %q, which is no secret:\n%s", kept, stored)
		}
	}
}

func TestAddRemoteBounds(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	attrs := make([]Attr, 0, maxAttrs+10)
	attrs = append(attrs, Attr{Key: "", Value: "keyless"}, Attr{Key: strings.Repeat("k", maxKeyBytes+5), Value: "v"})
	for i := range maxAttrs + 8 {
		attrs = append(attrs, Attr{Key: "f" + strconv.Itoa(i), Value: "v"})
	}
	r.AddRemote(t.Context(), SourceWorker(strings.Repeat("w", maxSourceIDBytes+9)), RemoteRecord{
		Message: strings.Repeat("m", maxMessageBytes+100), Attrs: attrs,
	})
	recs, _ := r.List(Query{Limit: 1, MinLevel: slog.LevelDebug})
	got := recs[0]
	if want := sourceWorkerPrefix + strings.Repeat("w", maxSourceIDBytes); got.Source != want {
		t.Errorf("source length = %d, want %d", len(got.Source), len(want))
	}
	// The keyless attribute is skipped and counts against the cap, which applies first.
	if len(got.Attrs) != maxAttrs-1 {
		t.Errorf("attrs = %d, want %d", len(got.Attrs), maxAttrs-1)
	}
	if len(got.Attrs[0].Key) != maxKeyBytes {
		t.Errorf("key length = %d, want %d", len(got.Attrs[0].Key), maxKeyBytes)
	}
	if len(got.Message) > maxMessageBytes+len("…") {
		t.Errorf("message length = %d, want at most %d", len(got.Message), maxMessageBytes+len("…"))
	}
}

func TestSourceFilterSeparatesAPIAndWorkers(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	slog.New(r.Wrap(debugHandler(), SourceAPI)).Info("api record")
	r.AddRemote(t.Context(), SourceWorker("w1"), RemoteRecord{Level: slog.LevelInfo, Message: "w1 record"})
	r.AddRemote(t.Context(), SourceWorker("w2"), RemoteRecord{Level: slog.LevelInfo, Message: "w2 record"})
	// A source that only starts with "worker" is no worker record.
	r.AddRemote(t.Context(), "workers-elsewhere", RemoteRecord{Level: slog.LevelInfo, Message: "other record"})
	cases := []struct {
		source string
		want   []string
	}{
		{"", []string{"other record", "w2 record", "w1 record", "api record"}},
		{SourceAPI, []string{"api record"}},
		{SourceWorkers, []string{"w2 record", "w1 record"}},
		{SourceWorker("w1"), []string{"w1 record"}},
		{SourceWorker("w3"), nil},
		{"worker:", nil},
	}
	for _, c := range cases {
		recs, _ := r.List(Query{Source: c.source, Limit: 10})
		got := make([]string, 0, len(recs))
		for _, rec := range recs {
			got = append(got, rec.Message)
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("source %q: got %q, want %q", c.source, got, c.want)
		}
	}
}
