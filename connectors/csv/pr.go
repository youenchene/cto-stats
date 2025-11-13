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
func WritePullRequestCSV(path string, org string, repo string, prs []gh.PullRequest) error {
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
		row := []string{org, repo, strconv.Itoa(pr.Number), pr.Title, pr.HTMLURL, pr.State, created, closed, merged, creator}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// AppendPullRequests appends a slice of PRs to a single CSV, writing header if the file does not exist.
// Headers: org, repo, number, title, url, state, created_at, closed_at, merged_at, creator
func AppendPullRequests(path string, org string, repo string, prs []gh.PullRequest) error {
	if len(prs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var f *os.File
	var err error
	if _, statErr := os.Stat(path); statErr == nil {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
	} else {
		f, err = os.Create(path)
		if err != nil {
			return err
		}
		w := csv.NewWriter(f)
		if err := w.Write([]string{"org", "repo", "number", "title", "url", "state", "created_at", "closed_at", "merged_at", "creator"}); err != nil {
			_ = f.Close()
			return err
		}
		w.Flush()
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
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
		row := []string{org, repo, strconv.Itoa(pr.Number), pr.Title, pr.HTMLURL, pr.State, created, closed, merged, creator}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// AppendPullRequestReviews appends a batch of reviews for a PR to a CSV file.
// Headers: org, repo, number, state, submitted_at, user
func AppendPullRequestReviews(path string, org string, repo string, number int, reviews []gh.PullRequestReview) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// open file and write header if creating
	var f *os.File
	var err error
	if _, statErr := os.Stat(path); statErr == nil {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
	} else {
		f, err = os.Create(path)
		if err != nil {
			return err
		}
		w := csv.NewWriter(f)
		if err := w.Write([]string{"org", "repo", "number", "state", "submitted_at", "user"}); err != nil {
			_ = f.Close()
			return err
		}
		w.Flush()
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	for _, rv := range reviews {
		sub := rv.SubmittedAt.UTC().Format(time.RFC3339)
		user := ""
		if rv.User != nil {
			user = rv.User.Login
		}
		row := []string{org, repo, strconv.Itoa(number), rv.State, sub, user}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}
