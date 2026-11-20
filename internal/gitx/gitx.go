// Package gitx asks git which repository a folder is in. It only reads.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CommonDir is the full path of the common git folder of the repository dir
// is in, links resolved: the same for every worktree of one repository, and
// different for two repositories whose folders share a name. It fails
// outside a git repository.
func CommonDir(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	// The answer is about dir, whatever repository the caller's own
	// environment points git at.
	cmd.Env = withoutGitVars(os.Environ())
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git rev-parse in %s: %s", dir, msg)
	}
	common := strings.TrimSpace(out.String())
	if common == "" || !filepath.IsAbs(common) {
		return "", errors.New("git gave no common git folder")
	}
	if real, err := filepath.EvalSymlinks(common); err == nil {
		common = real
	}
	return filepath.Clean(common), nil
}

// MainRoot is the main worktree's folder for a common git folder, where
// handoff keeps .handoff/, or "" for a bare repository.
func MainRoot(common string) string {
	if filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}

func withoutGitVars(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_CEILING_DIRECTORIES":
			continue
		}
		out = append(out, kv)
	}
	return out
}
