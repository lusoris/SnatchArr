// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"bytes"
	"strings"
)

// DocsEnvs returns the variable names that docs/configuration.md documents: the backticked
// first cell of every table row.
func DocsEnvs(md []byte) map[string]struct{} {
	envs := make(map[string]struct{})
	for line := range strings.SplitSeq(string(md), "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) < 3 || parts[0] != "" {
			continue
		}
		cell := strings.TrimSpace(parts[1])
		if len(cell) > 2 && strings.HasPrefix(cell, "`") && strings.HasSuffix(cell, "`") {
			envs[strings.Trim(cell, "`")] = struct{}{}
		}
	}
	return envs
}

// ParityTSV renders the parity table committed as docs/settings-parity.tsv: one row per
// setting with its class, apply mode and whether docs/configuration.md documents it.
func ParityTSV(reg []Setting, docs map[string]struct{}) []byte {
	var buf bytes.Buffer
	buf.WriteString("env\tkey\tclass\tapply\tdocs\n")
	for _, s := range reg {
		documented := "no"
		if _, ok := docs[s.Env]; ok {
			documented = "yes"
		}
		buf.WriteString(strings.Join([]string{s.Env, s.Key, string(s.Class), string(s.Apply), documented}, "\t") + "\n")
	}
	return buf.Bytes()
}
