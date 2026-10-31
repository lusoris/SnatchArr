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

	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

// prePullAttempts is the maximum number of attempts to pull a container image.
const prePullAttempts = 3

// prePullBaseDelay is the default base duration between pull attempts.
var prePullBaseDelay = 2 * time.Second

// imagePuller defines the subset of the Docker client needed to pull an image.
type imagePuller interface {
	ImagePull(ctx context.Context, ref string, opts client.ImagePullOptions) (client.ImagePullResponse, error)
}

// authResolver resolves and encodes Docker registry authentication for an image reference.
type authResolver func(ctx context.Context, ref string) (string, error)

// defaultAuthResolver resolves Docker registry authentication for the given image reference,
// mirroring testcontainers-go attemptToPullImage.
func defaultAuthResolver(ctx context.Context, ref string) (string, error) {
	_, imageAuth, err := testcontainers.DockerImageAuth(ctx, ref)
	if err == nil {
		encoded, encodeErr := authconfig.Encode(imageAuth)
		if encodeErr != nil {
			return "", fmt.Errorf("encode registry auth: %w", encodeErr)
		}
		return encoded, nil
	}
	return "", nil
}

// prePullOptions configures prePull retry delays and authentication resolution.
type prePullOptions struct {
	baseDelay    time.Duration
	authResolver authResolver
}

// resolvePrePullOptions applies defaults to optional prePull settings.
func resolvePrePullOptions(opts []prePullOptions) (time.Duration, authResolver) {
	if len(opts) == 0 {
		return prePullBaseDelay, defaultAuthResolver
	}
	delay := opts[0].baseDelay
	resolver := defaultAuthResolver
	if opts[0].authResolver != nil {
		resolver = opts[0].authResolver
	}
	return delay, resolver
}

// waitDelay sleeps for d or until ctx is canceled.
func waitDelay(ctx context.Context, d time.Duration) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	timer := time.NewTimer(d)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
// Test seams for base delay and registry authentication can be passed via opts.
func prePull(ctx context.Context, p imagePuller, ref string, opts ...prePullOptions) error {
	baseDelay, resolver := resolvePrePullOptions(opts)

	auth, err := resolver(ctx, ref)
	if err != nil {
		return fmt.Errorf("resolve auth for %s: %w", ref, err)
	}
	pullOpts := client.ImagePullOptions{
		RegistryAuth: auth,
	}

	var lastErr error
	for attempt := 1; attempt <= prePullAttempts; attempt++ {
		resp, pullErr := p.ImagePull(ctx, ref, pullOpts)
		if pullErr != nil {
			lastErr = pullErr
		} else {
			waitErr := resp.Wait(ctx)
			closeErr := resp.Close()
			lastErr = errors.Join(waitErr, closeErr)
			if lastErr == nil {
				return nil
			}
		}

		if attempt == prePullAttempts {
			break
		}
		if waitErr := waitDelay(ctx, time.Duration(attempt)*baseDelay); waitErr != nil {
			return waitErr
		}
	}
	return fmt.Errorf("pull %s after %d attempts: %w", ref, prePullAttempts, lastErr)
}

// fakeResponse implements client.ImagePullResponse for tests.
type fakeResponse struct {
	waitErr  error
	closeErr error
}

// Read implements io.Reader.
func (f *fakeResponse) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

// Close implements io.Closer.
func (f *fakeResponse) Close() error {
	return f.closeErr
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
	pullErrs  []error
	waitErrs  []error
	closeErrs []error
	lastOpts  client.ImagePullOptions
	calls     int
	onPull    func(call int)
}

// ImagePull simulates pulling an image and returns configured responses or errors.
func (f *fakePuller) ImagePull(_ context.Context, _ string, opts client.ImagePullOptions) (client.ImagePullResponse, error) {
	f.calls++
	f.lastOpts = opts
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
	var closeErr error
	if len(f.closeErrs) > 0 {
		closeErr = f.closeErrs[0]
		if len(f.closeErrs) > 1 {
			f.closeErrs = f.closeErrs[1:]
		}
	}
	return &fakeResponse{waitErr: waitErr, closeErr: closeErr}, nil
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
	closeErrs     []error
	authResolver  authResolver
	cancelOnPull  int
	wantCalls     int
	wantAuth      string
	checkAuth     bool
	wantErrSub    string
	wantCtxCancel bool
}

// checkPrePullResult validates test expectations for prePull test cases.
func checkPrePullResult(t *testing.T, tc prePullTestCase, err error, fake *fakePuller, ref string) {
	t.Helper()
	if fake.calls != tc.wantCalls {
		t.Errorf("call count = %d, want %d", fake.calls, tc.wantCalls)
	}
	if tc.checkAuth && fake.lastOpts.RegistryAuth != tc.wantAuth {
		t.Errorf("registry auth = %q, want %q", fake.lastOpts.RegistryAuth, tc.wantAuth)
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
		{
			name:         "F1 resolver returning encoded auth passes it to pull options",
			authResolver: func(_ context.Context, _ string) (string, error) { return "abc", nil },
			wantCalls:    1,
			checkAuth:    true,
			wantAuth:     "abc",
		},
		{
			name: "F1 resolver error on docker image auth yields empty auth and pull proceeds",
			authResolver: func(_ context.Context, _ string) (string, error) {
				return "", nil
			},
			wantCalls: 1,
			checkAuth: true,
			wantAuth:  "",
		},
		{
			name: "F1 resolver error on encode returns wrapped error promptly",
			authResolver: func(_ context.Context, _ string) (string, error) {
				return "", errors.New("encode registry auth failed")
			},
			wantCalls:  0,
			wantErrSub: "encode registry auth failed",
		},
		{
			name:       "F2 close error with wait nil retries and fails with close error text",
			waitErrs:   []error{nil},
			closeErrs:  []error{errors.New("stream close failed")},
			wantCalls:  3,
			wantErrSub: "stream close failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			fake := &fakePuller{
				pullErrs:  tt.pullErrs,
				waitErrs:  tt.waitErrs,
				closeErrs: tt.closeErrs,
				onPull: func(call int) {
					if tt.cancelOnPull > 0 && call == tt.cancelOnPull {
						cancel()
					}
				},
			}

			const ref = "postgres:17-alpine"
			err := prePull(ctx, fake, ref, prePullOptions{
				baseDelay:    0,
				authResolver: tt.authResolver,
			})
			checkPrePullResult(t, tt, err, fake, ref)
		})
	}
}

// TestDefaultAuthResolver checks the real resolver against controlled Docker auth configs, never the host's.
func TestDefaultAuthResolver(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		ref      string
		wantAuth bool
	}{
		{name: "credentials for the mirror are sent", config: `{"auths":{"mirror.example":{"auth":"dTpw"}}}`, ref: "mirror.example/postgres:17-alpine", wantAuth: true},
		{name: "registry without credentials pulls anonymously", config: `{"auths":{"mirror.example":{"auth":"dTpw"}}}`, ref: "other.example/postgres:17-alpine"},
		{name: "unreadable auth config pulls anonymously", config: `{`, ref: "mirror.example/postgres:17-alpine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DOCKER_CONFIG", t.TempDir())
			t.Setenv("DOCKER_AUTH_CONFIG", tt.config)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			auth, err := defaultAuthResolver(ctx, tt.ref)
			if err != nil {
				t.Fatalf("defaultAuthResolver(%s): %v", tt.ref, err)
			}
			if got := auth != ""; got != tt.wantAuth {
				t.Errorf("defaultAuthResolver(%s) auth set = %v, want %v", tt.ref, got, tt.wantAuth)
			}
		})
	}
}
