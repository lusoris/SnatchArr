// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/oauth2"

	"github.com/golusoris/golusoris/auth/oidc"
)

func TestOIDCRefusalDetails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		err        error
		wantReason string
		wantDetail string
	}{
		{
			name:       "boundary nil error",
			err:        nil,
			wantReason: "",
			wantDetail: "",
		},
		{
			name:       "positive flow state error",
			err:        oidc.ErrFlowState,
			wantReason: "invalid_state",
			wantDetail: "",
		},
		{
			name:       "positive flow expired error",
			err:        oidc.ErrFlowExpired,
			wantReason: "flow_expired",
			wantDetail: "",
		},
		{
			name:       "positive flow idp error",
			err:        oidc.ErrFlowIDP,
			wantReason: "idp_error",
			wantDetail: "",
		},
		{
			name:       "positive flow code error",
			err:        oidc.ErrFlowCode,
			wantReason: "missing_code",
			wantDetail: "",
		},
		{
			name:       "positive nonce mismatch error",
			err:        oidc.ErrNonceMismatch,
			wantReason: "invalid_nonce",
			wantDetail: "",
		},
		{
			name:       "positive exchange error without retrieve error",
			err:        oidc.ErrExchange,
			wantReason: "exchange_failed",
			wantDetail: "",
		},
		{
			name:       "boundary exchange error with retrieve error",
			err:        fmt.Errorf("%w: %w", oidc.ErrExchange, &oauth2.RetrieveError{ErrorCode: "invalid_grant"}),
			wantReason: "exchange_failed",
			wantDetail: "invalid_grant",
		},
		{
			name:       "positive id token missing error",
			err:        oidc.ErrIDTokenMissing,
			wantReason: "missing_id_token",
			wantDetail: "",
		},
		{
			name:       "boundary id token invalid error with cause",
			err:        fmt.Errorf("%w: %w", oidc.ErrIDTokenInvalid, errors.New("bad signature")),
			wantReason: "invalid_id_token",
			wantDetail: "bad signature",
		},
		{
			name:       "boundary discovery error",
			err:        errors.New("auth/oidc: discover provider: connection refused"),
			wantReason: "discovery_failed",
			wantDetail: "",
		},
		{
			name:       "negative unrecognized error fallback",
			err:        errors.New("database connection lost"),
			wantReason: "database connection lost",
			wantDetail: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotReason, gotDetail := oidcRefusalDetails(tc.err)
			if gotReason != tc.wantReason {
				t.Errorf("reason = %q, want %q", gotReason, tc.wantReason)
			}
			if gotDetail != tc.wantDetail {
				t.Errorf("detail = %q, want %q", gotDetail, tc.wantDetail)
			}
		})
	}
}

func wrapN(err error, n int) error {
	for i := range n {
		err = fmt.Errorf("layer %d: %w", i, err)
	}
	return err
}

func TestIDTokenErrorDetail(t *testing.T) {
	t.Parallel()
	cause := errors.New("token expired")
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "positive single chain within bound", err: wrapN(cause, 3), want: "token expired"},
		{name: "positive joined error follows last branch", err: errors.Join(errors.New("first"), wrapN(cause, 2)), want: "token expired"},
		{name: "boundary chain at bound", err: wrapN(cause, maxUnwrapDepth), want: "token expired"},
		{name: "negative chain beyond bound stops at bound", err: wrapN(cause, maxUnwrapDepth+1), want: "layer 0: token expired"},
		{name: "boundary unwrapped error", err: cause, want: "token expired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := idTokenErrorDetail(tc.err); got != tc.want {
				t.Errorf("detail = %q, want %q", got, tc.want)
			}
		})
	}
}
