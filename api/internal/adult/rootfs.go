// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"fmt"
	"os"
	"path/filepath"

	gerr "github.com/golusoris/golusoris/core/errors"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func resolveRootRel(base, path string) (string, string, error) {
	cleanBase := filepath.Clean(base)
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(filepath.Join(cleanBase, cleanPath))
	}
	if !isCleanSubdir(cleanBase, cleanPath) {
		return "", "", fmt.Errorf("adult: file path escapes base dir: %w", domain.ErrInvalid)
	}
	rel, err := filepath.Rel(cleanBase, cleanPath)
	if err != nil {
		return "", "", fmt.Errorf("adult: rel path: %w", domain.ErrInvalid)
	}
	return cleanBase, rel, nil
}

func verifyRegularFile(lfi, ffi os.FileInfo) error {
	if !ffi.Mode().IsRegular() || !os.SameFile(lfi, ffi) {
		return fmt.Errorf("adult: file is symlink or not regular: %w", domain.ErrInvalid)
	}
	return nil
}

func openInRoot(root *os.Root, rel string, openFn func(root *os.Root, rel string) (*os.File, error)) (*os.File, error) {
	if openFn == nil {
		openFn = defaultRootOpen
	}
	return openFn(root, rel)
}

// openRegularInRoot opens the file at path strictly within base via os.Root,
// verifying that path does not escape base, is not a symlink, is a regular file,
// and that no symlink swap occurred between Lstat and Open.
func openRegularInRoot(base, path string) (*os.File, os.FileInfo, error) {
	return openRegularInRootWith(base, path, nil)
}

// openRegularInRootWith opens the file at path strictly within base using the
// provided opener (or root.Open if nil), validating containment, regular file
// status, and that no symlink swap occurred between Lstat and Open.
func openRegularInRootWith(
	base, path string,
	openFn func(root *os.Root, rel string) (*os.File, error),
) (f *os.File, fi os.FileInfo, err error) {
	cleanBase, rel, err := resolveRootRel(base, path)
	if err != nil {
		return nil, nil, err
	}

	root, err := os.OpenRoot(cleanBase)
	if err != nil {
		return nil, nil, fmt.Errorf("adult: open root: %w", err)
	}
	// Runs after the root close below: a failed root close must not leak f.
	defer closeOnErr(&f, &fi, &err)
	defer gerr.CloseInto(root, &err, "adult: close root")

	lfi, err := root.Lstat(rel)
	if err != nil {
		return nil, nil, fmt.Errorf("adult: lstat root file: %w", err)
	}
	if !lfi.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("adult: file is symlink or not regular: %w", domain.ErrInvalid)
	}

	f, err = openInRoot(root, rel, openFn)
	if err != nil {
		return nil, nil, err
	}

	ffi, err := f.Stat()
	if err != nil {
		gerr.CloseInto(f, &err, "adult: close file")
		return nil, nil, fmt.Errorf("adult: stat open file: %w", err)
	}
	if err := verifyRegularFile(lfi, ffi); err != nil {
		gerr.CloseInto(f, &err, "adult: close invalid file")
		return nil, nil, err
	}

	return f, ffi, nil
}

// closeOnErr closes *f and clears the results when *err is set.
func closeOnErr(f **os.File, fi *os.FileInfo, err *error) {
	if *err == nil || *f == nil {
		return
	}
	gerr.CloseInto(*f, err, "adult: close file")
	*f, *fi = nil, nil
}
