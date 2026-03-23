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
	fullArgs := append([]string{"-C", workdir}, args...)
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

// GetDiffBase returns the merge-base ref for diffing on feature branches.
// On main/master it returns "HEAD" (uncommitted changes only).
// On feature branches it returns the merge-base with main/master.
func GetDiffBase(ctx context.Context, workdir string) string {
	if !Available() {
		return "HEAD"
	}
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

// ShowPrefix returns the path prefix of the workdir within the repo (e.g. "subdir/").
// Returns "" if at the repo root or if git is not available.
func ShowPrefix(ctx context.Context, workdir string) string {
	if !Available() {
		return ""
	}
	prefix, _ := git(ctx, workdir, "rev-parse", "--show-prefix")
	return prefix
}

// IsRepo returns true if the directory is inside a git repository.
func IsRepo(ctx context.Context, workdir string) bool {
	if !Available() {
		return false
	}
	_, err := git(ctx, workdir, "rev-parse", "--git-dir")
	return err == nil
}
