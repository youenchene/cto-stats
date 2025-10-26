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
		_ = resp.Body.Close()
		if len(out.Errors) > 0 {
			return nil, fmt.Errorf("graphql: %s", out.Errors[0].Message)
		}
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
func (hc *Client) ListAllIssues(ctx context.Context, owner, repo, since string) ([]gh.Issue, error) {
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
			return nil, err
		}
		if len(out.Errors) > 0 {
			return nil, fmt.Errorf("graphql: %s", out.Errors[0].Message)
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
		if !pi.HasNextPage || pi.EndCursor == nil {
			break
		}
		vars["after"] = *pi.EndCursor
	}
	slog.Info("phase.issues.fetch.done", "owner", owner, "repo", repo, "count", len(all))
	return all, nil
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
		_ = resp.Body.Close()
		if len(out.Errors) > 0 {
			return nil, fmt.Errorf("graphql: %s", out.Errors[0].Message)
		}
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
