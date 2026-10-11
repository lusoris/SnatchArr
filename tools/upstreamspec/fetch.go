// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

const (
	// rawBase serves a file of a GitHub repository at a commit: <base>/<owner>/<name>/<commit>/<path>.
	rawBase = "https://raw.githubusercontent.com"
	// maxSpecBytes bounds one download; the pinned specs are about 300 kB each.
	maxSpecBytes = 4 << 20
)

// fetcher fills the cache directory: the pinned spec files and the contracts built from them.
type fetcher struct {
	client *http.Client
	base   string
	cache  string
	out    io.Writer
}

// all fetches every pin. It stops at the first one that fails. The receipt an earlier contract
// test left for a pin is removed first, so `receipts` counts only the tests that ran afterwards.
func (f fetcher) all(ctx context.Context, pins []pin) error {
	if err := os.MkdirAll(f.cache, 0o750); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	for _, p := range pins {
		if err := os.Remove(filepath.Join(f.cache, contract.ReceiptName(p.Name))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: remove the earlier receipt: %w", p.Name, err)
		}
	}
	for _, p := range pins {
		origin, err := f.one(ctx, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Name, err)
		}
		if _, err := fmt.Fprintf(f.out, "upstreamspec: %s %s (%s): %d operation(s), spec %s\n",
			p.Name, p.Tag, p.Commit[:12], len(p.Operations), origin); err != nil {
			return fmt.Errorf("write summary: %w", err)
		}
	}
	return nil
}

// one puts the spec of p into the cache and writes its contract next to it. It returns where the
// spec came from: "cached" or "downloaded".
func (f fetcher) one(ctx context.Context, p pin) (string, error) {
	file := filepath.Join(f.cache, p.specFile())
	origin := "cached"
	// #nosec G304 -- the cache directory is the caller's argument; the file is hashed before use
	data, err := os.ReadFile(file)
	if err != nil || digest(data) != p.SHA256 {
		origin = "downloaded"
		if data, err = f.download(ctx, p); err != nil {
			return "", err
		}
		if err = os.WriteFile(file, data, 0o600); err != nil {
			return "", fmt.Errorf("write spec: %w", err)
		}
	}
	doc, err := parseDocument(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", p.Path, err)
	}
	built, err := doc.contractOf(p)
	if err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(built, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode contract: %w", err)
	}
	if err := os.WriteFile(filepath.Join(f.cache, contract.FileName(p.Name)), encoded, 0o600); err != nil {
		return "", fmt.Errorf("write contract: %w", err)
	}
	return origin, nil
}

// download reads the pinned file and returns it only when it has the pinned checksum.
func (f fetcher) download(ctx context.Context, p pin) ([]byte, error) {
	source := p.source(f.base)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", source, err)
	}
	// #nosec G107 G704 -- the address is the fixed base plus manifest fields matched against patterns
	reply, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", source, err)
	}
	data, err := body(reply)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", source, err)
	}
	if len(data) > maxSpecBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes; nothing was written", source, maxSpecBytes)
	}
	if got := digest(data); got != p.SHA256 {
		return nil, fmt.Errorf("%s at commit %s has sha256 %s, the manifest pins %s; nothing was written",
			p.Path, p.Commit, got, p.SHA256)
	}
	return data, nil
}

// body closes the reply and returns what a 200 reply carried, at most one byte above the size bound.
func body(reply *http.Response) ([]byte, error) {
	if reply.StatusCode != http.StatusOK {
		return nil, errors.Join(fmt.Errorf("HTTP %d", reply.StatusCode), reply.Body.Close())
	}
	data, err := io.ReadAll(io.LimitReader(reply.Body, maxSpecBytes+1))
	if err = errors.Join(err, reply.Body.Close()); err != nil {
		return nil, fmt.Errorf("read the reply: %w", err)
	}
	return data, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// errUsage is returned for a command line the tool does not understand.
var errUsage = errors.New("usage: upstreamspec <fetch|guard|receipts> [-manifest <specs.yaml>] [-cache <directory>] [-repo <repository root>]")
