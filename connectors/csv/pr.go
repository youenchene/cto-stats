package csv

import (
	gh "cto-stats/domain/github"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// WritePullRequestCSV writes a complete CSV snapshot of PRs for a repository.
// Headers: org, repo, number, title, url, state, created_at, closed_at, merged_at, creator
func WritePullRequests(path string, prs []gh.PullRequest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"org", "repo", "number", "title", "url", "state", "created_at", "closed_at", "merged_at", "creator"}); err != nil {
		return err
	}
	for _, pr := range prs {
		created := pr.CreatedAt.UTC().Format(time.RFC3339)
		closed := ""
		if pr.ClosedAt != nil {
			closed = pr.ClosedAt.UTC().Format(time.RFC3339)
		}
		merged := ""
		if pr.MergedAt != nil {
			merged = pr.MergedAt.UTC().Format(time.RFC3339)
		}
		creator := ""
		if pr.User != nil {
			creator = pr.User.Login
		}
		row := []string{pr.Org, pr.Repo, strconv.Itoa(pr.Number), pr.Title, pr.HTMLURL, pr.State, created, closed, merged, creator}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func WritePullRequestReviews(path string, reviews []gh.PullRequestReview) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"org", "repo", "number", "state", "submitted_at", "user"}); err != nil {
		return err
	}
	for _, rv := range reviews {
		sub := rv.SubmittedAt.UTC().Format(time.RFC3339)
		user := ""
		if rv.User != nil {
			user = rv.User.Login
		}
		row := []string{rv.Org, rv.Repo, strconv.Itoa(rv.PullRequestNumber), rv.State, sub, user}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}
