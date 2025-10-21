package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	gh "untitled/domain/github"
)

// Minimal GitHub issues aggregator for an organization.
// Usage:
//   GITHUB_TOKEN=ghp_xxx go run . -org my-org [-since 2025-01-01] [-repo repoA,repoB] [-out issues.json]
// Notes:
// - Collects: issue state, labels (bug detection), creator, assignees, closed_by (as committer),
//   history of state changes (open/close/reopen), project column move history (classic projects),
//   and current project+column per classic project if still present.
// - Requires a Personal Access Token with repo/read:org and classic Projects access.

const (
	githubAPIBase    = "https://api.github.com"
	acceptDefault    = "application/vnd.github+json"
	acceptTimeline   = "application/vnd.github.mockingbird-preview+json" // timeline
	perPage          = 100
	rateSafetyMargin = 2 * time.Second
)

type Repo = gh.Repo

type Issue = gh.Issue

type User = gh.User

type Label = gh.Label

type TimelineEvent = gh.TimelineEvent

type ProjectCard = gh.ProjectCard

type ProjectColumn = gh.ProjectColumn

type Project = gh.Project

type StatusEvent = gh.StatusEvent

type ProjectMoveEvent = gh.ProjectMoveEvent

type IssueReport = gh.IssueReport

type CurrentProject = gh.CurrentProject

type httpClient struct {
	c     *http.Client
	token string
}

func (hc *httpClient) newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", strings.Join([]string{acceptDefault, acceptTimeline}, ", "))
	if hc.token != "" {
		req.Header.Set("Authorization", "Bearer "+hc.token)
	}
	return req, nil
}

func (hc *httpClient) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	for {
		resp, err := hc.c.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == 403 && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			reset := resp.Header.Get("X-RateLimit-Reset")
			_ = drainAndClose(resp.Body)
			if reset != "" {
				if sec, err := strconv.ParseInt(reset, 10, 64); err == nil {
					wait := time.Until(time.Unix(sec, 0)) + rateSafetyMargin
					if wait > 0 {
						slog.Warn("rate.limit.sleep", "wait", wait, "resetAt", time.Unix(sec, 0))
						fmt.Fprintf(os.Stderr, "Rate limit reached. Sleeping %s until reset...\n", wait)
						time.Sleep(wait)
					}
					continue
				}
			}
			return nil, errors.New("rate limited by GitHub API")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		// read body for diagnostics and return error
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("github API %s %s returned %d: %s", req.Method, req.URL.String(), resp.StatusCode, string(b))
	}
}

func drainAndClose(rc io.ReadCloser) error {
	_, _ = io.Copy(io.Discard, rc)
	return rc.Close()
}

func listAllRepos(ctx context.Context, hc *httpClient, org string) ([]Repo, error) {
	slog.Info("phase.repos.fetch.start", "org", org)
	var all []Repo
	for page := 1; ; page++ {
		u := fmt.Sprintf("%s/orgs/%s/repos?type=all&per_page=%d&page=%d", githubAPIBase, url.PathEscape(org), perPage, page)
		req, _ := hc.newRequest(ctx, http.MethodGet, u)
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var repos []Repo
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		if len(repos) == 0 {
			break
		}
		all = append(all, repos...)
	}
	slog.Info("phase.repos.fetch.done", "org", org, "repos", len(all))
	return all, nil
}

func listAllIssues(ctx context.Context, hc *httpClient, owner, repo, since string) ([]Issue, error) {
	slog.Info("phase.issues.fetch.start", "owner", owner, "repo", repo, "since", since)
	var all []Issue
	params := url.Values{}
	params.Set("state", "all")
	params.Set("per_page", strconv.Itoa(perPage))
	if since != "" {
		params.Set("since", since)
	}
	for page := 1; ; page++ {
		params.Set("page", strconv.Itoa(page))
		u := fmt.Sprintf("%s/repos/%s/%s/issues?%s", githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), params.Encode())
		req, _ := hc.newRequest(ctx, http.MethodGet, u)
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var issues []Issue
		if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		if len(issues) == 0 {
			break
		}
		all = append(all, issues...)
	}
	slog.Info("phase.issues.fetch.done", "owner", owner, "repo", repo, "count", len(all))
	return all, nil
}

func listAllTimeline(ctx context.Context, hc *httpClient, owner, repo string, number int) ([]TimelineEvent, error) {
	slog.Info("phase.timeline.fetch.start", "owner", owner, "repo", repo, "issue", number)
	var all []TimelineEvent
	for page := 1; ; page++ {
		u := fmt.Sprintf("%s/repos/%s/%s/issues/%d/timeline?per_page=%d&page=%d", githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), number, perPage, page)
		req, _ := hc.newRequest(ctx, http.MethodGet, u)
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var evs []TimelineEvent
		b, _ := io.ReadAll(resp.Body)
		slog.Debug("resp.Body", string(b))
		resp.Body = io.NopCloser(bytes.NewReader(b))
		if err := json.NewDecoder(resp.Body).Decode(&evs); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		if len(evs) == 0 {
			break
		}
		all = append(all, evs...)
	}
	slog.Info("phase.timeline.fetch.done", "owner", owner, "repo", repo, "issue", number, "events", len(all))
	return all, nil
}

// Cache for project columns and projects to minimize API calls

type columnCacheEntry struct {
	col ProjectColumn
	ok  bool
}

type projectCacheEntry struct {
	proj Project
	ok   bool
}

type resolver struct {
	hc       *httpClient
	colCache map[int64]columnCacheEntry
	prjCache map[int64]projectCacheEntry
}

func newResolver(hc *httpClient) *resolver {
	return &resolver{hc: hc, colCache: make(map[int64]columnCacheEntry), prjCache: make(map[int64]projectCacheEntry)}
}

func (r *resolver) getColumn(ctx context.Context, id int64) (ProjectColumn, bool, error) {
	if e, ok := r.colCache[id]; ok {
		slog.Debug("phase.resolve.column.cache", "id", id, "hit", true)
		return e.col, e.ok, nil
	}
	slog.Debug("phase.resolve.column.fetch.start", "id", id)
	u := fmt.Sprintf("%s/projects/columns/%d", githubAPIBase, id)
	req, _ := r.hc.newRequest(ctx, http.MethodGet, u)
	resp, err := r.hc.do(ctx, req)
	if err != nil {
		// cache miss as not ok
		r.colCache[id] = columnCacheEntry{ok: false}
		slog.Debug("phase.resolve.column.fetch.done", "id", id, "ok", false)
		return ProjectColumn{}, false, nil
	}
	var col ProjectColumn
	if err := json.NewDecoder(resp.Body).Decode(&col); err != nil {
		_ = resp.Body.Close()
		return ProjectColumn{}, false, err
	}
	_ = resp.Body.Close()
	r.colCache[id] = columnCacheEntry{col: col, ok: true}
	slog.Debug("phase.resolve.column.fetch.done", "id", id, "ok", true)
	return col, true, nil
}

func (r *resolver) getProject(ctx context.Context, id int64) (Project, bool, error) {
	if e, ok := r.prjCache[id]; ok {
		slog.Debug("phase.resolve.project.cache", "id", id, "hit", true)
		return e.proj, e.ok, nil
	}
	slog.Debug("phase.resolve.project.fetch.start", "id", id)
	u := fmt.Sprintf("%s/projects/%d", githubAPIBase, id)
	req, _ := r.hc.newRequest(ctx, http.MethodGet, u)
	resp, err := r.hc.do(ctx, req)
	if err != nil {
		r.prjCache[id] = projectCacheEntry{ok: false}
		slog.Debug("phase.resolve.project.fetch.done", "id", id, "ok", false)
		return Project{}, false, nil
	}
	var pr Project
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		_ = resp.Body.Close()
		return Project{}, false, err
	}
	_ = resp.Body.Close()
	r.prjCache[id] = projectCacheEntry{proj: pr, ok: true}
	slog.Debug("phase.resolve.project.fetch.done", "id", id, "ok", true)
	return pr, true, nil
}

func main() {
	org := flag.String("org", "", "GitHub organization (required)")
	since := flag.String("since", "", "Only issues updated since this ISO8601/RFC3339 time, e.g., 2025-01-01T00:00:00Z (optional)")
	repoFilter := flag.String("repo", "", "Comma-separated list of repositories to include (optional)")
	flag.Parse()

	// Initialize slog logger (text to stderr, INFO level by default)
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))

	if *org == "" {
		fmt.Fprintln(os.Stderr, "-org is required")
		slog.Error("import.validation.error", "reason", "missing org")
		os.Exit(2)
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "GITHUB_TOKEN environment variable is required.")
		slog.Error("import.validation.error", "reason", "missing GITHUB_TOKEN")
		os.Exit(2)
	}

	slog.Info("import.start", "org", *org, "since", *since, "repoFilter", *repoFilter)

	ctx := context.Background()
	hc := &httpClient{c: &http.Client{Timeout: 30 * time.Second}, token: token}
	res := newResolver(hc)

	allowedRepos := map[string]bool{}
	if *repoFilter != "" {
		for _, r := range strings.Split(*repoFilter, ",") {
			allowedRepos[strings.TrimSpace(r)] = true
		}
	}

	repos, err := listAllRepos(ctx, hc, *org)
	if err != nil {
		slog.Error("phase.repos.fetch.error", "org", *org, "error", err)
		fmt.Fprintf(os.Stderr, "error listing repos: %v\n", err)
		os.Exit(1)
	}

	var reports []IssueReport
	for _, r := range repos {
		if *repoFilter != "" && !allowedRepos[r.Name] {
			continue
		}
		slog.Info("phase.issues.import.start", "owner", r.Owner.Login, "repo", r.Name, "since", *since)
		issues, err := listAllIssues(ctx, hc, r.Owner.Login, r.Name, *since)
		if err != nil {
			slog.Error("phase.issues.fetch.error", "owner", r.Owner.Login, "repo", r.Name, "error", err)
			fmt.Fprintf(os.Stderr, "error listing issues for %s/%s: %v\n", r.Owner.Login, r.Name, err)
			continue
		}
		slog.Info("phase.issues.import.fetched", "owner", r.Owner.Login, "repo", r.Name, "count", len(issues))
		for _, is := range issues {
			// Skip PRs
			if is.PullRequest != nil {
				continue
			}
			// timeline aggregation below
			report := IssueReport{
				Org:       *org,
				Repo:      r.Name,
				Number:    is.Number,
				Title:     is.Title,
				URL:       is.HTMLURL,
				State:     is.State,
				Creator:   valueOrEmpty(is.User),
				Assignees: usersToLogins(is.Assignees),
				CreatedAt: is.CreatedAt,
				ClosedAt:  is.ClosedAt,
			}
			for _, l := range is.Labels {
				if strings.EqualFold(l.Name, "bug") {
					report.IsBug = true
					break
				}
			}

			// Timeline aggregation
			evts, err := listAllTimeline(ctx, hc, r.Owner.Login, r.Name, is.Number)
			if err != nil {
				slog.Warn("phase.timeline.fetch.error", "owner", r.Owner.Login, "repo", r.Name, "issue", is.Number, "error", err)
				fmt.Fprintf(os.Stderr, "warning: timeline fetch failed for %s/%s#%d: %v\n", r.Owner.Login, r.Name, is.Number, err)
			} else {
				statusHist := make([]StatusEvent, 0, 4)
				projHist := make([]ProjectMoveEvent, 0, 8)
				// seed opened
				statusHist = append(statusHist, StatusEvent{Type: "opened", At: is.CreatedAt, By: valueOrEmpty(is.User)})
				// Track current per project
				type current struct {
					present     bool
					projectID   int64
					projectName string
					columnID    int64
					columnName  string
				}
				currentByProject := map[int64]*current{}

				for _, ev := range evts {
					slog.Debug(ev.Event)
					switch ev.Event {
					case "closed":
						statusHist = append(statusHist, StatusEvent{Type: "closed", At: ev.CreatedAt, By: valueOrEmpty(ev.Actor)})
						// set committer as the actor who closed
						if report.Committer == "" && ev.Actor != nil {
							report.Committer = ev.Actor.Login
						}
					case "reopened":
						statusHist = append(statusHist, StatusEvent{Type: "reopened", At: ev.CreatedAt, By: valueOrEmpty(ev.Actor)})
					case "added_to_project_v2":
						var projID int64
						var projName string
						var colID int64
						var colName string
						if ev.Project != nil {
							projID = ev.Project.ID
							projName = ev.Project.Name
						}
						if ev.ProjectCard != nil && ev.ProjectCard.ColumnID != 0 {
							colID = ev.ProjectCard.ColumnID
							if col, ok, _ := res.getColumn(ctx, colID); ok {
								colName = col.Name
								if projID == 0 && col.ProjectID != 0 {
									projID = col.ProjectID
								}
							}
						}
						if projID != 0 {
							if projName == "" {
								if pr, ok, _ := res.getProject(ctx, projID); ok {
									projName = pr.Name
								}
							}
							projHist = append(projHist, ProjectMoveEvent{ProjectID: projID, ProjectName: projName, FromColumn: "", ToColumn: firstNonEmpty(ev.ProjectColumnName, colName), At: ev.CreatedAt, By: valueOrEmpty(ev.Actor), Type: "added"})
							c := &current{present: true, projectID: projID, projectName: projName, columnID: colID, columnName: firstNonEmpty(ev.ProjectColumnName, colName)}
							currentByProject[projID] = c
						}
					case "project_v2_item_status_changed":
						var projID int64
						var projName string
						var colNameTo = ev.ProjectColumnName
						var colNameFrom = ev.PreviousProjectColumnName
						if ev.ProjectCard != nil && ev.ProjectCard.ColumnID != 0 {
							if col, ok, _ := res.getColumn(ctx, ev.ProjectCard.ColumnID); ok {
								projID = col.ProjectID
								if colNameTo == "" {
									colNameTo = col.Name
								}
								if pr, ok, _ := res.getProject(ctx, projID); ok {
									projName = pr.Name
								}
							}
						}
						if projID != 0 {
							projHist = append(projHist, ProjectMoveEvent{ProjectID: projID, ProjectName: projName, FromColumn: colNameFrom, ToColumn: colNameTo, At: ev.CreatedAt, By: valueOrEmpty(ev.Actor), Type: "moved"})
							c := currentByProject[projID]
							if c == nil {
								c = &current{present: true, projectID: projID, projectName: projName}
								currentByProject[projID] = c
							}
							c.present = true
							c.projectName = projName
							c.columnName = colNameTo
						}
					case "removed_from_project_v2":
						var projID int64
						var projName string
						if ev.ProjectCard != nil && ev.ProjectCard.ColumnID != 0 {
							if col, ok, _ := res.getColumn(ctx, ev.ProjectCard.ColumnID); ok {
								projID = col.ProjectID
								if pr, ok, _ := res.getProject(ctx, projID); ok {
									projName = pr.Name
								}
							}
						}
						if projID != 0 {
							projHist = append(projHist, ProjectMoveEvent{ProjectID: projID, ProjectName: projName, FromColumn: "", ToColumn: "", At: ev.CreatedAt, By: valueOrEmpty(ev.Actor), Type: "removed"})
							c := currentByProject[projID]
							if c == nil {
								c = &current{projectID: projID, projectName: projName}
								currentByProject[projID] = c
							}
							c.present = false
						}
					}
				}

				report.StatusHistory = statusHist
				report.ProjectHistory = projHist
				for pid, cur := range currentByProject {
					if cur.present {
						report.CurrentProjects = append(report.CurrentProjects, CurrentProject{ProjectID: pid, ProjectName: cur.projectName, ColumnID: cur.columnID, ColumnName: cur.columnName})
					}
				}
			}

			reports = append(reports, report)
		}
	}

	// Write CSV outputs into data/ directory
	if err := writeAllCSVs(*org, repos, reports); err != nil {
		slog.Error("phase.csv.write.error", "error", err)
		fmt.Fprintf(os.Stderr, "failed to write CSV outputs: %v\n", err)
		// do not exit; JSON already written
	}
	slog.Info("import.done", "reports", len(reports))
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func writeAllCSVs(org string, repos []Repo, reports []IssueReport) error {
	dir := filepath.Join("data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeRepositoryCSV(filepath.Join(dir, "repository.csv"), org, repos); err != nil {
		return err
	}
	if err := writeProjectCSV(filepath.Join(dir, "project.csv"), reports); err != nil {
		return err
	}
	if err := writeIssueCSV(filepath.Join(dir, "issue.csv"), reports); err != nil {
		return err
	}
	if err := writeIssueStatusCSV(filepath.Join(dir, "issue_status_event.csv"), reports); err != nil {
		return err
	}
	if err := writeIssueProjectCSV(filepath.Join(dir, "issue_project_event.csv"), reports); err != nil {
		return err
	}
	return nil
}

func writeRepositoryCSV(path string, org string, repos []Repo) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"org", "repo", "owner", "private"}); err != nil {
		return err
	}
	for _, r := range repos {
		row := []string{org, r.Name, r.Owner.Login, strconv.FormatBool(r.Private)}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeProjectCSV(path string, reports []IssueReport) error {
	// collect unique projects by ID
	projects := map[int64]string{}
	for _, rep := range reports {
		for _, ev := range rep.ProjectHistory {
			if ev.ProjectID != 0 {
				if ev.ProjectName != "" {
					projects[ev.ProjectID] = ev.ProjectName
				} else if _, ok := projects[ev.ProjectID]; !ok {
					projects[ev.ProjectID] = ""
				}
			}
		}
		for _, cur := range rep.CurrentProjects {
			if cur.ProjectID != 0 {
				if cur.ProjectName != "" {
					projects[cur.ProjectID] = cur.ProjectName
				} else if _, ok := projects[cur.ProjectID]; !ok {
					projects[cur.ProjectID] = ""
				}
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"project_id", "project_name"}); err != nil {
		return err
	}
	for id, name := range projects {
		row := []string{strconv.FormatInt(id, 10), name}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeIssueCSV(path string, reports []IssueReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	headers := []string{"org", "repo", "number", "title", "url", "state", "is_bug", "creator", "assignees", "created_at", "closed_at", "committer"}
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, rep := range reports {
		assignees := strings.Join(rep.Assignees, ";")
		created := rep.CreatedAt.UTC().Format(time.RFC3339)
		closed := ""
		if rep.ClosedAt != nil {
			closed = rep.ClosedAt.UTC().Format(time.RFC3339)
		}
		row := []string{
			rep.Org,
			rep.Repo,
			strconv.Itoa(rep.Number),
			rep.Title,
			rep.URL,
			rep.State,
			strconv.FormatBool(rep.IsBug),
			rep.Creator,
			assignees,
			created,
			closed,
			rep.Committer,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func writeIssueStatusCSV(path string, reports []IssueReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"org", "repo", "number", "type", "at", "by"}); err != nil {
		return err
	}
	for _, rep := range reports {
		for _, ev := range rep.StatusHistory {
			row := []string{
				rep.Org,
				rep.Repo,
				strconv.Itoa(rep.Number),
				ev.Type,
				ev.At.UTC().Format(time.RFC3339),
				ev.By,
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}
	return w.Error()
}

func writeIssueProjectCSV(path string, reports []IssueReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	headers := []string{"org", "repo", "number", "project_id", "project_name", "from_column", "to_column", "at", "by", "type"}
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, rep := range reports {
		for _, ev := range rep.ProjectHistory {
			row := []string{
				rep.Org,
				rep.Repo,
				strconv.Itoa(rep.Number),
				strconv.FormatInt(ev.ProjectID, 10),
				ev.ProjectName,
				ev.FromColumn,
				ev.ToColumn,
				ev.At.UTC().Format(time.RFC3339),
				ev.By,
				ev.Type,
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}
	return w.Error()
}
