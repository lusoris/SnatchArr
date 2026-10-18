// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"fmt"
	"io"
	"time"
)

const day = 24 * time.Hour

type input struct {
	day       time.Time
	slack     time.Duration
	snatch    snatchLog
	unblocker unblockerLog
}

// mismatch is one download only one side acted on.
type mismatch struct {
	side   string
	ev     event
	reason string
}

// row is one day's parity. Would and Unblocker count the downloads of the day on each side.
// Matched downloads lie on both. Skipped would-imports were refused by the unblocker's
// candidate check, which the observe mode does not run. Unseen downloads the unblocker
// imported before any SnatchArr sweep saw them.
type row struct {
	day        time.Time
	shadowLive bool
	would      int
	unblocker  int
	matched    int
	skipped    int
	unseen     int
	mismatches []mismatch
}

// red is a mismatch on either side, or more unseen downloads than matched ones: a sweep
// every 10 minutes sees most items before the hourly unblocker run, and a silent shadow
// leaves every unblocker import unseen. shadowLive only explains such a row.
func (r row) red() bool {
	return len(r.mismatches) > 0 || r.unseen > r.matched
}

func (r row) write(w io.Writer) error {
	verdict := "green"
	if r.red() {
		verdict = "red"
	}
	if _, err := fmt.Fprintf(w, "day=%s would=%d unblocker=%d matched=%d skipped=%d unseen=%d mismatches=%d shadow_live=%t verdict=%s\n",
		r.day.Format(time.DateOnly), r.would, r.unblocker, r.matched, r.skipped, r.unseen, len(r.mismatches), r.shadowLive, verdict); err != nil {
		return fmt.Errorf("write row: %w", err)
	}
	for _, m := range r.mismatches {
		if _, err := fmt.Fprintf(w, "mismatch side=%s download_id=%s at=%s reason=%q title=%q\n",
			m.side, m.ev.ID, m.ev.At.UTC().Format(time.RFC3339), m.reason, m.ev.Title); err != nil {
			return fmt.Errorf("write mismatch: %w", err)
		}
	}
	return nil
}

func compare(in input) row {
	r := row{day: in.day, shadowLive: anyInDay(in, in.snatch.would) || anyInDay(in, in.snatch.strikes)}
	for _, w := range firstPerID(in, in.snatch.would) {
		r.would++
		switch {
		case near(in.unblocker.imports, w.At, in.slack, sameID(w)):
			r.matched++
		case near(in.unblocker.skips, w.At, in.slack, sameTitle(w)):
			r.skipped++
		default:
			r.mismatches = append(r.mismatches, mismatch{side: "snatcharr", ev: w, reason: "the unblocker did not import it"})
		}
	}
	for _, u := range firstPerID(in, in.unblocker.imports) {
		r.unblocker++
		classifyUnblocker(in, u, &r)
	}
	return r
}

// classifyUnblocker files an unblocker import that has no would-import near it: struck by
// SnatchArr in the day before is a mismatch, never struck is unseen.
func classifyUnblocker(in input, u event, r *row) {
	if near(in.snatch.would, u.At, in.slack, sameID(u)) {
		return
	}
	for _, s := range in.snatch.strikes {
		if s.ID != u.ID || s.At.Before(u.At.Add(-day)) || s.At.After(u.At.Add(in.slack)) {
			continue
		}
		reason := "SnatchArr struck it under rule " + s.Rule
		if s.Rule == "id_matched" {
			reason = "SnatchArr struck it under rule id_matched without a would-import"
		}
		r.mismatches = append(r.mismatches, mismatch{side: "unblocker", ev: u, reason: reason})
		return
	}
	r.unseen++
}

func inDay(in input, t time.Time) bool {
	return !t.Before(in.day) && t.Before(in.day.Add(day))
}

func anyInDay(in input, evs []event) bool {
	for _, e := range evs {
		if inDay(in, e.At) {
			return true
		}
	}
	return false
}

// firstPerID keeps the first event of each download inside the day: a strike row that
// expires re-logs its would-import, and a dry-run unblocker logs WOULD import every hour.
func firstPerID(in input, evs []event) []event {
	seen := make(map[string]bool, len(evs))
	var out []event
	for _, e := range evs {
		if !inDay(in, e.At) || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	return out
}

func near(evs []event, at time.Time, slack time.Duration, match func(event) bool) bool {
	for _, e := range evs {
		if match(e) && e.At.Sub(at).Abs() <= slack {
			return true
		}
	}
	return false
}

func sameID(a event) func(event) bool {
	return func(b event) bool { return a.ID != "" && a.ID == b.ID }
}

func sameTitle(a event) func(event) bool {
	return func(b event) bool { return a.Title != "" && a.Title == b.Title }
}
