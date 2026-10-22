// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package testdiag provides test-only diagnostics when integration test calls time out.
package testdiag

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jonboulle/clockwork"
)

// Watcher arms a snapshot at 80 % of budget to capture diagnostics if a guarded call blocks.
type Watcher struct {
	budget       time.Duration
	pool         *pgxpool.Pool
	clk          clockwork.Clock
	start        time.Time
	timer        clockwork.Timer
	snapshotDone chan struct{}
	doneOnce     sync.Once
	mu           sync.Mutex
	snapshotText string
	taken        bool
	stopped      bool
}

// Watch arms one snapshot at 80 % of budget. Call Stop when the guarded call returns,
// then Report with its error.
func Watch(budget time.Duration, pool *pgxpool.Pool) *Watcher {
	return watchWithClock(budget, pool, clockwork.NewRealClock())
}

func watchWithClock(budget time.Duration, pool *pgxpool.Pool, clk clockwork.Clock) *Watcher {
	w := &Watcher{
		budget:       budget,
		pool:         pool,
		clk:          clk,
		start:        clk.Now(),
		snapshotDone: make(chan struct{}),
	}
	fireAfter := budget * 4 / 5
	w.timer = clk.AfterFunc(fireAfter, w.takeSnapshot)
	return w
}

// Stop cancels the armed snapshot timer if it has not fired yet.
func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	timer := w.timer
	w.mu.Unlock()

	if timer != nil && timer.Stop() {
		w.finishSnapshot("", false)
	}
}

// Report logs the snapshot if err is context.DeadlineExceeded or a network timeout.
func (w *Watcher) Report(tb testing.TB, err error) {
	tb.Helper()
	if w == nil || !isTimeoutError(err) {
		return
	}
	<-w.snapshotDone
	w.mu.Lock()
	taken := w.taken
	text := w.snapshotText
	w.mu.Unlock()

	if !taken {
		tb.Log("no snapshot: call returned before 80 % of budget")
		return
	}
	tb.Log(text)
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (w *Watcher) finishSnapshot(text string, taken bool) {
	w.mu.Lock()
	if taken {
		w.snapshotText = text
		w.taken = true
	}
	w.mu.Unlock()
	w.doneOnce.Do(func() {
		close(w.snapshotDone)
	})
}

func (w *Watcher) takeSnapshot() {
	w.mu.Lock()
	stopped := w.stopped
	w.mu.Unlock()
	if stopped {
		w.finishSnapshot("", false)
		return
	}

	text := w.collect()
	w.finishSnapshot(text, true)
}

func (w *Watcher) collect() string {
	offset := w.clk.Since(w.start)
	var sb strings.Builder

	fmt.Fprintf(&sb, "=== testdiag snapshot (offset: %s) ===\n", offset)
	sb.WriteString(readLoadAvg())
	sb.WriteByte('\n')
	sb.WriteString(formatRuntime())

	if w.pool != nil {
		sb.WriteString(formatPoolStat(w.pool))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sb.WriteString(queryPgStatActivity(ctx, w.pool))
	}

	sb.WriteString(collectGoroutineProfile())
	return sb.String()
}

func readLoadAvg() string {
	data, err := os.ReadFile("/proc/loadavg") // #nosec G304 -- fixed proc path for diagnostics
	if err != nil {
		return fmt.Sprintf("loadavg: unavailable (%s)", err.Error())
	}
	return "loadavg: " + strings.TrimSpace(string(data))
}

func formatRuntime() string {
	return fmt.Sprintf("runtime: goroutines=%d cpus=%d gomaxprocs=%d\n",
		runtime.NumGoroutine(), runtime.NumCPU(), runtime.GOMAXPROCS(0))
}

func formatPoolStat(pool *pgxpool.Pool) string {
	if pool == nil {
		return ""
	}
	s := pool.Stat()
	return fmt.Sprintf("pool: total=%d acquired=%d idle=%d max=%d empty_acquires=%d acquire_duration=%s\n",
		s.TotalConns(), s.AcquiredConns(), s.IdleConns(), s.MaxConns(), s.EmptyAcquireCount(), s.AcquireDuration())
}

func queryPgStatActivity(ctx context.Context, pool *pgxpool.Pool) string {
	if pool == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("=== pg_stat_activity ===\n")

	blockedLocks, err := queryUngrantedLocks(ctx, pool)
	if err != nil {
		fmt.Fprintf(&sb, "pg_locks ungranted error: %v\n", err)
	} else {
		fmt.Fprintf(&sb, "pg_locks ungranted: %d\n", blockedLocks)
	}

	lines, err := queryClientBackends(ctx, pool)
	if err != nil {
		fmt.Fprintf(&sb, "pg_stat_activity query error: %v\n", err)
		return sb.String()
	}

	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func queryUngrantedLocks(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_locks WHERE NOT granted").Scan(&count); err != nil {
		return 0, fmt.Errorf("query ungranted locks: %w", err)
	}
	return count, nil
}

const maxActivityRows = 200

const activitySQL = `SELECT pid, datname, state, wait_event_type, wait_event, extract(epoch from now()-query_start), left(query,120) FROM pg_stat_activity WHERE backend_type='client backend' ORDER BY query_start`

func queryClientBackends(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, activitySQL)
	if err != nil {
		return nil, fmt.Errorf("query pg_stat_activity: %w", err)
	}
	defer rows.Close()

	var lines []string
	for count := 0; count < maxActivityRows && rows.Next(); count++ {
		var (
			pid           int32
			datname       *string
			state         *string
			waitEventType *string
			waitEvent     *string
			queryAgeSec   *float64
			queryText     *string
		)
		if err := rows.Scan(&pid, &datname, &state, &waitEventType, &waitEvent, &queryAgeSec, &queryText); err != nil {
			return lines, fmt.Errorf("scan pg_stat_activity row: %w", err)
		}
		lines = append(lines, formatActivityRow(pid, datname, state, waitEventType, waitEvent, queryAgeSec, queryText))
	}
	if err := rows.Err(); err != nil {
		return lines, fmt.Errorf("iterate pg_stat_activity: %w", err)
	}
	return lines, nil
}

func formatActivityRow(
	pid int32,
	datname, state, waitType, waitEvent *string,
	ageSec *float64,
	query *string,
) string {
	d := "<nil>"
	if datname != nil {
		d = *datname
	}
	st := "<nil>"
	if state != nil {
		st = *state
	}
	wt := "<nil>"
	if waitType != nil {
		wt = *waitType
	}
	we := "<nil>"
	if waitEvent != nil {
		we = *waitEvent
	}
	age := "<nil>"
	if ageSec != nil {
		age = fmt.Sprintf("%.2fs", *ageSec)
	}
	q := "<nil>"
	if query != nil {
		q = *query
	}
	return fmt.Sprintf("pid=%d db=%s state=%s wait=%s/%s age=%s query=%s", pid, d, st, wt, we, age, q)
}

const maxGoroutineCap = 16 * 1024

func collectGoroutineProfile() string {
	prof := pprof.Lookup("goroutine")
	if prof == nil {
		return "goroutine profile: unavailable\n"
	}
	var rawBuf bytes.Buffer
	if err := prof.WriteTo(&rawBuf, 1); err != nil {
		return fmt.Sprintf("goroutine profile error: %v\n", err)
	}
	return filterGoroutineProfile(rawBuf.String())
}

func filterGoroutineProfile(raw string) string {
	blocks := strings.Split(raw, "\n\n")
	var sb strings.Builder
	sb.WriteString("goroutine profile (filtered):\n")

	const maxBlocks = 1000
	limit := min(len(blocks), maxBlocks)

	for _, rawBlock := range blocks[:limit] {
		b := strings.TrimSpace(rawBlock)
		if b == "" {
			continue
		}
		if containsAnyFrame(b, "SnatchArr", "pgx", "argon2") {
			if sb.Len()+len(b)+2 > maxGoroutineCap {
				remaining := maxGoroutineCap - sb.Len()
				if remaining > 0 {
					sb.WriteString(b[:remaining])
				}
				break
			}
			sb.WriteString(b)
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

func containsAnyFrame(block string, targets ...string) bool {
	for _, target := range targets {
		if strings.Contains(block, target) {
			return true
		}
	}
	return false
}
