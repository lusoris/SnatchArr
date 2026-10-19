// SPDX-FileCopyrightText: 2026 lusoris
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"

	"cauda.dev/cuesplit/cuesheet"
)

type RefusalError struct {
	Rule string
	Msg  string
}

func (e *RefusalError) Error() string {
	if e.Rule != "" {
		return fmt.Sprintf("%s: %s", e.Rule, e.Msg)
	}
	return e.Msg
}

func Refusal(rule, format string, args ...any) error {
	return &RefusalError{
		Rule: rule,
		Msg:  fmt.Sprintf(format, args...),
	}
}

func IsRefusal(err error) bool {
	var refErr *RefusalError
	if errors.As(err, &refErr) {
		return true
	}
	var cueErr *cuesheet.Error
	return errors.As(err, &cueErr)
}
