// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/tools/upstreamspec/contract"
)

const (
	cachedSpec     = "openapi: 3.0.1\npaths: {}\n"
	cachedContract = `{"spec":"sample","operations":{"GET /api/v1/status":{"parameters":[],"response":{"version":"string"}}}}`
)

// gitEnvironment is what a git hook exports. A guard test works on its own repository, so it must
// not inherit the index or the directory of the repository the hook runs in.
var gitEnvironment = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR", "GIT_PREFIX", "GIT_NAMESPACE",
}

// isolate points git at an empty configuration and removes the variables a hook exports.
func isolate(t *testing.T) {
	t.Helper()
	for _, name := range gitEnvironment {
		t.Setenv(name, "") // registers the restore
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func runGit(t *testing.T, repo string, arguments ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}
}

// sandbox is a git repository in the state the guard accepts: the cache ignored by
// `.gitignore` and by `.dockerignore`, filled for the sample pin, and one tracked file.
func sandbox(t *testing.T) (repo, cache string) {
	t.Helper()
	repo = t.TempDir()
	cache = filepath.Join(repo, ".cache", "upstream-specs")
	runGit(t, repo, "init", "--quiet")
	writeFile(t, filepath.Join(repo, ".gitignore"), "/.cache/\n")
	writeFile(t, filepath.Join(repo, ".dockerignore"), "# build context\n.git\n.cache\ndocs\n")
	writeFile(t, filepath.Join(repo, "README.md"), "sample\n")
	writeFile(t, filepath.Join(cache, "sample.spec"), cachedSpec)
	writeFile(t, filepath.Join(cache, contract.FileName("sample")), cachedContract)
	runGit(t, repo, "add", ".gitignore", ".dockerignore", "README.md")
	return repo, cache
}

func guardOf(t *testing.T, repo, cache string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out bytes.Buffer
	err := guard(ctx, repo, cache, []pin{{Name: "sample"}}, &out)
	return out.String(), err
}

//nolint:paralleltest // isolate sets environment variables, which a parallel test may not
func TestGuardAcceptsACleanRepository(t *testing.T) {
	isolate(t)
	repo, cache := sandbox(t)
	out, err := guardOf(t, repo, cache)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	want := "upstreamspec: 2 cache file(s) in .cache/upstream-specs/, none of them among 3 indexed file(s); ignored by git and by .dockerignore\n"
	if out != want {
		t.Errorf("summary = %q, want %q", out, want)
	}
}

//nolint:paralleltest // isolate sets environment variables, which a parallel test may not
func TestGuardRefuses(t *testing.T) {
	isolate(t)
	tests := []struct {
		name  string
		plant func(t *testing.T, repo, cache string)
		want  []string // the problems, each as a part of one line of the error
	}{
		{
			"a cache file added by force",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				runGit(t, repo, "add", "--force", ".cache/upstream-specs/sample.spec")
			},
			[]string{".cache/upstream-specs/sample.spec is in the git index; the cache is never committed"},
		},
		{
			"a spec copied to a tracked file",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, "api/testdata/sonarr.json"), cachedSpec)
				runGit(t, repo, "add", "api/testdata/sonarr.json")
			},
			[]string{"api/testdata/sonarr.json is in the git index and has the content of the cache file sample.spec"},
		},
		{
			"a contract copied to a tracked file",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, "worker/fixtures/queue.json"), cachedContract)
				runGit(t, repo, "add", "worker/fixtures/queue.json")
			},
			[]string{"worker/fixtures/queue.json is in the git index and has the content of the cache file sample.contract.json"},
		},
		{
			"a .gitignore without the cache",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, ".gitignore"), "/target/\n")
			},
			[]string{".gitignore does not ignore .cache/upstream-specs/"},
		},
		{
			"a .gitignore that takes the cache back out",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, ".gitignore"), "/.cache/*\n!/.cache/upstream-specs/\n")
			},
			[]string{".gitignore does not ignore .cache/upstream-specs/"},
		},
		{
			"a cache ignored only by the local exclude file",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, ".gitignore"), "/target/\n")
				writeFile(t, filepath.Join(repo, ".git/info/exclude"), "/.cache/\n")
			},
			[]string{".cache/upstream-specs/ is ignored only by .git/info/exclude, which is not a .gitignore file"},
		},
		{
			"a .dockerignore without the cache",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, ".dockerignore"), ".git\ndocs\n")
			},
			[]string{`.dockerignore has no rule ".cache"; the cache would enter the image build context`},
		},
		{
			"a .dockerignore that takes a cache file back in",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, ".dockerignore"), ".cache\n!.cache/upstream-specs/sample.spec\n")
			},
			[]string{`.dockerignore rule "!.cache/upstream-specs/sample.spec" may re-include files of .cache/upstream-specs/`},
		},
		{
			"every problem at once",
			func(t *testing.T, repo, _ string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, ".gitignore"), "")
				writeFile(t, filepath.Join(repo, ".dockerignore"), "!**/*.json\n")
				writeFile(t, filepath.Join(repo, "copy.yaml"), cachedSpec)
				runGit(t, repo, "add", "copy.yaml", ".cache/upstream-specs/sample.contract.json")
			},
			[]string{
				"5 problem(s)",
				".cache/upstream-specs/sample.contract.json is in the git index",
				"copy.yaml is in the git index and has the content of the cache file sample.spec",
				".gitignore does not ignore .cache/upstream-specs/",
				`.dockerignore rule "!**/*.json" may re-include files`,
				`.dockerignore has no rule ".cache"`,
			},
		},
	}
	for _, test := range tests { //nolint:paralleltest // the parent sets environment variables
		t.Run(test.name, func(t *testing.T) {
			repo, cache := sandbox(t)
			test.plant(t, repo, cache)
			out, err := guardOf(t, repo, cache)
			if err == nil {
				t.Fatalf("guard accepted the repository: %s", out)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error lacks %q:\n%v", want, err)
				}
			}
		})
	}
}

//nolint:paralleltest // isolate sets environment variables, which a parallel test may not
func TestGuardNeedsSomethingToCompare(t *testing.T) {
	isolate(t)
	tests := []struct {
		name  string
		plant func(t *testing.T, repo, cache string) (useCache string)
		want  string
	}{
		{
			"a cache without the spec",
			func(t *testing.T, _, cache string) string {
				t.Helper()
				if err := os.Remove(filepath.Join(cache, "sample.spec")); err != nil {
					t.Fatalf("remove: %v", err)
				}
				return cache
			},
			"sample.spec is not in the cache; run `upstreamspec fetch` first",
		},
		{
			"a cache without the contract",
			func(t *testing.T, _, cache string) string {
				t.Helper()
				if err := os.Remove(filepath.Join(cache, contract.FileName("sample"))); err != nil {
					t.Fatalf("remove: %v", err)
				}
				return cache
			},
			"sample.contract.json is not in the cache; run `upstreamspec fetch` first",
		},
		{
			"a directory where the spec should be",
			func(t *testing.T, _, cache string) string {
				t.Helper()
				if err := os.Remove(filepath.Join(cache, "sample.spec")); err != nil {
					t.Fatalf("remove: %v", err)
				}
				writeFile(t, filepath.Join(cache, "sample.spec", "inner"), cachedSpec)
				return cache
			},
			"sample.spec is not in the cache",
		},
		{
			"a cache outside the repository",
			func(t *testing.T, _, _ string) string { t.Helper(); return t.TempDir() },
			"is not inside the repository",
		},
		{
			"the repository root as the cache",
			func(t *testing.T, repo, _ string) string { t.Helper(); return repo },
			"is not inside the repository",
		},
		{
			"a repository with an empty index",
			func(t *testing.T, repo, cache string) string {
				t.Helper()
				runGit(t, repo, "rm", "--cached", "--quiet", ".gitignore", ".dockerignore", "README.md")
				return cache
			},
			"git ls-files lists no file",
		},
		{
			"a repository without .dockerignore",
			func(t *testing.T, repo, cache string) string {
				t.Helper()
				if err := os.Remove(filepath.Join(repo, ".dockerignore")); err != nil {
					t.Fatalf("remove: %v", err)
				}
				return cache
			},
			"read .dockerignore",
		},
		{
			"a directory that is no repository",
			func(t *testing.T, repo, cache string) string {
				t.Helper()
				if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
					t.Fatalf("remove: %v", err)
				}
				// Wherever the temporary directory lies, git must not find a repository above it.
				t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(repo))
				return cache
			},
			"git ls-files",
		},
	}
	for _, test := range tests { //nolint:paralleltest // the parent sets environment variables
		t.Run(test.name, func(t *testing.T) {
			repo, cache := sandbox(t)
			out, err := guardOf(t, repo, test.plant(t, repo, cache))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("guard = %q, %v; want an error with %q", out, err, test.want)
			}
		})
	}
}

func TestDockerignoreProblems(t *testing.T) {
	t.Parallel()
	const inside = ".cache/upstream-specs"
	tests := []struct {
		name  string
		rules string
		want  []string // parts, one per problem
	}{
		{"the top directory", ".cache\n", nil},
		{"the top directory with slashes", "/.cache/\n", nil},
		{"the cache directory itself", ".cache/upstream-specs\n", nil},
		{"comments, blank lines and carriage returns", "# context\r\n\r\n  .cache  \r\ntools\r\n", nil},
		{"a re-include that cannot reach the cache", ".cache\n!docs/readme.md\n", nil},
		{"no rule at all", "", []string{"has no rule"}},
		{"a rule for a sibling", ".cachefiles\n.cache2\n", []string{"has no rule"}},
		{"a rule for a file inside only", ".cache/upstream-specs/sonarr.spec\n", []string{"has no rule"}},
		{"a comment that names the cache", "# .cache\n", []string{"has no rule"}},
		{"a re-include of the top directory", ".cache\n!.cache\n", []string{`rule "!.cache" may re-include`}},
		{"a re-include before the exclusion", "!.cache/x\n.cache\n", []string{`rule "!.cache/x" may re-include`}},
		{"a re-include with a wildcard", ".cache\n!*/upstream-specs\n", []string{`rule "!*/upstream-specs" may re-include`}},
		{"a re-include of everything", ".cache\n!**\n", []string{`rule "!**" may re-include`}},
		{"a re-include with a class", ".cache\n![.]cache\n", []string{`rule "![.]cache" may re-include`}},
		{"only a re-include", "!.cache\n", []string{`rule "!.cache" may re-include`, "has no rule"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := dockerignoreProblems(test.rules, inside)
			matched := len(got) == len(test.want)
			for i := 0; matched && i < len(got); i++ {
				matched = strings.Contains(got[i], test.want[i])
			}
			if !matched {
				t.Errorf("problems = %q, want one with each of %q", got, test.want)
			}
		})
	}
	// Boundary: a cache directly below the root is its own top directory.
	if got := dockerignoreProblems("cache\n", "cache"); !slices.Equal(got, nil) {
		t.Errorf("problems = %q, want none", got)
	}
}
