// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	// maxLines bounds one log; a day of either log is far below it.
	maxLines = 2_000_000
	// maxLineBytes bounds one log line.
	maxLineBytes = 1 << 20

	msgWouldImport = "queue: would force import"
	msgStruck      = "queue: item struck"
)

// event is one log line either side wrote about one download. Rule is set on SnatchArr strikes.
type event struct {
	ID    string
	Title string
	Rule  string
	At    time.Time
}

// snatchLog holds SnatchArr's shadow lines: would-imports, and every rule's first strike.
type snatchLog struct {
	would   []event
	strikes []event
}

// unblockerLog holds the unblocker's IMPORT (or dry-run WOULD import) and SKIP lines. A SKIP
// line names the release title only.
type unblockerLog struct {
	imports []event
	skips   []event
}

var errTooLong = errors.New("log exceeds the line bound")

var (
	unblockerImport = regexp.MustCompile(`\[[^\]]+\] (?:IMPORT|WOULD import) '(.*)' downloadId=(\S+) files=`)
	unblockerSkip   = regexp.MustCompile(`\[[^\]]+\] SKIP '(.*)': `)
)

func eachLine(r io.Reader, fn func(n int, line []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for n := 1; sc.Scan(); n++ {
		if n > maxLines {
			return errTooLong
		}
		if err := fn(n, sc.Bytes()); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read log: %w", err)
	}
	return nil
}

// parseSnatchArr reads the api's JSON lines; a kubectl timestamp before the object is
// ignored, and lines without an object are startup noise.
func parseSnatchArr(r io.Reader) (snatchLog, error) {
	var out snatchLog
	err := eachLine(r, func(n int, line []byte) error {
		i := bytes.IndexByte(line, '{')
		if i < 0 {
			return nil
		}
		var rec struct {
			Time       time.Time `json:"time"`
			Msg        string    `json:"msg"`
			Rule       string    `json:"rule"`
			DownloadID string    `json:"download_id"`
			Title      string    `json:"title"`
		}
		if err := json.Unmarshal(line[i:], &rec); err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
		ev := event{ID: strings.ToLower(rec.DownloadID), Title: rec.Title, Rule: rec.Rule, At: rec.Time}
		switch rec.Msg {
		case msgWouldImport:
			out.would = append(out.would, ev)
		case msgStruck:
			out.strikes = append(out.strikes, ev)
		}
		return nil
	})
	return out, err
}

// parseUnblocker reads `kubectl logs --timestamps` lines. An IMPORT or SKIP line without a
// leading RFC 3339 timestamp cannot be placed in a day and fails the read.
func parseUnblocker(r io.Reader) (unblockerLog, error) {
	var out unblockerLog
	err := eachLine(r, func(n int, line []byte) error {
		s := string(line)
		imp, skip := unblockerImport.FindStringSubmatch(s), unblockerSkip.FindStringSubmatch(s)
		if imp == nil && skip == nil {
			return nil
		}
		stamp, _, _ := strings.Cut(s, " ")
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return fmt.Errorf("line %d: no timestamp: %w", n, err)
		}
		if imp != nil {
			out.imports = append(out.imports, event{ID: strings.ToLower(imp[2]), Title: imp[1], At: at})
			return nil
		}
		out.skips = append(out.skips, event{Title: skip[1], At: at})
		return nil
	})
	return out, err
}
