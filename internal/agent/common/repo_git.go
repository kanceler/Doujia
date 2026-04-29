package common

import (
	"context"
	"fmt"
	"strings"
)

func GitCheckout(ctx context.Context, repoDir string, branch string) error {
	if strings.TrimSpace(repoDir) == "" {
		return fmt.Errorf("repo dir is required")
	}
	if strings.TrimSpace(branch) == "" {
		return fmt.Errorf("branch is required")
	}
	_, err := runGit(ctx, repoDir, "checkout", branch)
	return err
}

func GitRevParseHEAD(ctx context.Context, repoDir string) (string, error) {
	result, err := runGit(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func GitIsClean(ctx context.Context, repoDir string) (bool, error) {
	result, err := runGit(ctx, repoDir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.TrimSpace(line) != "" {
			return false, nil
		}
	}
	return true, nil
}

func GitCherryPickRange(ctx context.Context, repoDir string, baseCommit string, headCommit string) error {
	baseCommit = strings.TrimSpace(baseCommit)
	headCommit = strings.TrimSpace(headCommit)
	if baseCommit == "" {
		return fmt.Errorf("base commit is required")
	}
	if headCommit == "" {
		return fmt.Errorf("head commit is required")
	}
	if baseCommit == headCommit {
		return nil
	}
	_, err := runGit(ctx, repoDir, "cherry-pick", baseCommit+".."+headCommit)
	return err
}

func GitDiffNameOnly(ctx context.Context, repoDir, baseCommit, headCommit string, paths ...string) ([]string, error) {
	baseCommit = strings.TrimSpace(baseCommit)
	headCommit = strings.TrimSpace(headCommit)
	if baseCommit == "" {
		return nil, fmt.Errorf("base commit is required")
	}
	if headCommit == "" {
		return nil, fmt.Errorf("head commit is required")
	}
	args := []string{"diff", "--name-only", baseCommit + ".." + headCommit}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	result, err := runGit(ctx, repoDir, args...)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func GitCherryPickAbort(ctx context.Context, repoDir string) error {
	_, err := runGit(ctx, repoDir, "cherry-pick", "--abort")
	return err
}

func GitResetHard(ctx context.Context, repoDir string, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("reset ref is required")
	}
	_, err := runGit(ctx, repoDir, "reset", "--hard", ref)
	return err
}
