// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package findings reads the findings from the JSON report of a Go analysis tool. gosec and
// golangci-lint both list them under the top-level field "Issues".
package findings

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Decode returns the entries of the report's "Issues" field as values of T.
// A report without that field is refused: it is not a report of zero findings.
func Decode[T any](report []byte) ([]T, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(report, &fields); err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	raw, ok := fields["Issues"]
	if !ok {
		return nil, errors.New(`report has no "Issues" field`)
	}
	var issues []T
	if err := json.Unmarshal(raw, &issues); err != nil {
		return nil, fmt.Errorf("report field \"Issues\": %w", err)
	}
	return issues, nil
}
