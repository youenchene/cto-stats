package calculate

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	lo "github.com/samber/lo"
)

// Row models for reading CSVs

type issueRow struct {
	Org       string
	Repo      string
	Number    string
	Title     string
	IsBug     bool
	CreatedAt time.Time
}

type statusEventRow struct {
	Org    string
	Repo   string
	Number string
	Type   string // opened|closed|reopened
	At     time.Time
}

type projectEventRow struct {
	Org       string
	Repo      string
	Number    string
	ToColumn  string
	At        time.Time
	EventType string // added|moved|removed
}

// Output row

type calculatedIssue struct {
	ID                        string
	Name                      string
	CreationDatetime          time.Time
	LeadTimeStartDatetime     *time.Time
	CycleTimeStartDatetime    *time.Time
	DevStartDatetime          *time.Time
	ReviewStartDatetime       *time.Time
	QAStartDatetime           *time.Time
	WaitingToPodStartDatetime *time.Time
	EndDatetime               *time.Time
	Bug                       bool
}

// Run executes the calculate command (no extra args expected)
func Run(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("calculate: no arguments expected")
	}

	// Read inputs from data/
	base := "data"
	issues, err := readIssues(filepath.Join(base, "issue.csv"))
	if err != nil {
		return err
	}
	statusByID, err := readStatus(filepath.Join(base, "issue_status_event.csv"))
	if err != nil {
		return err
	}
	projByID, err := readProject(filepath.Join(base, "issue_project_event.csv"))
	if err != nil {
		return err
	}

	// Build output
	var out []calculatedIssue
	for id, is := range issues {
		proj := projByID[id]
		st := statusByID[id]
		row := calculatedIssue{
			ID:               id,
			Name:             is.Title,
			CreationDatetime: is.CreatedAt,
			Bug:              is.IsBug,
		}
		row.LeadTimeStartDatetime = firstMoveToAny(proj, []string{"Backlog", "Ready"})
		row.CycleTimeStartDatetime = firstMoveToAny(proj, []string{"In Progress", "In progress"})
		if row.CycleTimeStartDatetime == nil {
			row.CycleTimeStartDatetime = firstMoveToAny(proj, []string{"Backlog", "Ready"})
		}
		row.DevStartDatetime = firstMoveToAny(proj, []string{"In Progress", "In progress"})
		if row.DevStartDatetime == nil {
			row.DevStartDatetime = firstMoveToAny(proj, []string{"Backlog", "Ready"})
		}
		row.ReviewStartDatetime = firstMoveToAny(proj, []string{"In review", "In Review"})
		// No rule yet
		row.QAStartDatetime = nil
		row.WaitingToPodStartDatetime = firstMoveTo(proj, "Done")
		row.EndDatetime = computeEnd(st, proj)

		out = append(out, row)
	}

	// Deterministic order
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	if err := writeOutput(filepath.Join(base, "calculated_issue.csv"), out); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "calculate.done count=%d\n", len(out))
	return nil
}

func key(org, repo, number string) string { return org + "/" + repo + "#" + number }

func readIssues(path string) (map[string]issueRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	rec, err := r.Read()
	if err != nil {
		return nil, err
	}
	// Expect headers: org,repo,number,title,url,state,is_bug,creator,assignees,created_at,closed_at,committer
	idx := indexMap(rec)
	required := []string{"org", "repo", "number", "title", "is_bug", "created_at"}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("issue.csv missing column %s", col)
		}
	}

	res := map[string]issueRow{}
	for {
		rec, err = r.Read()
		if errors.Is(err, os.ErrClosed) {
			break
		}
		if err != nil {
			if errors.Is(err, csv.ErrFieldCount) {
				continue
			}
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		org := rec[idx["org"]]
		repo := rec[idx["repo"]]
		num := rec[idx["number"]]
		title := rec[idx["title"]]
		isBug := parseBool(rec[idx["is_bug"]])
		created, _ := time.Parse(time.RFC3339, rec[idx["created_at"]])
		res[key(org, repo, num)] = issueRow{Org: org, Repo: repo, Number: num, Title: title, IsBug: isBug, CreatedAt: created}
	}
	return res, nil
}

func readStatus(path string) (map[string][]statusEventRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := indexMap(head)
	required := []string{"org", "repo", "number", "type", "at"}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("issue_status_event.csv missing column %s", col)
		}
	}
	res := map[string][]statusEventRow{}
	for {
		rec, err := r.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		org := rec[idx["org"]]
		repo := rec[idx["repo"]]
		num := rec[idx["number"]]
		typ := rec[idx["type"]]
		at, _ := time.Parse(time.RFC3339, rec[idx["at"]])
		id := key(org, repo, num)
		res[id] = append(res[id], statusEventRow{Org: org, Repo: repo, Number: num, Type: typ, At: at})
	}
	// Sort by time
	for _, v := range res {
		sort.Slice(v, func(i, j int) bool { return v[i].At.Before(v[j].At) })
	}
	return res, nil
}

func readProject(path string) (map[string][]projectEventRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := indexMap(head)
	required := []string{"org", "repo", "number", "to_column", "at", "type"}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("issue_project_event.csv missing column %s", col)
		}
	}
	res := map[string][]projectEventRow{}
	for {
		rec, err := r.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		org := rec[idx["org"]]
		repo := rec[idx["repo"]]
		num := rec[idx["number"]]
		toCol := rec[idx["to_column"]]
		at, _ := time.Parse(time.RFC3339, rec[idx["at"]])
		typ := rec[idx["type"]]
		id := key(org, repo, num)
		res[id] = append(res[id], projectEventRow{Org: org, Repo: repo, Number: num, ToColumn: toCol, At: at, EventType: typ})
	}
	for _, v := range res {
		sort.Slice(v, func(i, j int) bool { return v[i].At.Before(v[j].At) })
	}
	return res, nil
}

func indexMap(headers []string) map[string]int {
	m := map[string]int{}
	for i, h := range headers {
		m[strings.TrimSpace(strings.ToLower(h))] = i
	}
	return m
}

func parseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1" || s == "yes"
}

// Independent rules per field

func firstMoveTo(events []projectEventRow, column string) *time.Time {
	if len(events) == 0 {
		return nil
	}
	col := strings.ToLower(strings.TrimSpace(column))
	// Only consider moved events
	if ev, ok := lo.Find(events, func(e projectEventRow) bool {
		return e.EventType == "moved" && strings.ToLower(strings.TrimSpace(e.ToColumn)) == col
	}); ok {
		return &ev.At
	}
	return nil
}

func firstMoveToAny(events []projectEventRow, columns []string) *time.Time {
	if len(events) == 0 {
		return nil
	}
	set := lo.SliceToMap(columns, func(s string) (string, struct{}) { return strings.ToLower(strings.TrimSpace(s)), struct{}{} })
	if ev, ok := lo.Find(events, func(e projectEventRow) bool {
		_, wanted := set[strings.ToLower(strings.TrimSpace(e.ToColumn))]
		return e.EventType == "moved" && wanted
	}); ok {
		return &ev.At
	}
	return nil
}

func computeEnd(status []statusEventRow, proj []projectEventRow) *time.Time {
	var closed *time.Time
	if ev, ok := lo.Find(status, func(s statusEventRow) bool { return s.Type == "closed" }); ok {
		closed = &ev.At
	}
	var archived *time.Time
	if ev, ok := lo.Find(proj, func(p projectEventRow) bool { return p.EventType == "moved" && equalFoldTrim(p.ToColumn, "Archive") }); ok {
		archived = &ev.At
	}
	if closed == nil && archived == nil {
		return nil
	}
	if closed == nil {
		return archived
	}
	if archived == nil {
		return closed
	}
	if closed.Before(*archived) {
		return closed
	}
	return archived
}

func equalFoldTrim(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func writeOutput(path string, rows []calculatedIssue) error {
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
	headers := []string{"id", "name", "creationdatetime", "leadtimestartdatetime", "cycletimestartdatetime", "devstartdatetime", "reviewstartdatetime", "qastartdatetime", "waitingtopodstartdateime", "enddatetime", "bug"}
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, r := range rows {
		row := []string{
			r.ID,
			r.Name,
			r.CreationDatetime.UTC().Format(time.RFC3339),
			formatTime(r.LeadTimeStartDatetime),
			formatTime(r.CycleTimeStartDatetime),
			formatTime(r.DevStartDatetime),
			formatTime(r.ReviewStartDatetime),
			formatTime(r.QAStartDatetime),
			formatTime(r.WaitingToPodStartDatetime),
			formatTime(r.EndDatetime),
			fmt.Sprintf("%t", r.Bug),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
