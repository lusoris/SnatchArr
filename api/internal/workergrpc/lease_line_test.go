// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import "testing"

// woken reports whether the call holds a wake token, taking it.
func woken(turn *leaseTurn) bool {
	select {
	case <-turn.wake:
		return true
	default:
		return false
	}
}

// The first call is the head; leaving hands the head, and a token, to the next in order.
func TestLeaseLineOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func(l *leaseLine) (head bool, token bool)
	}{
		{"the first call is the head", func(l *leaseLine) (bool, bool) {
			a := l.join()
			return l.isHead(a), woken(a)
		}},
		{"a later call is not, and gets no token", func(l *leaseLine) (bool, bool) {
			l.join()
			b := l.join()
			return l.isHead(b), woken(b)
		}},
		{"the head leaving wakes the next", func(l *leaseLine) (bool, bool) {
			a, b := l.join(), l.join()
			l.leave(a)
			return l.isHead(b), woken(b)
		}},
		{"a call in the middle leaving keeps the order", func(l *leaseLine) (bool, bool) {
			a, b, c := l.join(), l.join(), l.join()
			l.leave(b)
			l.leave(a)
			return l.isHead(c), woken(c)
		}},
		{"leaving twice, or leaving as no call, changes nothing", func(l *leaseLine) (bool, bool) {
			a, b := l.join(), l.join()
			l.leave(a)
			l.leave(a)
			l.leave(nil)
			return l.isHead(b), woken(b)
		}},
		{"an empty line has no head", func(l *leaseLine) (bool, bool) {
			a := l.join()
			l.leave(a)
			return l.isHead(a), woken(a)
		}},
	}
	wantHead := []bool{true, false, true, true, true, false}
	wantToken := []bool{false, false, true, true, true, false}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			head, token := tc.run(&leaseLine{})
			if head != wantHead[i] || token != wantToken[i] {
				t.Errorf("head, token = %t, %t; want %t, %t", head, token, wantHead[i], wantToken[i])
			}
		})
	}
}
