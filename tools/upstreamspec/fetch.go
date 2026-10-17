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
		if _, err := fmt.Fprintf(f.out, "upstreamspec: %s %s (%s): %d operation(s)%s, spec %s\n",
			p.Name, p.ref(), p.Commit[:12], len(p.Operations), commandSummary(p), origin); err != nil {
			return fmt.Errorf("write summary: %w", err)
		}
	}
	return nil
}

// commandSummary names the command classes of p for the summary line, and nothing for a pin
// without any.
func commandSummary(p pin) string {
	if len(p.Commands) == 0 {
		return ""
	}
	return fmt.Sprintf(", %d command class(es)", len(p.Commands))
}

// one puts the spec of p and its command classes into the cache and writes its contract next to
// them. It returns where the spec came from: "cached" or "downloaded".
func (f fetcher) one(ctx context.Context, p pin) (string, error) {
	data, origin, err := f.pinned(ctx, p, p.Path, p.specFile(), p.SHA256)
	if err != nil {
		return "", err
	}
	doc, err := parseDocument(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", p.Path, err)
	}
	built, err := doc.contractOf(p)
	if err != nil {
		return "", err
	}
	if built.Commands, err = f.commands(ctx, p); err != nil {
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

// commands puts each command class of p into the cache and returns the properties of every
// command by name. Two classes of one pin never declare the same command.
func (f fetcher) commands(ctx context.Context, p pin) (map[string][]string, error) {
	if len(p.Commands) == 0 {
		return nil, nil //nolint:nilnil // a pin without command classes has no commands
	}
	commands := make(map[string][]string, len(p.Commands))
	for _, c := range p.Commands {
		source, _, err := f.pinned(ctx, p, c.Path, p.commandFile(c), c.SHA256)
		if err != nil {
			return nil, err
		}
		name, properties, err := parseCommand(source)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Path, err)
		}
		if _, repeated := commands[name]; repeated {
			return nil, fmt.Errorf("%s: declares the command %s again", c.Path, name)
		}
		commands[name] = properties
	}
	return commands, nil
}

// pinned returns the file at remote in the repository of p, at its commit: from the cache when the
// cached bytes have the pinned checksum, downloaded otherwise.
func (f fetcher) pinned(ctx context.Context, p pin, remote, cached, want string) (data []byte, origin string, err error) {
	file := filepath.Join(f.cache, cached)
	// #nosec G304 -- the cache directory is the caller's argument; the file is hashed before use
	data, err = os.ReadFile(file)
	if err == nil && digest(data) == want {
		return data, "cached", nil
	}
	if data, err = f.download(ctx, p, remote, want); err != nil {
		return nil, "", err
	}
	if err = os.WriteFile(file, data, 0o600); err != nil {
		return nil, "", fmt.Errorf("write %s: %w", cached, err)
	}
	return data, "downloaded", nil
}

// download reads the pinned file and returns it only when it has the pinned checksum.
func (f fetcher) download(ctx context.Context, p pin, remote, want string) ([]byte, error) {
	source := p.sourceOf(f.base, remote)
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
	if got := digest(data); got != want {
		return nil, fmt.Errorf("%s at commit %s has sha256 %s, the manifest pins %s; nothing was written",
			remote, p.Commit, got, want)
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
