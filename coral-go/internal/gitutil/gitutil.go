// Package gitutil provides shared git helper functions.
// All functions gracefully handle the case where git is not installed
// or the directory is not a git repository.
package gitutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cdknorow/coral/internal/executil"
)

var (
	gitAvailable     bool
	gitAvailableOnce sync.Once
)

// Available returns true if git is installed and accessible on PATH.
func Available() bool {
	gitAvailableOnce.Do(func() {
		_, err := exec.LookPath("git")
		gitAvailable = err == nil
	})
	return gitAvailable
}

// git runs a git command in the given directory and returns trimmed stdout.
// Returns ("", err) if git is not available or the command fails.
func git(ctx context.Context, workdir string, args ...string) (string, error) {
	if !Available() {
		return "", exec.ErrNotFound
	}
	fullArgs := append([]string{"--no-optional-locks", "-C", workdir}, args...)
	out, err := executil.Command(ctx, "git", fullArgs...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ResolveGitRoot finds the git toplevel for a directory.
// If the directory itself isn't a git repo, checks one level of subdirectories.
// Returns the original workdir if no git repo is found.
func ResolveGitRoot(ctx context.Context, workdir string) string {
	if !Available() {
		return workdir
	}
	if root, err := git(ctx, workdir, "rev-parse", "--show-toplevel"); err == nil {
		return root
	}
	// workdir isn't a repo — check one level of subdirectories
	entries, _ := os.ReadDir(workdir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(workdir, e.Name())
		if root, err := git(ctx, sub, "rev-parse", "--show-toplevel"); err == nil {
			return root
		}
	}
	return workdir
}

// GetDiffBase returns the base ref for diffing.
//
// When mode is "previous_commit", returns "HEAD~1" to diff against the
// previous commit on the current branch. Falls back to "HEAD" if there
// is no previous commit.
//
// When mode is "branch_point" (or empty, the default), returns the
// merge-base with main/master on feature branches, or "HEAD" on
// main/master (uncommitted changes only).
func GetDiffBase(ctx context.Context, workdir, mode string) string {
	if !Available() {
		return "HEAD"
	}

	if mode == "previous_commit" {
		// Check that HEAD~1 exists (at least 2 commits)
		if _, err := git(ctx, workdir, "rev-parse", "--verify", "HEAD~1"); err == nil {
			return "HEAD~1"
		}
		return "HEAD"
	}

	if mode == "main_head" {
		// Diff against main/master branch HEAD directly
		for _, branch := range []string{"main", "master"} {
			if _, err := git(ctx, workdir, "rev-parse", "--verify", branch); err == nil {
				return branch
			}
		}
		return "HEAD"
	}

	// Default: branch_point (merge-base)
	branch, err := git(ctx, workdir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "HEAD"
	}
	if branch == "main" || branch == "master" || branch == "HEAD" || branch == "" {
		return "HEAD"
	}
	for _, baseBranch := range []string{"main", "master"} {
		if base, err := git(ctx, workdir, "merge-base", baseBranch, "HEAD"); err == nil && base != "" {
			return base
		}
	}
	return "HEAD"
}

// ParseRepoName extracts a short "org/repo" name from a git remote URL.
// Handles SSH (git@github.com:org/repo.git) and HTTPS (https://github.com/org/repo.git).
// Returns "" if the URL is empty or unparseable.
func ParseRepoName(remoteURL string) string {
	if remoteURL == "" {
		return ""
	}
	u := strings.TrimSuffix(remoteURL, ".git")

	// SSH: git@github.com:org/repo
	if strings.HasPrefix(u, "git@") {
		parts := strings.SplitN(u, ":", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}

	// HTTPS: https://github.com/org/repo (or any host)
	if idx := strings.LastIndex(u, "://"); idx >= 0 {
		path := u[idx+3:]
		if slashIdx := strings.Index(path, "/"); slashIdx >= 0 {
			return path[slashIdx+1:]
		}
	}

	return ""
}

// ShowPrefix returns the path prefix of the workdir within the repo (e.g. "subdir/").
// Returns "" if at the repo root or if git is not available.
func ShowPrefix(ctx context.Context, workdir string) string {
	if !Available() {
		return ""
	}
	prefix, _ := git(ctx, workdir, "rev-parse", "--show-prefix")
	return prefix
}
