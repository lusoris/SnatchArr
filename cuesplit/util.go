// SPDX-FileCopyrightText: 2019-2020 Sigrid Solveig Haflínudóttir
// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
)

var pathReplaceChars func(string) string

func init() {
	r := strings.NewReplacer(
		`/`, `∕`,
		`"`, `“`,
		`?`, `？`,
		`*`, `∗`,
		`\`, `⧵`,
		`|`, `∣`,
		`;`, `;`,
		`:`, `∶`,
		`<`, `‹`,
		`>`, `›`,
	)
	pathReplaceChars = r.Replace
}

// findAudioFile looks in dir for an audio file with the same base name (ignoring extension).
func findAudioFile(dir, name string) (string, bool) {
	name, _ = strings.CutSuffix(name, filepath.Ext(name))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	validExts := map[string]bool{
		".flac": true,
		".wav":  true,
		".ape":  true,
		".wv":   true,
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !validExts[ext] {
			continue
		}
		n, _ := strings.CutSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.EqualFold(n, name) {
			path := filepath.Join(dir, entry.Name())
			return path, true
		}
	}

	return "", false
}
