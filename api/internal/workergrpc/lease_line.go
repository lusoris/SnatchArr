// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"slices"
	"sync"
)

// leaseLine puts the LeaseRun calls that wait for a run in the order they arrived. Only the
// call at the head of the line asks the database for a run; when it has its answer (a run, an
// error or the end of its wait) the next call becomes the head and asks at once. A worker that
// polls again the moment it has been served therefore joins behind the workers already
// waiting, instead of beating their next poll to every run queued meanwhile (#343). The line
// is per control-plane replica; the zero value is an empty line.
type leaseLine struct {
	mu    sync.Mutex
	calls []*leaseTurn
}

// leaseTurn is one waiting call. wake carries at most one token: "you are the head, ask now".
// timer is the wait the call is in, if any; leave stops it with the token, so a woken call
// holds no timer from then on (a fake clock counts only the waits that can still end).
type leaseTurn struct {
	wake  chan struct{}
	timer stopper
}

// stopper is the part of a clock timer the line needs.
type stopper interface{ Stop() bool }

// join adds a call at the end of the line. A call that finds the line empty is the head
// and asks without waiting for a token.
func (l *leaseLine) join() *leaseTurn {
	turn := &leaseTurn{wake: make(chan struct{}, 1)}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, turn)
	return turn
}

// leave takes a call out of the line, if it is still in, and wakes the new head.
func (l *leaseLine) leave(turn *leaseTurn) {
	if turn == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.calls, turn)
	if i < 0 {
		return
	}
	l.calls = slices.Delete(l.calls, i, i+1)
	if len(l.calls) == 0 {
		return
	}
	head := l.calls[0]
	select {
	case head.wake <- struct{}{}:
	default:
	}
	if head.timer != nil {
		head.timer.Stop()
		head.timer = nil
	}
}

// arm records the timer of the wait the call starts. A call that already holds a token
// returns from the wait at once, so its timer is stopped here.
func (l *leaseLine) arm(turn *leaseTurn, timer stopper) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(turn.wake) > 0 {
		timer.Stop()
		return
	}
	turn.timer = timer
}

// disarm forgets the timer of the wait the call has ended.
func (l *leaseLine) disarm(turn *leaseTurn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	turn.timer = nil
}

// isHead reports whether the call is the first in the line.
func (l *leaseLine) isHead(turn *leaseTurn) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls) > 0 && l.calls[0] == turn
}
