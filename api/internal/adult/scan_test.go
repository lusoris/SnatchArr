// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestAcceptedExtensions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ext  string
		want bool
	}{
		{ext: ".jpg", want: true},
		{ext: ".JPG", want: true},
		{ext: ".jpeg", want: true},
		{ext: ".JPEG", want: true},
		{ext: ".png", want: true},
		{ext: ".PNG", want: true},
		{ext: ".webp", want: true},
		{ext: ".WEBP", want: true},
		{ext: ".gif", want: true},
		{ext: ".GIF", want: true},
		{ext: ".txt", want: false},
		{ext: ".bmp", want: false},
		{ext: ".tiff", want: false},
		{ext: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			t.Parallel()
			got := isAcceptedImageExt(tc.ext)
			if got != tc.want {
				t.Fatalf("isAcceptedImageExt(%q) = %v, want %v", tc.ext, got, tc.want)
			}
		})
	}
}

func TestMatchMimeWithExt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ext  string
		mime string
		want bool
	}{
		{ext: ".jpg", mime: "image/jpeg", want: true},
		{ext: ".jpeg", mime: "image/jpeg", want: true},
		{ext: ".png", mime: "image/png", want: true},
		{ext: ".gif", mime: "image/gif", want: true},
		{ext: ".webp", mime: "image/webp", want: true},
		{ext: ".png", mime: "image/jpeg", want: false},
		{ext: ".jpg", mime: "image/png", want: false},
		{ext: ".jpg", mime: "text/plain", want: false},
		{ext: ".gif", mime: "image/webp", want: false},
		{ext: ".bin", mime: "application/octet-stream", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.ext+"_"+tc.mime, func(t *testing.T) {
			t.Parallel()
			got := matchMimeWithExt(tc.ext, tc.mime)
			if got != tc.want {
				t.Fatalf("matchMimeWithExt(%q, %q) = %v, want %v", tc.ext, tc.mime, got, tc.want)
			}
		})
	}
}

func TestFitAspect(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		srcW, srcH int
		maxW, maxH int
		wantW      int
		wantH      int
	}{
		{name: "smaller preserved", srcW: 200, srcH: 100, maxW: 400, maxH: 400, wantW: 200, wantH: 100},
		{name: "square larger scaled", srcW: 800, srcH: 800, maxW: 400, maxH: 400, wantW: 400, wantH: 400},
		{name: "landscape scaled", srcW: 800, srcH: 400, maxW: 400, maxH: 400, wantW: 400, wantH: 200},
		{name: "portrait scaled", srcW: 400, srcH: 800, maxW: 400, maxH: 400, wantW: 200, wantH: 400},
		{name: "zero input fallback", srcW: 0, srcH: 0, maxW: 400, maxH: 400, wantW: 400, wantH: 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotW, gotH := fitAspect(tc.srcW, tc.srcH, tc.maxW, tc.maxH)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Fatalf("fitAspect(%d, %d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.srcW, tc.srcH, tc.maxW, tc.maxH, gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestIsCleanSubdir(t *testing.T) {
	t.Parallel()
	base := "/tmp/media/imagesets"
	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "valid sub dir", target: filepath.Join(base, "gallery1"), want: true},
		{name: "valid nested file", target: filepath.Join(base, "gallery1", "image.jpg"), want: true},
		{name: "same directory", target: base, want: false},
		{name: "escape parent", target: "/tmp/media/other", want: false},
		{name: "escape relative", target: "/tmp/media/imagesets/../other", want: false},
		{name: "root escape", target: "/etc/passwd", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isCleanSubdir(base, tc.target)
			if got != tc.want {
				t.Fatalf("isCleanSubdir(%q, %q) = %v, want %v", base, tc.target, got, tc.want)
			}
		})
	}
}

func TestLogSafeErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		// Positive: fs.PathError paths stripped
		{
			name: "path error stripped",
			err:  &fs.PathError{Op: "open", Path: "/secret/path/to/image.jpg", Err: os.ErrNotExist},
			want: "open: file does not exist",
		},
		{
			name: "wrapped path error stripped",
			err:  fmt.Errorf("wrapped context: %w", &fs.PathError{Op: "stat", Path: "/etc/passwd", Err: os.ErrPermission}),
			want: "stat: permission denied",
		},
		{
			name: "link error paths stripped",
			err:  &os.LinkError{Op: "symlink", Old: "/a/secret/path", New: "/b/target/path", Err: os.ErrExist},
			want: "symlink: file already exists",
		},
		// Negative: standard errors unchanged
		{
			name: "standard error unchanged",
			err:  errors.New("postgres connection timeout"),
			want: "postgres connection timeout",
		},
		{
			name: "wrapped standard error unchanged",
			err:  fmt.Errorf("query error: %w", errors.New("table not found")),
			want: "query error: table not found",
		},
		// Boundary: nil, empty, nil inner errors
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
		{
			name: "empty error string",
			err:  errors.New(""),
			want: "",
		},
		{
			name: "path error with nil inner err",
			err:  &fs.PathError{Op: "read", Path: "/var/log/app.log", Err: nil},
			want: "read",
		},
		{
			name: "link error with nil inner err",
			err:  &os.LinkError{Op: "link", Old: "/old", New: "/new", Err: nil},
			want: "link",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := logSafeErr(tc.err)
			if got != tc.want {
				t.Fatalf("logSafeErr() = %q, want %q", got, tc.want)
			}
		})
	}
}
