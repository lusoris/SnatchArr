// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
)

// prePullAttempts is the maximum number of attempts to pull a container image.
const prePullAttempts = 3

// prePullBaseDelay is the default base duration between pull attempts.
var prePullBaseDelay = 2 * time.Second

// imagePuller defines the subset of the Docker client needed to pull an image.
type imagePuller interface {
	ImagePull(ctx context.Context, ref string, opts client.ImagePullOptions) (client.ImagePullResponse, error)
}

// hubRef mirrors testcontainers prependHubRegistry for registry-less images.
func hubRef(prefix, image string) string {
	if prefix == "" {
		return image
	}
	seg, _, hasSlash := strings.Cut(image, "/")
	if hasSlash && (strings.Contains(seg, ".") || strings.Contains(seg, ":") || seg == "localhost") {
		return image
	}
	return path.Join(prefix, image)
}

// prePull pulls the given image reference with explicit error checking and retries.
// An optional base delay can be passed as a seam for testing; if omitted, prePullBaseDelay is used.
func prePull(ctx context.Context, p imagePuller, ref string, delays ...time.Duration) error {
	baseDelay := prePullBaseDelay
	if len(delays) > 0 {
		baseDelay = delays[0]
	}

	var lastErr error
	for attempt := 1; attempt <= prePullAttempts; attempt++ {
		resp, err := p.ImagePull(ctx, ref, client.ImagePullOptions{})
		if err != nil {
			lastErr = err
		} else {
			lastErr = resp.Wait(ctx)
			_ = resp.Close()
			if lastErr == nil {
				return nil
			}
		}

		if attempt == prePullAttempts {
			break
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		timer := time.NewTimer(time.Duration(attempt) * baseDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("pull %s after %d attempts: %w", ref, prePullAttempts, lastErr)
}

// fakeResponse implements client.ImagePullResponse for tests.
type fakeResponse struct {
	waitErr error
}

// Read implements io.Reader.
func (f *fakeResponse) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

// Close implements io.Closer.
func (f *fakeResponse) Close() error {
	return nil
}

// JSONMessages implements client.ImagePullResponse.
func (f *fakeResponse) JSONMessages(_ context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(_ func(jsonstream.Message, error) bool) {}
}

// Wait implements client.ImagePullResponse.
func (f *fakeResponse) Wait(_ context.Context) error {
	return f.waitErr
}

// fakePuller implements imagePuller for testing prePull retry behavior.
type fakePuller struct {
	pullErrs []error
	waitErrs []error
	calls    int
	onPull   func(call int)
}

// ImagePull simulates pulling an image and returns configured responses or errors.
func (f *fakePuller) ImagePull(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
	f.calls++
	if f.onPull != nil {
		f.onPull(f.calls)
	}
	if len(f.pullErrs) > 0 {
		err := f.pullErrs[0]
		if len(f.pullErrs) > 1 {
			f.pullErrs = f.pullErrs[1:]
		}
		if err != nil {
			return nil, err
		}
	}
	var waitErr error
	if len(f.waitErrs) > 0 {
		waitErr = f.waitErrs[0]
		if len(f.waitErrs) > 1 {
			f.waitErrs = f.waitErrs[1:]
		}
	}
	return &fakeResponse{waitErr: waitErr}, nil
}

// TestHubRef tests image reference resolution with and without custom registry prefixes.
func TestHubRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		prefix string
		image  string
		want   string
	}{
		{
			name:   "empty prefix returns image unchanged",
			prefix: "",
			image:  "postgres:17-alpine",
			want:   "postgres:17-alpine",
		},
		{
			name:   "hub image with digest is prefixed",
			prefix: "mirror.gcr.io",
			image:  "postgres:17-alpine@sha256:x",
			want:   "mirror.gcr.io/postgres:17-alpine@sha256:x",
		},
		{
			name:   "external registry host ghcr is unchanged",
			prefix: "mirror.gcr.io",
			image:  "ghcr.io/a/b:1",
			want:   "ghcr.io/a/b:1",
		},
		{
			name:   "localhost registry with port is unchanged",
			prefix: "mirror.gcr.io",
			image:  "localhost:5000/a",
			want:   "localhost:5000/a",
		},
		{
			name:   "hub image with library segment is prefixed",
			prefix: "mirror.gcr.io",
			image:  "library/postgres:18",
			want:   "mirror.gcr.io/library/postgres:18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hubRef(tt.prefix, tt.image); got != tt.want {
				t.Errorf("hubRef(%q, %q) = %q, want %q", tt.prefix, tt.image, got, tt.want)
			}
		})
	}
}

// prePullTestCase defines the expectations for a single prePull test scenario.
type prePullTestCase struct {
	name          string
	pullErrs      []error
	waitErrs      []error
	cancelOnPull  int
	wantCalls     int
	wantErrSub    string
	wantCtxCancel bool
}

// checkPrePullResult validates test expectations for prePull test cases.
func checkPrePullResult(t *testing.T, tc prePullTestCase, err error, calls int, ref string) {
	t.Helper()
	if calls != tc.wantCalls {
		t.Errorf("call count = %d, want %d", calls, tc.wantCalls)
	}
	if tc.wantCtxCancel {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got err %v, want context.Canceled", err)
		}
		return
	}
	if tc.wantErrSub != "" {
		if err == nil {
			t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, ref) || !strings.Contains(errMsg, tc.wantErrSub) {
			t.Errorf("error %q does not contain ref %q and %q", errMsg, ref, tc.wantErrSub)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPrePull tests retry behavior, stream error handling, and cancellation during image pull.
func TestPrePull(t *testing.T) {
	t.Parallel()

	tests := []prePullTestCase{
		{
			name:       "positive wait nil succeeds on first call",
			waitErrs:   []error{nil},
			wantCalls:  1,
			wantErrSub: "",
		},
		{
			name:       "negative wait errorDetail manifest unknown retries then fails",
			waitErrs:   []error{errors.New("manifest unknown")},
			wantCalls:  3,
			wantErrSub: "manifest unknown",
		},
		{
			name:       "negative image pull error retries then fails",
			pullErrs:   []error{errors.New("daemon connection refused")},
			wantCalls:  3,
			wantErrSub: "daemon connection refused",
		},
		{
			name:       "boundary fail twice then succeed on third attempt",
			waitErrs:   []error{errors.New("temporary failure 1"), errors.New("temporary failure 2"), nil},
			wantCalls:  3,
			wantErrSub: "",
		},
		{
			name:          "ctx cancelled before retry returns ctx error promptly",
			waitErrs:      []error{errors.New("initial failure")},
			cancelOnPull:  1,
			wantCalls:     1,
			wantCtxCancel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			fake := &fakePuller{
				pullErrs: tt.pullErrs,
				waitErrs: tt.waitErrs,
				onPull: func(call int) {
					if tt.cancelOnPull > 0 && call == tt.cancelOnPull {
						cancel()
					}
				},
			}

			const ref = "postgres:17-alpine"
			err := prePull(ctx, fake, ref, 0)
			checkPrePullResult(t, tt, err, fake.calls, ref)
		})
	}
}
