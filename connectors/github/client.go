package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	gh "cto-stats/domain/github"
)

// Package github provides a minimal GitHub connector used by the collector.
// It exposes high-level listing functions backed by GitHub GraphQL API and
// handles rate limiting and auth.

const (
	githubAPIBase         = "https://api.github.com"
	githubGraphQLEndpoint = "https://api.github.com/graphql"
	acceptDefault         = "application/vnd.github+json"
	acceptTimeline        = "application/vnd.github.mockingbird-preview+json"
	perPage               = 100
	rateSafetyMargin      = 2 * time.Second
)

// Client is a thin wrapper over http.Client with token auth and helper methods.
// Use New to construct it.

type Client struct {
	c     *http.Client
	token string
}

func New(c *http.Client, token string) *Client {
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{c: c, token: token}
}

func (hc *Client) newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
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

func (hc *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
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
						fmt.Fprintf(io.Discard, "Rate limit reached. Sleeping %s until reset...\n", wait)
						time.Sleep(wait)
					}
					continue
				}
			}
			return nil, errors.New("rate limited by GitHub API")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Simple rate-aware pacing without concurrency: after each successful response,
			// inspect X-RateLimit headers and optionally sleep to avoid hitting the cap.
			remainingStr := resp.Header.Get("X-RateLimit-Remaining")
			resetStr := resp.Header.Get("X-RateLimit-Reset")
			if remainingStr != "" && resetStr != "" {
				if rem, err1 := strconv.Atoi(remainingStr); err1 == nil {
					if sec, err2 := strconv.ParseInt(resetStr, 10, 64); err2 == nil {
						resetAt := time.Unix(sec, 0)
						if rem <= 0 {
							// Out of requests in this window; wait until reset.
							sleep := time.Until(resetAt) + rateSafetyMargin
							if sleep > 0 {
								slog.Warn("rate.pacing.sleep.empty", "sleep", sleep, "resetAt", resetAt)
								time.Sleep(sleep)
							}
						} else if rem < 100 {
							// Low budget remaining; spread remaining calls evenly until reset.
							// Compute a small delay = remaining window time / remaining requests, plus tiny jitter.
							window := time.Until(resetAt)
							if window > 0 {
								perReq := window / time.Duration(rem+1)
								// Cap to a reasonable max to avoid overly long sleeps on long windows.
								if perReq > 2*time.Second {
									perReq = 2 * time.Second
								}
								// Add small jitter up to 100ms to de-sync if multiple processes are running.
								jitter := time.Duration(time.Now().UnixNano() % int64(100*time.Millisecond))
								sleep := perReq + jitter/10
								if sleep > 0 {
									slog.Info("rate.pacing.sleep", "sleep", sleep, "remaining", rem, "resetAt", resetAt)
									time.Sleep(sleep)
								}
							}
						}
					}
				}
			}
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

// sleepUntilResetIfRateLimited checks GraphQL error messages for rate limit hints
// and sleeps until the reset time advertised by GitHub headers. The sleep time
// is capped to 1 hour as requested. Returns true if it slept and caller should retry.
func sleepUntilResetIfRateLimited(resp *http.Response, messages []string) bool {
	if resp == nil {
		return false
	}
	rateLimited := false
	for _, m := range messages {
		lm := strings.ToLower(m)
		if strings.Contains(lm, "rate limit") || strings.Contains(lm, "api rate limit exceeded") {
			rateLimited = true
			break
		}
	}
	if !rateLimited {
		return false
	}
	// Default wait to 1h cap unless header gives a nearer reset
	wait := 1 * time.Hour
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if sec, err := strconv.ParseInt(reset, 10, 64); err == nil {
			resetAt := time.Unix(sec, 0)
			until := time.Until(resetAt) + rateSafetyMargin
			if until > 0 && until < wait {
				wait = until
			}
			if until <= 0 {
				wait = 5 * time.Second
			}
		}
	}
	slog.Warn("graphql.rate.limit.sleep", "sleep", wait, "resetAt", resp.Header.Get("X-RateLimit-Reset"))
	time.Sleep(wait)
	return true
}

// ListAllPullRequests lists PRs for a repo, optionally filtered by created since (ISO8601 string). Uses GraphQL.
func (hc *Client) ListAllPullRequests(ctx context.Context, owner, repo, since string) ([]gh.PullRequest, error) {
	slog.Info("phase.prs.fetch.start", "owner", owner, "repo", repo, "since", since)
	var all []gh.PullRequest
	query := `query($owner:String!, $name:String!, $pageSize:Int!, $after:String){
  repository(owner:$owner, name:$name){
    pullRequests(first:$pageSize, after:$after, orderBy:{field:UPDATED_AT, direction:ASC}, states:[OPEN, MERGED, CLOSED]){
      pageInfo{hasNextPage endCursor}
      nodes{
        number
        title
        state
        url
        createdAt
        updatedAt
        closedAt
        mergedAt
        author{login}
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "pageSize": perPage}
	for {
		body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLEndpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+hc.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var out struct {
			Data struct {
				Repository struct {
					PullRequests struct {
						PageInfo struct {
							HasNextPage bool    `json:"hasNextPage"`
							EndCursor   *string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							Number    int        `json:"number"`
							Title     string     `json:"title"`
							State     string     `json:"state"`
							URL       string     `json:"url"`
							CreatedAt time.Time  `json:"createdAt"`
							UpdatedAt time.Time  `json:"updatedAt"`
							ClosedAt  *time.Time `json:"closedAt"`
							MergedAt  *time.Time `json:"mergedAt"`
							Author    *struct {
								Login string `json:"login"`
							} `json:"author"`
						} `json:"nodes"`
					} `json:"pullRequests"`
				} `json:"repository"`
			} `json:"data"`
			Errors []struct{ Message string } `json:"errors"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		if len(out.Errors) > 0 {
			// Handle GraphQL rate limit (HTTP 200 + errors)
			msgs := make([]string, 0, len(out.Errors))
			for _, e := range out.Errors {
				msgs = append(msgs, e.Message)
			}
			if sleepUntilResetIfRateLimited(resp, msgs) {
				_ = resp.Body.Close()
				// retry same page after sleep
				continue
			}
			return nil, fmt.Errorf("graphql: %s", out.Errors[0].Message)
		}
		for _, n := range out.Data.Repository.PullRequests.Nodes {
			pr := gh.PullRequest{
				Number:    n.Number,
				Title:     n.Title,
				State:     strings.ToLower(strings.TrimSpace(n.State)),
				HTMLURL:   n.URL,
				CreatedAt: n.CreatedAt,
				UpdatedAt: n.UpdatedAt,
				ClosedAt:  n.ClosedAt,
				MergedAt:  n.MergedAt,
			}
			if n.Author != nil {
				pr.User = &gh.User{Login: n.Author.Login}
			}
			// Optional client-side filter by createdAt >= since
			if since != "" {
				if t, err := time.Parse(time.RFC3339, since); err == nil {
					if pr.CreatedAt.Before(t) {
						continue
					}
				}
			}
			all = append(all, pr)
		}
		pi := out.Data.Repository.PullRequests.PageInfo
		if !pi.HasNextPage || pi.EndCursor == nil {
			_ = resp.Body.Close()
			break
		}
		vars["after"] = *pi.EndCursor
		_ = resp.Body.Close()
	}
	slog.Info("phase.prs.fetch.done", "owner", owner, "repo", repo, "count", len(all))
	return all, nil
}

// ListAllPullRequestReviews lists reviews for a given PR number via REST API.
func (hc *Client) ListAllPullRequestReviews(ctx context.Context, owner, repo string, number int) ([]gh.PullRequestReview, error) {
	slog.Info("phase.pr.reviews.fetch.start", "owner", owner, "repo", repo, "pr", number)
	var all []gh.PullRequestReview
	page := 1
	for {
		url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews?per_page=%d&page=%d", githubAPIBase, owner, repo, number, perPage, page)
		req, err := hc.newRequest(ctx, http.MethodGet, url)
		if err != nil {
			return nil, err
		}
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var out []struct {
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submitted_at"`
			User        *struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		for _, r := range out {
			rev := gh.PullRequestReview{State: strings.ToUpper(strings.TrimSpace(r.State)), SubmittedAt: r.SubmittedAt}
			if r.User != nil {
				rev.User = &gh.User{Login: r.User.Login}
			}
			all = append(all, rev)
		}
		if len(out) < perPage {
			break
		}
		page++
	}
	slog.Info("phase.pr.reviews.fetch.done", "owner", owner, "repo", repo, "pr", number, "count", len(all))
	return all, nil
}

// ListAllRepos lists all repositories for the given organization.
func (hc *Client) ListAllRepos(ctx context.Context, org string) ([]gh.Repo, error) {
	slog.Info("phase.repos.fetch.start", "org", org)
	var all []gh.Repo
	query := `query($login:String!, $pageSize:Int!, $after:String){
  organization(login:$login){
    repositories(first:$pageSize, after:$after, orderBy:{field: NAME, direction: ASC}){
      pageInfo{hasNextPage endCursor}
      nodes{
        name
        isPrivate
        owner{login}
      }
    }
  }
}`
	vars := map[string]any{"login": org, "pageSize": perPage}
	for {
		// build request
		body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLEndpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+hc.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var out struct {
			Data struct {
				Organization struct {
					Repositories struct {
						PageInfo struct {
							HasNextPage bool    `json:"hasNextPage"`
							EndCursor   *string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							Name      string `json:"name"`
							IsPrivate bool   `json:"isPrivate"`
							Owner     struct {
								Login string `json:"login"`
							} `json:"owner"`
						} `json:"nodes"`
					} `json:"repositories"`
				} `json:"organization"`
			} `json:"data"`
			Errors []struct{ Message string } `json:"errors"`
		}
		// Read body content for logging
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		slog.Debug("phase.timeline.fetch.response", "body", string(bodyBytes))
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&out); err != nil {
			return nil, err
		}
		// Handle GraphQL errors possibly indicating a rate limit
		if len(out.Errors) > 0 {
			msgs := make([]string, 0, len(out.Errors))
			for _, e := range out.Errors {
				msgs = append(msgs, e.Message)
			}
			if sleepUntilResetIfRateLimited(resp, msgs) {
				_ = resp.Body.Close()
				// retry same page after sleeping
				continue
			}
			_ = resp.Body.Close()
			return nil, fmt.Errorf("graphql: %s", out.Errors[0].Message)
		}
		_ = resp.Body.Close()
		for _, n := range out.Data.Organization.Repositories.Nodes {
			all = append(all, gh.Repo{Name: n.Name, Private: n.IsPrivate, Owner: struct {
				Login string `json:"login"`
			}{Login: n.Owner.Login}})
		}
		pi := out.Data.Organization.Repositories.PageInfo
		if !pi.HasNextPage || pi.EndCursor == nil {
			break
		}
		vars["after"] = *pi.EndCursor
	}
	slog.Info("phase.repos.fetch.done", "org", org, "repos", len(all))
	return all, nil
}

// ListAllIssues lists all issues for a repo, optionally since a time.
// ListAllIssues lists all issues for a repo, optionally since a time and starting after a given cursor.
// It returns the collected issues and the last endCursor so callers can persist checkpoints.
func (hc *Client) ListAllIssues(ctx context.Context, owner, repo, since string, after string) ([]gh.Issue, *string, error) {
	slog.Info("phase.issues.fetch.start", "owner", owner, "repo", repo, "since", since)
	var all []gh.Issue
	query := `query($owner:String!, $name:String!, $pageSize:Int!, $after:String, $since:DateTime){
  repository(owner:$owner, name:$name){
    issues(first:$pageSize, after:$after, orderBy:{field:UPDATED_AT, direction:ASC}, states:[OPEN, CLOSED], filterBy:{since:$since}){
      pageInfo{hasNextPage endCursor}
      nodes{
        number
        title
        state
        url
        createdAt
        updatedAt
        closedAt
        author{login}
        assignees(first:20){nodes{login}}
        labels(first:50){nodes{name}}
        issueType { name }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "pageSize": perPage}
	if since != "" {
		vars["since"] = since
	}
	if strings.TrimSpace(after) != "" {
		vars["after"] = after
	}
	var lastCursor *string
	for {
		body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLEndpoint, bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+hc.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		var out struct {
			Data struct {
				Repository struct {
					Issues struct {
						PageInfo struct {
							HasNextPage bool    `json:"hasNextPage"`
							EndCursor   *string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							Number    int        `json:"number"`
							Title     string     `json:"title"`
							State     string     `json:"state"`
							URL       string     `json:"url"`
							CreatedAt time.Time  `json:"createdAt"`
							UpdatedAt time.Time  `json:"updatedAt"`
							ClosedAt  *time.Time `json:"closedAt"`
							Author    *struct {
								Login string `json:"login"`
							} `json:"author"`
							Assignees struct {
								Nodes []struct {
									Login string `json:"login"`
								}
							} `json:"assignees"`
							Labels struct {
								Nodes []struct {
									Name string `json:"name"`
								}
							} `json:"labels"`
							IssueType *struct {
								Name string `json:"name"`
							} `json:"issueType"`
						} `json:"nodes"`
					} `json:"issues"`
				} `json:"repository"`
			} `json:"data"`
			Errors []struct{ Message string } `json:"errors"`
		}
		// Decode directly from body
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, nil, err
		}
		if len(out.Errors) > 0 {
			msgs := make([]string, 0, len(out.Errors))
			for _, e := range out.Errors {
				msgs = append(msgs, e.Message)
			}
			if sleepUntilResetIfRateLimited(resp, msgs) {
				_ = resp.Body.Close()
				// retry same page after sleeping
				continue
			}
			return nil, nil, fmt.Errorf("graphql: %s", out.Errors[0].Message)
		}
		for _, n := range out.Data.Repository.Issues.Nodes {
			iss := gh.Issue{
				Number:    n.Number,
				Title:     n.Title,
				State:     strings.ToLower(n.State),
				HTMLURL:   n.URL,
				CreatedAt: n.CreatedAt,
				UpdatedAt: n.UpdatedAt,
				ClosedAt:  n.ClosedAt,
			}
			if n.Author != nil {
				iss.User = &gh.User{Login: n.Author.Login}
			}
			for _, a := range n.Assignees.Nodes {
				iss.Assignees = append(iss.Assignees, gh.User{Login: a.Login})
			}
			for _, l := range n.Labels.Nodes {
				iss.Labels = append(iss.Labels, gh.Label{Name: l.Name})
			}
			if n.IssueType != nil {
				iss.Type = strings.ToLower(strings.TrimSpace(n.IssueType.Name))
			}
			all = append(all, iss)
		}
		pi := out.Data.Repository.Issues.PageInfo
		if pi.EndCursor != nil {
			// remember the most recent cursor seen
			lastCursor = new(string)
			*lastCursor = *pi.EndCursor
		}
		if !pi.HasNextPage || pi.EndCursor == nil {
			slog.Info("phase.issues.fetch.done", "owner", owner, "repo", repo, "count", len(all))
			return all, lastCursor, nil
		}
		vars["after"] = *pi.EndCursor
	}
	// Unreachable, but keep compiler happy
	// slog.Info placed above on return; here as a fallback
	slog.Info("phase.issues.fetch.done", "owner", owner, "repo", repo, "count", len(all))
	return all, nil, nil
}

// ListAllTimeline lists timeline events for a given issue number.
func (hc *Client) ListAllTimeline(ctx context.Context, owner, repo string, number int) ([]gh.TimelineEvent, error) {
	slog.Info("phase.timeline.fetch.start", "owner", owner, "repo", repo, "issue", number)
	var all []gh.TimelineEvent
	query := `query($owner:String!, $name:String!, $number:Int!, $pageSize:Int!, $after:String){
  repository(owner:$owner, name:$name){
    issue(number:$number){
      timelineItems(first:$pageSize, after:$after, itemTypes:[CLOSED_EVENT, REOPENED_EVENT, ADDED_TO_PROJECT_V2_EVENT, PROJECT_V2_ITEM_STATUS_CHANGED_EVENT, REMOVED_FROM_PROJECT_V2_EVENT]){
        pageInfo{hasNextPage endCursor}
        nodes{
          __typename
          ... on ClosedEvent{ createdAt actor{login} }
          ... on ReopenedEvent{ createdAt actor{login} }
          ... on AddedToProjectV2Event{ createdAt actor{login} project{fullDatabaseId title} }
          ... on ProjectV2ItemStatusChangedEvent{ createdAt actor{login} project{fullDatabaseId title} status previousStatus }
          ... on RemovedFromProjectV2Event{ createdAt actor{login} project{fullDatabaseId title} }
        }
      }
    }
  }
}`
	vars := map[string]any{"owner": owner, "name": repo, "number": number, "pageSize": perPage}
	for {
		body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLEndpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+hc.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.do(ctx, req)
		if err != nil {
			return nil, err
		}
		var out struct {
			Data struct {
				Repository struct {
					Issue struct {
						TimelineItems struct {
							PageInfo struct {
								HasNextPage bool    `json:"hasNextPage"`
								EndCursor   *string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								Typename  string    `json:"__typename"`
								CreatedAt time.Time `json:"createdAt"`
								Actor     *struct {
									Login string `json:"login"`
								} `json:"actor"`
								Project *struct {
									DatabaseID string `json:"fullDatabaseId"`
									Name       string `json:"title"`
								} `json:"project"`
								ProjectColumnName         string `json:"status"`
								PreviousProjectColumnName string `json:"previousStatus"`
							} `json:"nodes"`
						} `json:"timelineItems"`
					} `json:"issue"`
				} `json:"repository"`
			} `json:"data"`
			Errors []struct{ Message string } `json:"errors"`
		}
		// Read body content for logging
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		slog.Debug("phase.timeline.fetch.response", "body", string(bodyBytes))
		if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&out); err != nil {
			return nil, err
		}
		if len(out.Errors) > 0 {
			msgs := make([]string, 0, len(out.Errors))
			for _, e := range out.Errors {
				msgs = append(msgs, e.Message)
			}
			if sleepUntilResetIfRateLimited(resp, msgs) {
				_ = resp.Body.Close()
				// retry same page after sleeping
				continue
			}
			_ = resp.Body.Close()
			return nil, fmt.Errorf("graphql: %s", out.Errors[0].Message)
		}
		_ = resp.Body.Close()
		for _, n := range out.Data.Repository.Issue.TimelineItems.Nodes {
			ev := gh.TimelineEvent{}
			ev.CreatedAt = n.CreatedAt
			if n.Actor != nil {
				ev.Actor = &gh.User{Login: n.Actor.Login}
			}
			if n.Project != nil {
				ev.Project = &gh.Project{ID: n.Project.DatabaseID, Name: n.Project.Name}
			}
			ev.ProjectColumnName = n.ProjectColumnName
			ev.PreviousProjectColumnName = n.PreviousProjectColumnName
			switch n.Typename {
			case "ClosedEvent":
				ev.Event = "closed"
			case "ReopenedEvent":
				ev.Event = "reopened"
			case "AddedToProjectV2Event":
				ev.Event = "added_to_project_v2"
			case "ProjectV2ItemStatusChangedEvent":
				ev.Event = "project_v2_item_status_changed"
			case "RemovedFromProjectV2Event":
				ev.Event = "removed_from_project_v2"
			default:
				continue
			}
			all = append(all, ev)
		}
		pi := out.Data.Repository.Issue.TimelineItems.PageInfo
		if !pi.HasNextPage || pi.EndCursor == nil {
			break
		}
		vars["after"] = *pi.EndCursor
	}
	slog.Info("phase.timeline.fetch.done", "owner", owner, "repo", repo, "issue", number, "events", len(all))
	return all, nil
}
