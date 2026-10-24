// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

type mockSink struct {
	mu      sync.Mutex
	calls   []sinkCall
	failFor string
}

type sinkCall struct {
	Kind     watchaware.Kind
	SourceID string
	Watches  []watchaware.Watch
}

func (m *mockSink) StoreWatches(_ context.Context, kind watchaware.Kind, sourceID string, ws []watchaware.Watch) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failFor != "" && m.failFor == sourceID {
		return errors.New("simulated sink store error")
	}

	m.calls = append(m.calls, sinkCall{
		Kind:     kind,
		SourceID: sourceID,
		Watches:  ws,
	})
	return nil
}

type staticSource struct {
	kind    watchaware.Kind
	watches []watchaware.Watch
	err     error
}

func (s *staticSource) Kind() watchaware.Kind {
	return s.kind
}

func (s *staticSource) Watches(_ context.Context) ([]watchaware.Watch, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.watches, nil
}

func TestSyncAll_FirstSourceFailsSecondStored(t *testing.T) {
	t.Parallel()

	// Source 1: server created and immediately closed to guarantee connection failure
	unreachableServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	unreachableServer.Close()

	src1, err := watchaware.NewJellyfin(unreachableServer.URL, "token", 100*time.Millisecond, "SnatchArr-test")
	if err != nil {
		t.Fatalf("NewJellyfin src1: %v", err)
	}

	// Source 2: working static source
	src2 := &staticSource{
		kind: watchaware.KindEmby,
		watches: []watchaware.Watch{
			{
				UserID:        "user-2",
				UserName:      "Bob",
				ItemID:        "item-42",
				ItemType:      "Movie",
				Played:        true,
				PositionTicks: 123456,
			},
		},
	}

	sources := []watchaware.Named{
		{ID: "src-1", Source: src1},
		{ID: "src-2", Source: src2},
	}

	sink := &mockSink{}
	var logged []string
	logf := func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}

	ctx := context.Background()
	errs := watchaware.SyncAll(ctx, sources, sink, logf)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	expectedPrefix := "watchaware: source src-1:"
	if !strings.HasPrefix(errs[0].Error(), expectedPrefix) {
		t.Errorf("error = %q, want prefix %q", errs[0].Error(), expectedPrefix)
	}

	if len(logged) != 1 || !strings.HasPrefix(logged[0], expectedPrefix) {
		t.Errorf("logged = %v, want 1 entry with prefix %q", logged, expectedPrefix)
	}

	// Verify that source 2 was still stored in the sink
	if len(sink.calls) != 1 {
		t.Fatalf("sink.calls = %d, want 1", len(sink.calls))
	}

	call := sink.calls[0]
	if call.SourceID != "src-2" || call.Kind != watchaware.KindEmby {
		t.Errorf("call = %+v, want SourceID: src-2, Kind: emby", call)
	}
	if len(call.Watches) != 1 || call.Watches[0].ItemID != "item-42" {
		t.Errorf("call.Watches = %+v, want 1 watch with ItemID item-42", call.Watches)
	}
}

func TestSyncAll_NilSink(t *testing.T) {
	t.Parallel()

	errs := watchaware.SyncAll(context.Background(), nil, nil, nil)
	if len(errs) != 1 || !errors.Is(errs[0], watchaware.ErrNilSink) {
		t.Fatalf("SyncAll with nil sink = %v, want ErrNilSink", errs)
	}
}

func TestSyncAll_MaxSourcesBound(t *testing.T) {
	t.Parallel()

	sources := make([]watchaware.Named, 40)
	for i := range sources {
		sources[i] = watchaware.Named{
			ID: fmt.Sprintf("src-%d", i),
			Source: &staticSource{
				kind: watchaware.KindJellyfin,
				watches: []watchaware.Watch{
					{ItemID: fmt.Sprintf("item-%d", i)},
				},
			},
		}
	}

	sink := &mockSink{}
	errs := watchaware.SyncAll(context.Background(), sources, sink, nil)
	if len(errs) != 0 {
		t.Fatalf("SyncAll errs = %v", errs)
	}

	const maxSources = 32
	if len(sink.calls) != maxSources {
		t.Errorf("sink.calls = %d, want %d (bounded by maxSources)", len(sink.calls), maxSources)
	}
}

func TestSourceError_AsAndUnwrap(t *testing.T) {
	t.Parallel()

	innerErr := errors.New("underlying network timeout")
	srcErr := &watchaware.SourceError{
		ID:  "source-abc-123",
		Err: innerErr,
	}

	wantText := "watchaware: source source-abc-123: underlying network timeout"
	if gotText := srcErr.Error(); gotText != wantText {
		t.Errorf("Error() = %q, want %q", gotText, wantText)
	}

	wrapped := fmt.Errorf("wrap: %w", srcErr)
	var target *watchaware.SourceError
	if !errors.As(wrapped, &target) {
		t.Fatalf("errors.As failed to match *SourceError")
	}
	if target.ID != "source-abc-123" {
		t.Errorf("target.ID = %q, want source-abc-123", target.ID)
	}

	if !errors.Is(wrapped, innerErr) {
		t.Errorf("errors.Is(wrapped, innerErr) = false, want true")
	}
	if !errors.Is(target.Unwrap(), innerErr) {
		t.Errorf("target.Unwrap() = %v, want %v", target.Unwrap(), innerErr)
	}
}
