// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testToken = "test-worker-secret-token"

type authCase struct {
	name       string
	configured string
	ctx        func(context.Context) context.Context
	wantCode   codes.Code
	wantRan    bool
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeServerStream) Context() context.Context {
	return s.ctx
}

func positiveAuthCases() []authCase {
	return []authCase{
		{
			name:       "metadata authorization Bearer token",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+testToken))
			},
			wantCode: codes.OK,
			wantRan:  true,
		},
		{
			name:       "second of two authorization values is right",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer wrong", "authorization", "Bearer "+testToken))
			},
			wantCode: codes.OK,
			wantRan:  true,
		},
		{
			name:       "empty configured token (dev)",
			configured: "",
			ctx: func(ctx context.Context) context.Context {
				return ctx
			},
			wantCode: codes.OK,
			wantRan:  true,
		},
	}
}

func negativeAuthCases() []authCase {
	return []authCase{
		{
			name:       "no metadata at all (plain ctx)",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return ctx
			},
			wantCode: codes.Unauthenticated,
			wantRan:  false,
		},
		{
			name:       "metadata without authorization key",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return metadata.NewIncomingContext(ctx, metadata.Pairs("other-key", "value"))
			},
			wantCode: codes.Unauthenticated,
			wantRan:  false,
		},
		{
			name:       "wrong token",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer wrong-token"))
			},
			wantCode: codes.Unauthenticated,
			wantRan:  false,
		},
	}
}

func boundaryAuthCases() []authCase {
	return []authCase{
		{
			name:       "token with one char missing",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+testToken[:len(testToken)-1]))
			},
			wantCode: codes.Unauthenticated,
			wantRan:  false,
		},
		{
			name:       "token with one extra char",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+testToken+"x"))
			},
			wantCode: codes.Unauthenticated,
			wantRan:  false,
		},
		{
			name:       "Bearer prefix only",
			configured: testToken,
			ctx: func(ctx context.Context) context.Context {
				return metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "))
			},
			wantCode: codes.Unauthenticated,
			wantRan:  false,
		},
	}
}

func tokenAuthCases() []authCase {
	cases := make([]authCase, 0, 9)
	cases = append(cases, positiveAuthCases()...)
	cases = append(cases, negativeAuthCases()...)
	cases = append(cases, boundaryAuthCases()...)
	return cases
}

func verifyUnary(t *testing.T, tc authCase) {
	t.Helper()
	auth := NewTokenAuth(tc.configured, slog.New(slog.DiscardHandler))
	ctx := tc.ctx(t.Context())
	ran := false
	handler := func(_ context.Context, _ any) (any, error) {
		ran = true
		return "ok", nil
	}
	_, err := auth.Unary(ctx, "req", nil, handler)
	if code := status.Code(err); code != tc.wantCode {
		t.Errorf("Unary code = %v, want %v", code, tc.wantCode)
	}
	if ran != tc.wantRan {
		t.Errorf("Unary handler ran = %v, want %v", ran, tc.wantRan)
	}
}

func verifyStream(t *testing.T, tc authCase) {
	t.Helper()
	auth := NewTokenAuth(tc.configured, slog.New(slog.DiscardHandler))
	ctx := tc.ctx(t.Context())
	ran := false
	handler := func(_ any, _ grpc.ServerStream) error {
		ran = true
		return nil
	}
	err := auth.Stream(nil, &fakeServerStream{ctx: ctx}, nil, handler)
	if code := status.Code(err); code != tc.wantCode {
		t.Errorf("Stream code = %v, want %v", code, tc.wantCode)
	}
	if ran != tc.wantRan {
		t.Errorf("Stream handler ran = %v, want %v", ran, tc.wantRan)
	}
}

func TestTokenAuth(t *testing.T) {
	t.Parallel()
	for _, tc := range tokenAuthCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verifyUnary(t, tc)
			verifyStream(t, tc)
		})
	}
}
