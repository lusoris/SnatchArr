// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestDecodeOASBody(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		want    oas.SnatchKind
		wantErr bool
	}{
		{name: "valid body", raw: `{"kind":"missing"}`, want: oas.SnatchKindMissing},
		{name: "empty body", raw: ``, wantErr: true},
		{name: "malformed json", raw: `{"kind":`, wantErr: true},
		{name: "enum outside the schema", raw: `{"kind":"everything"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeOASBody[oas.RunTrigger](json.RawMessage(tc.raw))
			if tc.wantErr {
				if !errors.Is(err, domain.ErrBadRequest) {
					t.Fatalf("err = %v, want ErrBadRequest", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.Kind != tc.want {
				t.Fatalf("kind = %q, want %q", got.Kind, tc.want)
			}
		})
	}
}

// A body type without the generated methods fails closed and is not reported
// as a client error.
func TestDecodeOASBodyRejectsPlainType(t *testing.T) {
	t.Parallel()
	_, err := decodeOASBody[struct{ Kind string }](json.RawMessage(`{"Kind":"missing"}`))
	if err == nil || errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("err = %v, want an internal error", err)
	}
}
