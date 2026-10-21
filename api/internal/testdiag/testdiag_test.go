// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package testdiag

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

type fakeTB struct {
	testing.TB
	mu   sync.Mutex
	logs []string
}

func newFakeTB(tb testing.TB) *fakeTB {
	tb.Helper()
	return &fakeTB{TB: tb}
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Log(args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, fmt.Sprint(args...))
}

func (f *fakeTB) Logf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, fmt.Sprintf(format, args...))
}

func (f *fakeTB) allLogs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.logs, "\n")
}

type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "network timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }

var _ net.Error = fakeTimeoutError{}

type fakeNonTimeoutNetError struct{}

func (fakeNonTimeoutNetError) Error() string   { return "network error" }
func (fakeNonTimeoutNetError) Timeout() bool   { return false }
func (fakeNonTimeoutNetError) Temporary() bool { return false }

var _ net.Error = fakeNonTimeoutNetError{}

func TestWatchReport(t *testing.T) {
	t.Parallel()

	st := storetest.New(t)
	pool := st.Pool()

	tests := []struct {
		name        string
		pool        *pgxpool.Pool
		budget      time.Duration
		sleep       time.Duration
		err         error
		wantLogs    []string
		wantNoLogs  []string
		expectEmpty bool
	}{
		{
			name:        "positive guarded call past 80 percent with deadline exceeded",
			pool:        pool,
			budget:      50 * time.Millisecond,
			sleep:       70 * time.Millisecond,
			err:         context.DeadlineExceeded,
			wantLogs:    []string{"pg_stat_activity", "goroutine", "loadavg", "pool:"},
			expectEmpty: false,
		},
		{
			name:        "positive guarded call past 80 percent with net error timeout",
			pool:        pool,
			budget:      50 * time.Millisecond,
			sleep:       70 * time.Millisecond,
			err:         fakeTimeoutError{},
			wantLogs:    []string{"pg_stat_activity", "goroutine", "loadavg"},
			expectEmpty: false,
		},
		{
			name:        "negative nil error",
			pool:        pool,
			budget:      50 * time.Millisecond,
			sleep:       70 * time.Millisecond,
			err:         nil,
			expectEmpty: true,
		},
		{
			name:        "negative other error",
			pool:        pool,
			budget:      50 * time.Millisecond,
			sleep:       70 * time.Millisecond,
			err:         errors.New("connection reset"),
			expectEmpty: true,
		},
		{
			name:        "boundary stop before 80 percent",
			pool:        pool,
			budget:      200 * time.Millisecond,
			sleep:       0,
			err:         context.DeadlineExceeded,
			wantLogs:    []string{"no snapshot: call returned before 80 % of budget"},
			wantNoLogs:  []string{"pg_stat_activity", "goroutine profile"},
			expectEmpty: false,
		},
		{
			name:        "boundary pool nil snapshot without panic",
			pool:        nil,
			budget:      50 * time.Millisecond,
			sleep:       70 * time.Millisecond,
			err:         context.DeadlineExceeded,
			wantLogs:    []string{"goroutine", "loadavg"},
			wantNoLogs:  []string{"pool:", "pg_stat_activity"},
			expectEmpty: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeTB(t)
			w := Watch(tc.budget, tc.pool)
			if tc.sleep > 0 {
				time.Sleep(tc.sleep)
			}
			w.Stop()
			w.Report(fake, tc.err)

			logs := fake.allLogs()
			if tc.expectEmpty {
				if len(fake.logs) != 0 {
					t.Fatalf("expected no logs, got: %s", logs)
				}
				return
			}
			for _, want := range tc.wantLogs {
				if !strings.Contains(logs, want) {
					t.Errorf("expected logs to contain %q, but logs were:\n%s", want, logs)
				}
			}
			for _, unwanted := range tc.wantNoLogs {
				if strings.Contains(logs, unwanted) {
					t.Errorf("expected logs NOT to contain %q, but logs were:\n%s", unwanted, logs)
				}
			}
		})
	}
}

func TestFilterGoroutineProfile(t *testing.T) {
	t.Parallel()

	raw := `goroutine profile: total 3
1 @ 0x1 0x2
#	0x1	github.com/lusoris/SnatchArr/api/internal/testdiag.Watch+0x10
#	0x2	main.main+0x20

2 @ 0x3 0x4
#	0x3	github.com/jackc/pgx/v5/pgxpool.(*Pool).Acquire+0x10
#	0x4	runtime.goexit+0x0

3 @ 0x5 0x6
#	0x5	golang.org/x/crypto/argon2.Key+0x10
#	0x6	runtime.goexit+0x0

4 @ 0x7 0x8
#	0x7	net/http.(*Server).Serve+0x10
#	0x8	runtime.goexit+0x0
`
	filtered := filterGoroutineProfile(raw)
	if !strings.Contains(filtered, "SnatchArr") {
		t.Errorf("expected filtered profile to contain SnatchArr, got:\n%s", filtered)
	}
	if !strings.Contains(filtered, "pgx") {
		t.Errorf("expected filtered profile to contain pgx, got:\n%s", filtered)
	}
	if !strings.Contains(filtered, "argon2") {
		t.Errorf("expected filtered profile to contain argon2, got:\n%s", filtered)
	}
	if strings.Contains(filtered, "net/http.(*Server).Serve") {
		t.Errorf("expected filtered profile to exclude net/http server stack, got:\n%s", filtered)
	}
}

func TestFilterGoroutineCap(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := range 200 {
		fmt.Fprintf(&sb, "%d @ 0x1\n#\t0x1\tgithub.com/lusoris/SnatchArr/pkg.Func_%d\n\n", i, i)
	}
	filtered := filterGoroutineProfile(sb.String())
	if len(filtered) > maxGoroutineCap {
		t.Fatalf("filtered length %d exceeds max cap %d", len(filtered), maxGoroutineCap)
	}
}

func TestIsTimeoutError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, true},
		{"fakeTimeoutError", fakeTimeoutError{}, true},
		{"fakeNonTimeoutNetError", fakeNonTimeoutNetError{}, false},
		{"generic error", errors.New("something went wrong"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isTimeoutError(tc.err)
			if got != tc.want {
				t.Errorf("isTimeoutError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
