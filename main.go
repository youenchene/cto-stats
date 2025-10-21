package main

import (
	cmdcalculate "cto-stats/command/calculate"
	cmdimport "cto-stats/command/import"
	gh "cto-stats/domain/github"
	"fmt"
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
	if len(args) > 1 {
		switch args[1] {
		case "import":
			if err := cmdimport.Run(args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "calculate":
			if err := cmdcalculate.Run(args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintln(os.Stderr, "usage: github-stats import -org <org> [-since <ts>] [-repo <list>] | calculate")
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
