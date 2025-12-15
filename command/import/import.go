package cmdimport

import (
	"context"
	"cto-stats/connectors/azure"
	"cto-stats/connectors/config"
	ccsv "cto-stats/connectors/csv"
	"cto-stats/connectors/gcp"
	cg "cto-stats/connectors/github"
	"cto-stats/domain/cloudspending"
	gh "cto-stats/domain/github"
	"encoding/csv"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Type aliases to avoid leaking internal domain types to callers while keeping code concise here
// (same as previous main.go aliases)
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

// checkpoints stores per-repo import progress to allow incremental runs.
// Note: checkpoint management removed. The import now runs without persisting cursors.

// Run executes the import subcommand. It expects flag arguments like: -org, -since, -repo.
func Run(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	org := fs.String("org", "", "GitHub organization (optional if CONFIG_PATH points to config with github.org)")
	since := fs.String("since", "", "Only issues updated since this ISO8601/RFC3339 time, e.g., 2025-01-01T00:00:00Z (optional)")
	repoFilter := fs.String("repo", "", "Comma-separated list of repositories to include (optional)")
	// Scopes: allow separating processing into issues and PRs
	issuesScope := fs.Bool("issues", false, "Process issues scope: issues, timelines, project moves")
	prScope := fs.Bool("pr", false, "Process pull-requests scope: PRs and change-request reviews")
	cloudSpendingScope := fs.Bool("cloudspending", false, "Process cloud spending scope: Azure and GCP costs")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Cloud spending scope is independent
	if *cloudSpendingScope {
		return runCloudSpendingImport()
	}

	// Backward compatibility: if no scope is specified, process both issues and PRs
	if !*issuesScope && !*prScope {
		*issuesScope = true
		*prScope = true
	}

	// Resolve config and org
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}
	if *org == "" {
		// Try read org from config if file exists
		if _, err := os.Stat(cfgPath); err == nil {
			if cfg, err := config.Load(cfgPath); err == nil {
				if cfg.GitHub.Org != "" {
					*org = cfg.GitHub.Org
				}
			}
		}
	}
	if *org == "" {
		fmt.Fprintln(os.Stderr, "-org is required when no config file with github.org is provided (set CONFIG_PATH to a config file to provide org)")
		slog.Error("import.validation.error", "reason", "missing org")
		return fmt.Errorf("missing required -org or CONFIG_PATH with github.org")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "GITHUB_TOKEN environment variable is required.")
		slog.Error("import.validation.error", "reason", "missing GITHUB_TOKEN")
		return fmt.Errorf("missing GITHUB_TOKEN")
	}

	slog.Info("import.start", "org", *org, "since", *since, "repoFilter", *repoFilter, "issues", *issuesScope, "pr", *prScope)

	ctx := context.Background()
	ghc := cg.New(nil, token)

	allowedRepos := map[string]bool{}
	if *repoFilter != "" {
		for _, r := range strings.Split(*repoFilter, ",") {
			allowedRepos[strings.TrimSpace(r)] = true
		}
	}

	repos, err := ghc.ListAllRepos(ctx, *org)
	if err != nil {
		slog.Error("phase.repos.fetch.error", "org", *org, "error", err)
		fmt.Fprintf(os.Stderr, "error listing repos: %v\n", err)
		return err
	}

	var reports []IssueReport
	if *issuesScope {
		for _, r := range repos {
			if *repoFilter != "" && !allowedRepos[r.Name] {
				continue
			}
			// No checkpoint resume: always start from the beginning or respect the provided -since filter.
			slog.Info("phase.issues.import.start", "owner", r.Owner.Login, "repo", r.Name, "since", *since)
			issues, _, err := ghc.ListAllIssues(ctx, r.Owner.Login, r.Name, *since, "")
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
				// Prefer GitHub IssueType when available; fallback to label heuristics. Also set IsBug.
				var typ string
				if strings.TrimSpace(is.Type) != "" {
					typ = strings.ToLower(strings.TrimSpace(is.Type))
				}
				if typ == "" {
					for _, l := range is.Labels {
						name := strings.ToLower(strings.TrimSpace(l.Name))
						if name == "bug" {
							report.IsBug = true
							if typ == "" {
								typ = "bug"
							}
						} else if typ == "" { // only derive if not already known
							if strings.Contains(name, "feature") {
								typ = "feature"
							} else if strings.Contains(name, "chore") || strings.Contains(name, "refactor") {
								typ = "chore"
							} else if strings.Contains(name, "doc") {
								typ = "docs"
							}
						}
					}

					if typ == "" {
						typ = "task"
					}
				}
				report.Type = typ
				if strings.EqualFold(typ, "bug") {
					report.IsBug = true
				}

				// Timeline aggregation
				evts, err := ghc.ListAllTimeline(ctx, r.Owner.Login, r.Name, is.Number)
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
						projectID   string
						projectName string
						columnID    int64
						columnName  string
					}
					currentByProject := map[string]*current{}

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
							var projID string
							var projName string
							if ev.Project != nil {
								projID = ev.Project.ID
								projName = ev.Project.Name
							}
							if projID != "" {
								projHist = append(projHist, ProjectMoveEvent{ProjectID: projID, ProjectName: projName, FromColumn: "", At: ev.CreatedAt, By: valueOrEmpty(ev.Actor), Type: "added"})
								c := &current{present: true, projectID: projID, projectName: projName}
								currentByProject[projID] = c
							}
						case "project_v2_item_status_changed":
							var projID string
							var projName string
							var colNameTo = ev.ProjectColumnName
							var colNameFrom = ev.PreviousProjectColumnName
							// Prefer GraphQL-provided project info
							if ev.Project != nil {
								projID = ev.Project.ID
								projName = ev.Project.Name
							}
							if projID != "" {
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
							var projID string
							var projName string
							if ev.Project != nil {
								projID = ev.Project.ID
								projName = ev.Project.Name
							}
							if projID != "" {
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
		if err := ccsv.WriteAllCSVs(*org, repos, reports); err != nil {
			slog.Error("phase.csv.write.error", "error", err)
			fmt.Fprintf(os.Stderr, "failed to write CSV outputs: %v\n", err)
		}
	}

	// New: fetch PRs and reviews and write to unified CSVs (PR scope)
	prUnifiedPath := "data/pr.csv"
	rvUnifiedPath := "data/pr_review.csv"

	var allPRs []gh.PullRequest
	var allReviews []gh.PullRequestReview

	if *prScope {
		for _, r := range repos {
			if *repoFilter != "" && !allowedRepos[r.Name] {
				continue
			}
			// List PRs opened/updated since
			prs, err := ghc.ListAllPullRequests(ctx, r.Owner.Login, r.Name, *since)
			if err != nil {
				slog.Warn("phase.prs.fetch.error", "owner", r.Owner.Login, "repo", r.Name, "error", err)
				continue
			}
			// Collect all PRs
			for i := range prs {
				prs[i].Org = *org
				prs[i].Repo = r.Name
			}
			allPRs = append(allPRs, prs...)

			// For each PR, fetch reviews and collect them
			for _, pr := range prs {
				reviews, err := ghc.ListAllPullRequestReviews(ctx, r.Owner.Login, r.Name, pr.Number)
				if err != nil {
					slog.Warn("phase.pr.reviews.fetch.error", "repo", r.Name, "pr", pr.Number, "error", err)
					continue
				}
				if len(reviews) == 0 {
					continue
				}
				// Collect all reviews
				for i := range reviews {
					reviews[i].Org = *org
					reviews[i].Repo = r.Name
					reviews[i].PullRequestNumber = pr.Number
				}
				allReviews = append(allReviews, reviews...)
			}
		}

		// Write all collected PRs and reviews at once
		if err := ccsv.WritePullRequests(prUnifiedPath, allPRs); err != nil {
			slog.Warn("phase.prs.csv.error", "error", err)
		}
		if err := ccsv.WritePullRequestReviews(rvUnifiedPath, allReviews); err != nil {
			slog.Warn("phase.pr.reviews.csv.error", "error", err)
		}
	}
	slog.Info("import.done", "reports", len(reports))
	return nil
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

// runCloudSpendingImport fetches cloud spending data from Azure and GCP
func runCloudSpendingImport() error {
	slog.Info("cloudspending.import.start")
	ctx := context.Background()

	var allRecords []cloudspending.CostRecord

	// Fetch Azure costs (last 24 months)
	// Support multiple subscription IDs separated by commas
	azureSubscriptionIDs := os.Getenv("AZURE_SUBSCRIPTION_ID")
	azureTenantID := os.Getenv("AZURE_TENANT_ID")
	azureClientID := os.Getenv("AZURE_CLIENT_ID")
	azureClientSecret := os.Getenv("AZURE_CLIENT_SECRET")

	if azureSubscriptionIDs != "" && azureTenantID != "" && azureClientID != "" && azureClientSecret != "" {
		slog.Info("cloudspending.azure.fetch.start")

		// Split subscription IDs by comma to support multiple subscriptions
		subscriptionList := strings.Split(azureSubscriptionIDs, ",")

		for _, subID := range subscriptionList {
			subID = strings.TrimSpace(subID)
			if subID == "" {
				continue
			}

			slog.Info("cloudspending.azure.fetch.subscription", "subscription_id", subID)
			azureClient := azure.NewClient(subID, azureTenantID, azureClientID, azureClientSecret)
			azureRecords, err := azureClient.FetchCosts(ctx, 24)
			if err != nil {
				slog.Warn("cloudspending.azure.fetch.error", "subscription_id", subID, "error", err)
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch Azure costs for subscription %s: %v\n", subID, err)
			} else {
				allRecords = append(allRecords, azureRecords...)
				slog.Info("cloudspending.azure.fetch.done", "subscription_id", subID, "count", len(azureRecords))
			}
		}
	} else {
		slog.Info("cloudspending.azure.skip", "reason", "missing environment variables")
	}

	// Fetch GCP costs (last 24 months)
	gcpProjectID := os.Getenv("GCP_PROJECT_ID")
	gcpBillingAccount := os.Getenv("GCP_BILLING_ACCOUNT")
	gcpServiceAccountJSON := os.Getenv("GCP_SERVICE_ACCOUNT_JSON")

	// Allow ADC: proceed if project and billing account are set. Service account JSON is optional now.
	if gcpProjectID != "" && gcpBillingAccount != "" {
		gcpLocation := os.Getenv("GCP_BIGQUERY_LOCATION") // e.g., EU, US, europe-west1
		if gcpLocation == "" {
			slog.Info("cloudspending.gcp.fetch.start", "project", gcpProjectID, "billing", gcpBillingAccount)
		} else {
			slog.Info("cloudspending.gcp.fetch.start", "project", gcpProjectID, "billing", gcpBillingAccount, "location", gcpLocation)
		}
		gcpClient := gcp.NewClient(gcpProjectID, gcpBillingAccount, gcpServiceAccountJSON, gcpLocation)
		gcpRecords, err := gcpClient.FetchCosts(ctx)
		if err != nil {
			slog.Warn("cloudspending.gcp.fetch.error", "error", err)
			fmt.Fprintf(os.Stderr, "Warning: failed to fetch GCP costs: %v\n", err)
		} else {
			allRecords = append(allRecords, gcpRecords...)
			slog.Info("cloudspending.gcp.fetch.done", "count", len(gcpRecords))
		}
	} else {
		slog.Info("cloudspending.gcp.skip", "reason", "missing GCP_PROJECT_ID or GCP_BILLING_ACCOUNT")
	}

	// Write to CSV
	if len(allRecords) == 0 {
		slog.Warn("cloudspending.import.no_data")
		return fmt.Errorf("no cloud spending data fetched - check environment variables")
	}

	outputPath := filepath.Join("data", "cloud_costs.csv")
	if err := writeCloudCostsCSV(outputPath, allRecords); err != nil {
		slog.Error("cloudspending.csv.write.error", "error", err)
		return fmt.Errorf("failed to write cloud costs CSV: %w", err)
	}

	slog.Info("cloudspending.import.done", "records", len(allRecords), "output", outputPath)
	return nil
}

// writeCloudCostsCSV writes cloud cost records to a CSV file
func writeCloudCostsCSV(path string, records []cloudspending.CostRecord) error {
	// Ensure data directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header
	header := []string{"provider", "service", "month", "cost", "currency"}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write records
	for _, r := range records {
		row := []string{
			r.Provider,
			r.Service,
			r.Month.Format("2006-01-02"),
			fmt.Sprintf("%.2f", r.Cost),
			r.Currency,
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("failed to write row: %w", err)
		}
	}

	return nil
}
