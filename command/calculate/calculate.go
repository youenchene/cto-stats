package calculate

import (
	"encoding/csv"
	"errors"
	"fmt"
	"math"
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
	Org         string
	Repo        string
	Number      string
	ProjectID   string
	ProjectName string
	ToColumn    string
	At          time.Time
	EventType   string // added|moved|removed
}

// Output row

type calculatedIssue struct {
	ID                        string
	Name                      string
	ProjectID                 string
	ProjectName               string
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
		if len(proj) > 0 {
			row.ProjectID = proj[0].ProjectID
			row.ProjectName = proj[0].ProjectName
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

	// Step 2: calculate monthly lead time and cycle time in days, using all issues with an EndDatetime
	if err := writeMonthlyCycleSummary(filepath.Join(base, "cycle_time.csv"), out); err != nil {
		return err
	}

	// Step 3: weekly throughput with Shewhart control limits (c-chart)
	if err := writeWeeklyThroughput(filepath.Join(base, "throughput_week.csv"), out); err != nil {
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
	required := []string{"org", "repo", "number", "project_id", "project_name", "to_column", "at", "type"}
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
		projID := rec[idx["project_id"]]
		projName := rec[idx["project_name"]]
		toCol := rec[idx["to_column"]]
		at, _ := time.Parse(time.RFC3339, rec[idx["at"]])
		typ := rec[idx["type"]]
		id := key(org, repo, num)
		res[id] = append(res[id], projectEventRow{Org: org, Repo: repo, Number: num, ProjectID: projID, ProjectName: projName, ToColumn: toCol, At: at, EventType: typ})
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
	headers := []string{"id", "name", "project_id", "project_name", "creationdatetime", "leadtimestartdatetime", "cycletimestartdatetime", "devstartdatetime", "reviewstartdatetime", "qastartdatetime", "waitingtopodstartdateime", "enddatetime", "bug"}
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, r := range rows {
		row := []string{
			r.ID,
			r.Name,
			r.ProjectID,
			r.ProjectName,
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

// Step 2 helpers: monthly summary of lead/cycle times in days
func writeMonthlyCycleSummary(path string, rows []calculatedIssue) error {
	byMonth := map[string][]calculatedIssue{}
	for _, r := range rows {
		if r.EndDatetime == nil {
			continue
		}
		m := r.EndDatetime.UTC().Format("2006-01")
		byMonth[m] = append(byMonth[m], r)
	}
	// prepare output rows sorted by month
	type outRow struct {
		Month        string
		IssueCount   int
		LeadDaysAvg  float64
		LeadCount    int
		CycleDaysAvg float64
		CycleCount   int
	}
	var months []string
	for m := range byMonth {
		months = append(months, m)
	}
	sort.Strings(months)
	var outs []outRow
	for _, m := range months {
		issues := byMonth[m]
		var leadSum float64
		var leadCnt int
		var cycleSum float64
		var cycleCnt int
		for _, r := range issues {
			end := r.EndDatetime.UTC()
			if r.LeadTimeStartDatetime != nil {
				lead := end.Sub(r.LeadTimeStartDatetime.UTC()).Hours() / 24.0
				leadSum += lead
				leadCnt++
			}
			if r.CycleTimeStartDatetime != nil {
				cycle := end.Sub(r.CycleTimeStartDatetime.UTC()).Hours() / 24.0
				cycleSum += cycle
				cycleCnt++
			}
		}
		var leadAvg, cycleAvg float64
		if leadCnt > 0 {
			leadAvg = leadSum / float64(leadCnt)
		}
		if cycleCnt > 0 {
			cycleAvg = cycleSum / float64(cycleCnt)
		}
		outs = append(outs, outRow{Month: m, IssueCount: len(issues), LeadDaysAvg: leadAvg, LeadCount: leadCnt, CycleDaysAvg: cycleAvg, CycleCount: cycleCnt})
	}
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
	headers := []string{"month", "issues_count", "leadtime_days_avg", "lead_count", "cycletime_days_avg", "cycle_count"}
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, r := range outs {
		row := []string{
			r.Month,
			fmt.Sprintf("%d", r.IssueCount),
			fmt.Sprintf("%.6f", r.LeadDaysAvg),
			fmt.Sprintf("%d", r.LeadCount),
			fmt.Sprintf("%.6f", r.CycleDaysAvg),
			fmt.Sprintf("%d", r.CycleCount),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// Step 3 helpers: weekly throughput with Shewhart control limits (c-chart)
func writeWeeklyThroughput(path string, rows []calculatedIssue) error {
	// Aggregate counts by ISO year-week
	type wk struct{ Year, Week int }
	counts := map[wk]int{}
	var minTime, maxTime *time.Time
	for _, r := range rows {
		if r.EndDatetime == nil {
			continue
		}
		end := r.EndDatetime.UTC()
		y, w := end.ISOWeek()
		counts[wk{Year: y, Week: w}]++
		if minTime == nil || end.Before(*minTime) {
			t := end
			minTime = &t
		}
		if maxTime == nil || end.After(*maxTime) {
			t := end
			maxTime = &t
		}
	}
	// Build ordered continuous list of ISO weeks between min and max (include zero-throughput weeks)
	var keys []wk
	if minTime != nil && maxTime != nil {
		// Align to Monday (start of ISO week)
		alignToMonday := func(t time.Time) time.Time {
			wd := int(t.Weekday()) // Sunday=0, Monday=1, ..., Saturday=6
			offset := (wd + 6) % 7 // 0 for Monday, 6 for Sunday
			tt := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return tt.AddDate(0, 0, -offset)
		}
		start := alignToMonday(*minTime)
		end := alignToMonday(*maxTime)
		for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 7) {
			y, w := cur.ISOWeek()
			keys = append(keys, wk{Year: y, Week: w})
		}
	}
	// Prepare arrays for per-week limits
	centers := make([]float64, len(keys))
	ucls := make([]float64, len(keys))
	lcls := make([]float64, len(keys))
	// Helper to clamp LCL at 0
	clamp0 := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		return v
	}
	// If no weeks, just write headers
	if len(keys) == 0 {
		// Write CSV headers only
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
		headers := []string{"year", "week", "throughput", "center", "ucl", "lcl"}
		if err := w.Write(headers); err != nil {
			return err
		}
		return w.Error()
	}
	if len(keys) < 6 {
		// Fewer than 6 total weeks: compute from available weeks and apply to all
		var sum float64
		for _, k := range keys {
			sum += float64(counts[k])
		}
		mean := sum / float64(len(keys))
		ucl := mean + 3.0*math.Sqrt(mean)
		lcl := clamp0(mean - 3.0*math.Sqrt(mean))
		for i := range keys {
			centers[i] = mean
			ucls[i] = ucl
			lcls[i] = lcl
		}
	} else {
		// 6-week cadence: compute at week 6,12,18,... and apply for each 6-week block
		lastAssigned := -1
		for blockEnd := 5; blockEnd < len(keys); blockEnd += 6 {
			// Compute mean over the last 6 observed weeks ending at blockEnd
			var sum float64
			for j := blockEnd - 5; j <= blockEnd; j++ {
				sum += float64(counts[keys[j]])
			}
			mean := sum / 6
			ucl := mean + 3.0*math.Sqrt(mean)
			lcl := clamp0(mean - 3.0*math.Sqrt(mean))
			// Assign the same limits for this 6-week block
			blockStart := blockEnd - 5
			for i := blockStart; i <= blockEnd && i < len(keys); i++ {
				ucls[i] = ucl
				lcls[i] = lcl
				lastAssigned = i
			}
		}
		// Tail: if any weeks remain after the last full block, reuse the last block's limits
		if lastAssigned < len(keys)-1 {
			lastUCL := ucls[lastAssigned]
			lastLCL := lcls[lastAssigned]
			for i := lastAssigned + 1; i < len(keys); i++ {
				ucls[i] = lastUCL
				lcls[i] = lastLCL
			}
		}
	}
	// Before writing, set center to the number of issues ended for each week (weekly throughput)
	for i, k := range keys {
		centers[i] = float64(counts[k])
	}
	// Write CSV
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
	headers := []string{"year", "week", "throughput", "center", "ucl", "lcl"}
	if err := w.Write(headers); err != nil {
		return err
	}
	for i, k := range keys {
		row := []string{
			fmt.Sprintf("%d", k.Year),
			fmt.Sprintf("%02d", k.Week),
			fmt.Sprintf("%d", counts[k]),
			fmt.Sprintf("%.6f", centers[i]),
			fmt.Sprintf("%.6f", ucls[i]),
			fmt.Sprintf("%.6f", lcls[i]),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}
