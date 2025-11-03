package main

import (
	cmdcalculate "cto-stats/command/calculate"
	cmdimport "cto-stats/command/import"
	cmdweb "cto-stats/command/web"
	gh "cto-stats/domain/github"
	"fmt"
	"log/slog"
	"os"
)

// Minimal GitHub issues aggregator for an organization.
// Usage:
//   GITHUB_TOKEN=ghp_xxx go run . -org my-org [-since 2025-01-01] [-repo repoA,repoB] [-out issues.json]
// Notes:
// - Collects: issue state, labels (bug detection), creator, assignees, closed_by (as committer),
//   history of state changes (open/close/reopen), project column move history (classic projects),
//   and current project+column per classic project if still present.
// - Requires a Personal Access Token with repo/read:org and classic Projects access.

type Repo = gh.Repo

type Issue = gh.Issue

type User = gh.User

type Label = gh.Label

type TimelineEvent = gh.TimelineEvent

type Project = gh.Project

type StatusEvent = gh.StatusEvent

type ProjectMoveEvent = gh.ProjectMoveEvent

type IssueReport = gh.IssueReport

type CurrentProject = gh.CurrentProject

func main() {
	args := os.Args
	// Initialize slog logger (text to stderr, DEBUG level for now)
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(h))

	if len(args) > 1 {
		sub := args[1]
		rest := append([]string{}, args[2:]...)
		switch sub {
		case "import":
			if err := cmdimport.Run(rest); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "calculate":
			if err := cmdcalculate.Run(rest); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "web":
			if err := cmdweb.Run(rest); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintln(os.Stderr, "usage: github-stats import -org <org> [-since <ts>] [-repo <list>] | calculate | web [-addr :8080] [-data ./data]\nENV: set CONFIG_PATH to point to a YAML config file (default ./config.yml)")
	os.Exit(2)
}

func valueOrEmpty(u *User) string {
	if u == nil {
		return ""
	}
	return u.Login
}

func usersToLogins(us []User) []string {
	res := make([]string, 0, len(us))
	for _, u := range us {
		if u.Login != "" {
			res = append(res, u.Login)
		}
	}
	return res
}
