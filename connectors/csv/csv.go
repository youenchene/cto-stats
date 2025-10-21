package csv

import (
	gh "cto-stats/domain/github"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WriteAllCSVs writes all CSV outputs into the data/ directory.
func WriteAllCSVs(org string, repos []gh.Repo, reports []gh.IssueReport) error {
	dir := filepath.Join("data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := WriteRepositoryCSV(filepath.Join(dir, "repository.csv"), org, repos); err != nil {
		return err
	}
	if err := WriteProjectCSV(filepath.Join(dir, "project.csv"), reports); err != nil {
		return err
	}
	if err := WriteIssueCSV(filepath.Join(dir, "issue.csv"), reports); err != nil {
		return err
	}
	if err := WriteIssueStatusCSV(filepath.Join(dir, "issue_status_event.csv"), reports); err != nil {
		return err
	}
	if err := WriteIssueProjectCSV(filepath.Join(dir, "issue_project_event.csv"), reports); err != nil {
		return err
	}
	return nil
}

func WriteRepositoryCSV(path string, org string, repos []gh.Repo) error {
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

func WriteProjectCSV(path string, reports []gh.IssueReport) error {
	// collect unique projects by ID
	projects := map[string]string{}
	for _, rep := range reports {
		for _, ev := range rep.ProjectHistory {
			if ev.ProjectID != "" {
				if ev.ProjectName != "" {
					projects[ev.ProjectID] = ev.ProjectName
				} else if _, ok := projects[ev.ProjectID]; !ok {
					projects[ev.ProjectID] = ""
				}
			}
		}
		for _, cur := range rep.CurrentProjects {
			if cur.ProjectID != "" {
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
		row := []string{id, name}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func WriteIssueCSV(path string, reports []gh.IssueReport) error {
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

func WriteIssueStatusCSV(path string, reports []gh.IssueReport) error {
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

func WriteIssueProjectCSV(path string, reports []gh.IssueReport) error {
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
				ev.ProjectID,
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
