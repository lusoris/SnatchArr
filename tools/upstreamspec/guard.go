// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

// guard checks that the cache stays out of the repository and out of the image build context:
//
//   - the cache holds the spec and the contract of every pin, so the checks below compare something,
//   - the index has no file inside the cache directory,
//   - the index has no file with the content of a cache file under another name,
//   - a `.gitignore` file of the repository ignores the cache directory,
//   - `.dockerignore` excludes the cache directory and re-includes nothing inside it.
//
// repo is the repository root; cache must lie inside it. A copy of a part of a cache file is not
// found: the comparison is by the content of whole files.
func guard(ctx context.Context, repo, cache string, pins []pin, out io.Writer) error {
	inside, err := insideRepo(repo, cache)
	if err != nil {
		return err
	}
	files, err := cacheFiles(cache, pins)
	if err != nil {
		return err
	}
	index, err := readIndex(ctx, repo)
	if err != nil {
		return err
	}
	cached, err := blobIDs(ctx, repo, files)
	if err != nil {
		return err
	}
	problems := index.problems(inside, cached)
	unignored, err := gitignoreProblems(ctx, repo, inside)
	if err != nil {
		return err
	}
	// #nosec G304 -- the repository root is the caller's argument; the file is only read
	rules, err := os.ReadFile(filepath.Join(repo, ".dockerignore"))
	if err != nil {
		return fmt.Errorf("read .dockerignore: %w", err)
	}
	problems = append(append(problems, unignored...), dockerignoreProblems(string(rules), inside)...)
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%d problem(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	if _, err := fmt.Fprintf(out, "upstreamspec: %d cache file(s) in %s/, none of them among %d indexed file(s); ignored by git and by .dockerignore\n",
		len(files), inside, len(index)); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

// insideRepo returns cache as a slash path relative to repo and refuses a cache outside of it.
func insideRepo(repo, cache string) (string, error) {
	root, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("repository root: %w", err)
	}
	target, err := filepath.Abs(cache)
	if err != nil {
		return "", fmt.Errorf("cache directory: %w", err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("cache directory: %w", err)
	}
	inside := filepath.ToSlash(relative)
	if !cleanRelative(inside) {
		return "", fmt.Errorf("the cache directory %s is not inside the repository %s", cache, repo)
	}
	return inside, nil
}

// cacheFiles returns the absolute paths of the spec, the command classes and the contract of every pin and refuses a
// cache that lacks one of them.
func cacheFiles(cache string, pins []pin) ([]string, error) {
	directory, err := filepath.Abs(cache)
	if err != nil {
		return nil, fmt.Errorf("cache directory: %w", err)
	}
	files := make([]string, 0, 2*len(pins))
	for _, p := range pins {
		files = append(files, filepath.Join(directory, p.specFile()), filepath.Join(directory, contract.FileName(p.Name)))
		for _, c := range p.Commands {
			files = append(files, filepath.Join(directory, p.commandFile(c)))
		}
	}
	for _, file := range files {
		if info, statErr := os.Stat(file); statErr != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not in the cache; run `upstreamspec fetch` first", file)
		}
	}
	return files, nil
}

// indexed maps the path of each file in the git index to its blob id.
type indexed map[string]string

// readIndex lists the index of the repository.
func readIndex(ctx context.Context, repo string) (indexed, error) {
	listing, err := git(ctx, repo, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	index := indexed{}
	for record := range strings.SplitSeq(listing, "\x00") {
		if record == "" {
			continue
		}
		// <mode> SP <blob id> SP <stage> TAB <path>
		meta, file, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git ls-files: unreadable record %q", record)
		}
		index[file] = fields[1]
	}
	if len(index) == 0 {
		return nil, errors.New("git ls-files lists no file; the check would compare nothing")
	}
	return index, nil
}

// blobIDs returns the name of each cache file under the blob id git gives its content.
func blobIDs(ctx context.Context, repo string, files []string) (map[string]string, error) {
	output, err := git(ctx, repo, append([]string{"hash-object", "--no-filters", "--"}, files...)...)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(output)
	if len(ids) != len(files) {
		return nil, fmt.Errorf("git hash-object returned %d id(s) for %d file(s)", len(ids), len(files))
	}
	byID := make(map[string]string, len(ids))
	for i, id := range ids {
		byID[id] = filepath.Base(files[i])
	}
	return byID, nil
}

// problems returns one line per indexed file inside the cache directory and per indexed file
// that has the content of a cache file.
func (index indexed) problems(inside string, cached map[string]string) []string {
	var problems []string
	for file, id := range index {
		if file == inside || strings.HasPrefix(file, inside+"/") {
			problems = append(problems, file+" is in the git index; the cache is never committed")
			continue
		}
		if name, ok := cached[id]; ok {
			problems = append(problems, fmt.Sprintf("%s is in the git index and has the content of the cache file %s", file, name))
		}
	}
	return problems
}

// gitignoreProblems reports a cache directory that no `.gitignore` file of the repository
// ignores. A rule in `.git/info/exclude` or in a personal excludes file does not count: it is
// not part of what another checkout gets.
func gitignoreProblems(ctx context.Context, repo, inside string) ([]string, error) {
	// --no-index: the answer must not depend on whether the probed file is tracked.
	output, err := git(ctx, repo, "check-ignore", "--verbose", "--no-index", "--", inside+"/probe")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return []string{fmt.Sprintf(".gitignore does not ignore %s/", inside)}, nil
	}
	if err != nil {
		return nil, err
	}
	// <source> ":" <line number> ":" <pattern> TAB <path>
	source, _, _ := strings.Cut(output, ":")
	if path.Base(filepath.ToSlash(source)) != ".gitignore" || path.IsAbs(filepath.ToSlash(source)) {
		return []string{fmt.Sprintf("%s/ is ignored only by %s, which is not a .gitignore file of the repository", inside, source)}, nil
	}
	return nil, nil
}

// git runs one git command in repo and returns its standard output.
func git(ctx context.Context, repo string, arguments ...string) (string, error) {
	// #nosec G204 G702 -- git is a fixed program; the arguments are fixed words and file paths after "--"
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, arguments...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", arguments[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// dockerignoreProblems checks the rules of a .dockerignore file. The last matching rule wins
// there, so a rule that may re-include a path inside the cache is refused wherever it stands.
func dockerignoreProblems(rules, inside string) []string {
	top, _, _ := strings.Cut(inside, "/")
	excluded := false
	var problems []string
	for line := range strings.SplitSeq(rules, "\n") {
		pattern, negated := strings.CutPrefix(strings.TrimSpace(line), "!")
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		pattern = path.Clean(strings.Trim(strings.TrimSpace(pattern), "/"))
		if negated && reaches(pattern, top) {
			problems = append(problems, fmt.Sprintf(".dockerignore rule %q may re-include files of %s/", strings.TrimSpace(line), inside))
		}
		if !negated && (pattern == top || pattern == inside) {
			excluded = true
		}
	}
	if !excluded {
		problems = append(problems, fmt.Sprintf(".dockerignore has no rule %q; the cache would enter the image build context", top))
	}
	return problems
}

// reaches reports whether a .dockerignore pattern may match a path below the directory top: its
// first segment is top or has a wildcard.
func reaches(pattern, top string) bool {
	first, _, _ := strings.Cut(pattern, "/")
	return first == top || strings.ContainsAny(first, "*?[")
}
