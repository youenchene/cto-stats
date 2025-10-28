package calculate

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cto-stats/connectors/config"

	lo "github.com/samber/lo"
)

// Row models for reading CSVs

type issueRow struct {
	Org       string
	Repo      string
	Number    string
	Title     string
	Type      string
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
	Type                      string
	CurrentColumn             string
}

// Run executes the calculate command
func Run(args []string) error {
	fs := flag.NewFlagSet("calculate", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to YAML config file (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}
	// For calculate, a config file is required
	if _, err := os.Stat(cfgPath); err != nil {
		return fmt.Errorf("calculate: config file required at -config (default ./config.yml): %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("calculate: failed to load config: %w", err)
	}
	// Build a project lookup by ID for quick access
	projCfgByID := map[string]config.Project{}
	for _, p := range cfg.GitHub.Projects {
		projCfgByID[p.ID] = p
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
	var allIssues []calculatedIssue
	for id, is := range issues {
		projEvents := projByID[id]
		st := statusByID[id]

		row := calculatedIssue{
			ID:               id,
			Name:             is.Title,
			CreationDatetime: is.CreatedAt,
			Bug:              is.IsBug,
			Type:             is.Type,
		}
		// Determine project on first project event if any
		var pid, pname string
		if len(projEvents) > 0 {
			pid = projEvents[0].ProjectID
			pname = projEvents[0].ProjectName
			row.ProjectID = pid
			row.ProjectName = pname
		}
		// Apply config filters if project known and present in config
		if pc, ok := projCfgByID[pid]; ok {
			if pc.Exclude {
				continue
			}
			if len(pc.Types) > 0 {
				// case-insensitive compare
				allowed := false
				for _, t := range pc.Types {
					if strings.EqualFold(strings.TrimSpace(t), strings.TrimSpace(is.Type)) {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}
			// Use configured columns for stage timestamps
			choose := func(cols []string) *time.Time {
				if len(cols) > 0 {
					return firstMoveToAny(projEvents, cols)
				}
				return nil
			}
			row.LeadTimeStartDatetime = choose(pc.LeadTimeColumns)
			row.CycleTimeStartDatetime = choose(pc.CycleTimeColumns)

			row.DevStartDatetime = choose(pc.DevStartColumns)
			row.ReviewStartDatetime = choose(pc.ReviewStartColumns)
			row.QAStartDatetime = choose(pc.QAStartColumns)
			row.WaitingToPodStartDatetime = choose(pc.WaitingToProdStartCols)
			// End datetime: by default earliest of status closed and configured inprod columns (e.g., Archive/Done)
			var endCandidates []*time.Time
			if e := choose(pc.InProdStartColumns); e != nil {
				endCandidates = append(endCandidates, e)
			}
			// closed status
			if ev, ok := lo.Find(st, func(s statusEventRow) bool { return s.Type == "closed" }); ok {
				end := ev.At
				endCandidates = append(endCandidates, &end)
			}
			row.EndDatetime = earliest(endCandidates)
			if row.EndDatetime != nil {
				if row.CycleTimeStartDatetime == nil {
					row.CycleTimeStartDatetime = row.LeadTimeStartDatetime
				}
				if row.DevStartDatetime == nil {
					row.DevStartDatetime = row.LeadTimeStartDatetime
				}
			}
		} else {
			slog.Info("calculate.project_unknown", "issue_id", id, "id", pid, "name", pname, "type", is.Type, "events", projEvents, "status", st)
			// No matching project in config: fallback to legacy behavior
			row.LeadTimeStartDatetime = firstMoveToAny(projEvents, []string{"Backlog", "Ready"})
			row.CycleTimeStartDatetime = firstMoveToAny(projEvents, []string{"In Progress", "In progress"})
			if row.CycleTimeStartDatetime == nil {
				row.CycleTimeStartDatetime = firstMoveToAny(projEvents, []string{"Backlog", "Ready"})
			}
			row.DevStartDatetime = firstMoveToAny(projEvents, []string{"In Progress", "In progress"})
			if row.DevStartDatetime == nil {
				row.DevStartDatetime = firstMoveToAny(projEvents, []string{"Backlog", "Ready"})
			}
			row.ReviewStartDatetime = firstMoveToAny(projEvents, []string{"In review", "In Review"})
			row.QAStartDatetime = nil
			row.WaitingToPodStartDatetime = firstMoveTo(projEvents, "Done")
			row.EndDatetime = computeEnd(st, projEvents)
		}

		allIssues = append(allIssues, row)
	}

	// Deterministic order
	sort.Slice(allIssues, func(i, j int) bool { return allIssues[i].ID < allIssues[j].ID })

	// Build convenience slices using lo
	closedIssues := lo.Filter(allIssues, func(ci calculatedIssue, _ int) bool { return ci.EndDatetime != nil })
	openIssues := lo.Filter(allIssues, func(ci calculatedIssue, _ int) bool { return ci.EndDatetime == nil })

	if err := writeOutput(filepath.Join(base, "calculated_issue.csv"), allIssues); err != nil {
		return err
	}

	// Step 2: calculate monthly lead time and cycle time in days, using all issues with an EndDatetime
	if err := writeMonthlyCycleSummary(filepath.Join(base, "cycle_time.csv"), closedIssues); err != nil {
		return err
	}

	// Step 3: weekly throughput with Shewhart control limits (c-chart)
	if err := writeWeeklyThroughput(filepath.Join(base, "throughput_week.csv"), closedIssues); err != nil {
		return err
	}

	// Step 4: current stocks for not-closed issues by stage
	if err := writeStocks(filepath.Join(base, "stocks.csv"), openIssues); err != nil {
		return err
	}

	// Step 5: weekly stocks per project by ISO year-week (cutoff at Sunday 23:59:59 UTC)
	if err := writeWeeklyStocks(filepath.Join(base, "stocks_week.csv"), openIssues); err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("calculate.done count=%d countClosed=%d count Open=%d", len(allIssues), len(closedIssues), len(openIssues)))
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
	// Expect headers: org,repo,number,title,url,state,type,is_bug,creator,assignees,created_at,closed_at,committer
	idx := indexMap(rec)
	required := []string{"org", "repo", "number", "title", "created_at"}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("issue.csv missing column %s", col)
		}
	}
	// Optional columns for backward compatibility
	_, hasType := idx["type"]
	_, hasIsBug := idx["is_bug"]

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
		typeVal := ""
		if hasType {
			typeVal = rec[idx["type"]]
		}
		isBug := false
		if hasIsBug {
			isBug = parseBool(rec[idx["is_bug"]])
		}
		created, _ := time.Parse(time.RFC3339, rec[idx["created_at"]])
		res[key(org, repo, num)] = issueRow{Org: org, Repo: repo, Number: num, Title: title, Type: typeVal, IsBug: isBug, CreatedAt: created}
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

// earliest returns the earliest non-nil time among candidates, or nil if none.
func earliest(ts []*time.Time) *time.Time {
	var res *time.Time
	for _, t := range ts {
		if t == nil {
			continue
		}
		if res == nil || t.Before(*res) {
			tt := *t
			res = &tt
		}
	}
	return res
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
	headers := []string{"id", "name", "project_id", "project_name", "creationdatetime", "leadtimestartdatetime", "cycletimestartdatetime", "devstartdatetime", "reviewstartdatetime", "qastartdatetime", "waitingtopodstartdateime", "enddatetime", "bug", "type"}
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
			r.Type,
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

// Step 4: stocks for not-closed issues by stage
func writeStocks(path string, rows []calculatedIssue) error {
	// aggregate by project
	type agg struct {
		OpenedBugs    int
		InBacklogs    int
		InDev         int
		InReview      int
		InQA          int
		WaitingToProd int
	}
	byProj := map[string]struct {
		ProjectID   string
		ProjectName string
		Agg         agg
	}{}
	// helper to get bucket/stage booleans for a not-closed issue
	stageFlags := func(r calculatedIssue) (openedBug bool, inBacklog bool, inDev bool, inReview bool, inQA bool, waiting bool) {
		if r.EndDatetime != nil {
			return false, false, false, false, false, false
		}
		openedBug = r.Bug
		// Stage logic: take the furthest known stage, ensuring exclusivity across stages
		if r.WaitingToPodStartDatetime != nil {
			return openedBug, false, false, false, false, true
		}
		if r.QAStartDatetime != nil {
			return openedBug, false, false, false, true, false
		}
		if r.ReviewStartDatetime != nil {
			return openedBug, false, false, true, false, false
		}
		if r.DevStartDatetime != nil {
			return openedBug, false, true, false, false, false
		}
		// Backlog (only early dates present: creation/lead/cycle)
		return openedBug, true, false, false, false, false
	}
	for _, r := range rows {
		if r.EndDatetime != nil {
			continue // only not-closed
		}
		key := r.ProjectID + "\u0000" + r.ProjectName
		rec := byProj[key]
		rec.ProjectID = r.ProjectID
		rec.ProjectName = r.ProjectName
		ob, ib, id, ir, iq, iw := stageFlags(r)
		if ob {
			rec.Agg.OpenedBugs++
		}
		if ib {
			rec.Agg.InBacklogs++
		}
		if id {
			rec.Agg.InDev++
		}
		if ir {
			rec.Agg.InReview++
		}
		if iq {
			rec.Agg.InQA++
		}
		if iw {
			rec.Agg.WaitingToProd++
		}
		byProj[key] = rec
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
	headers := []string{"project_id", "project_name", "opened_bugs", "in_backlogs", "in_dev", "in_review", "in_qa", "waiting_to_prod"}
	if err := w.Write(headers); err != nil {
		return err
	}
	// stable order by project_id then name
	var keys []string
	for k := range byProj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rec := byProj[k]
		row := []string{
			rec.ProjectID,
			rec.ProjectName,
			fmt.Sprintf("%d", rec.Agg.OpenedBugs),
			fmt.Sprintf("%d", rec.Agg.InBacklogs),
			fmt.Sprintf("%d", rec.Agg.InDev),
			fmt.Sprintf("%d", rec.Agg.InReview),
			fmt.Sprintf("%d", rec.Agg.InQA),
			fmt.Sprintf("%d", rec.Agg.WaitingToProd),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// Step 5: weekly stocks per project and ISO week with Sunday cutoff (UTC)
func writeWeeklyStocks(path string, rows []calculatedIssue) error {
	// Determine range of weeks
	timeUTC := func(t time.Time) time.Time { return t.UTC() }
	var minT, maxT *time.Time
	for _, r := range rows {
		c := timeUTC(r.CreationDatetime)
		if minT == nil || c.Before(*minT) {
			t := c
			minT = &t
		}
		cands := []*time.Time{r.LeadTimeStartDatetime, r.CycleTimeStartDatetime, r.DevStartDatetime, r.ReviewStartDatetime, r.QAStartDatetime, r.WaitingToPodStartDatetime, r.EndDatetime}
		for _, p := range cands {
			if p == nil {
				continue
			}
			t := p.UTC()
			if maxT == nil || t.After(*maxT) {
				u := t
				maxT = &u
			}
		}
	}
	if minT == nil {
		// nothing to write, create headers only
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
		headers := []string{"year", "week", "project_id", "project_name", "opened_bugs", "in_backlogs", "in_dev", "in_review", "in_qa", "waiting_to_prod"}
		if err := w.Write(headers); err != nil {
			return err
		}
		return w.Error()
	}
	if maxT == nil {
		m := time.Now().UTC()
		maxT = &m
	}
	// Align to Monday 00:00 UTC of ISO week
	alignToMonday := func(t time.Time) time.Time {
		wd := int(t.Weekday())
		offset := (wd + 6) % 7
		tt := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return tt.AddDate(0, 0, -offset)
	}
	start := alignToMonday(*minT)
	end := alignToMonday(*maxT)
	type wk struct{ Year, Week int }
	// Iterate weeks
	type agg struct{ OpenedBugs, InBacklogs, InDev, InReview, InQA, WaitingToProd int }
	type rec struct {
		ProjectID, ProjectName string
		Agg                    agg
	}
	// Helper: determine stage at cutoff
	stageAt := func(r calculatedIssue, cutoff time.Time) (openedBug bool, inBacklog bool, inDev bool, inReview bool, inQA bool, waiting bool) {
		cu := cutoff
		// Not yet created
		if timeUTC(r.CreationDatetime).After(cu) {
			return false, false, false, false, false, false
		}
		// If ended before or at cutoff, it is not in stock
		if r.EndDatetime != nil && !r.EndDatetime.UTC().After(cu) {
			return false, false, false, false, false, false
		}
		openedBug = r.Bug
		// Helper to check ts <= cutoff
		le := func(t *time.Time) bool { return t != nil && !t.UTC().After(cu) }
		// Furthest stage reached as of cutoff (no later stage timestamp <= cutoff)
		// Waiting
		if le(r.WaitingToPodStartDatetime) {
			return openedBug, false, false, false, false, true
		}
		// QA
		if le(r.QAStartDatetime) && !le(r.WaitingToPodStartDatetime) {
			return openedBug, false, false, false, true, false
		}
		// Review
		if le(r.ReviewStartDatetime) && !le(r.QAStartDatetime) && !le(r.WaitingToPodStartDatetime) {
			return openedBug, false, false, true, false, false
		}
		// Dev
		if le(r.DevStartDatetime) && !le(r.ReviewStartDatetime) && !le(r.QAStartDatetime) && !le(r.WaitingToPodStartDatetime) {
			return openedBug, false, true, false, false, false
		}
		// Backlog if created and dev not started as of cutoff
		return openedBug, true, false, false, false, false
	}
	// Aggregate per week per project
	byWeekProj := map[wk]map[string]rec{}
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 7) {
		// Sunday end-of-day cutoff: Monday+6 days 23:59:59.999...
		cutoff := time.Date(cur.Year(), cur.Month(), cur.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC).AddDate(0, 0, 6)
		y, w := cur.ISOWeek()
		projMap := map[string]rec{}
		for _, r := range rows {
			ob, ib, id, ir, iq, iw := stageAt(r, cutoff)
			if !(ob || ib || id || ir || iq || iw) {
				continue
			}
			k := r.ProjectID + "\u0000" + r.ProjectName
			rr := projMap[k]
			rr.ProjectID = r.ProjectID
			rr.ProjectName = r.ProjectName
			if ob {
				rr.Agg.OpenedBugs++
			}
			if ib {
				rr.Agg.InBacklogs++
			}
			if id {
				rr.Agg.InDev++
			}
			if ir {
				rr.Agg.InReview++
			}
			if iq {
				rr.Agg.InQA++
			}
			if iw {
				rr.Agg.WaitingToProd++
			}
			projMap[k] = rr
		}
		byWeekProj[wk{Year: y, Week: w}] = projMap
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
	headers := []string{"year", "week", "project_id", "project_name", "opened_bugs", "in_backlogs", "in_dev", "in_review", "in_qa", "waiting_to_prod"}
	if err := w.Write(headers); err != nil {
		return err
	}
	// stable iterate weeks then project keys
	// collect and sort week keys
	var weeks []wk
	for k := range byWeekProj {
		weeks = append(weeks, k)
	}
	sort.Slice(weeks, func(i, j int) bool {
		if weeks[i].Year != weeks[j].Year {
			return weeks[i].Year < weeks[j].Year
		}
		return weeks[i].Week < weeks[j].Week
	})
	for _, k := range weeks {
		projMap := byWeekProj[k]
		var pkeys []string
		for pk := range projMap {
			pkeys = append(pkeys, pk)
		}
		sort.Strings(pkeys)
		for _, pk := range pkeys {
			rec := projMap[pk]
			row := []string{
				fmt.Sprintf("%d", k.Year),
				fmt.Sprintf("%02d", k.Week),
				rec.ProjectID,
				rec.ProjectName,
				fmt.Sprintf("%d", rec.Agg.OpenedBugs),
				fmt.Sprintf("%d", rec.Agg.InBacklogs),
				fmt.Sprintf("%d", rec.Agg.InDev),
				fmt.Sprintf("%d", rec.Agg.InReview),
				fmt.Sprintf("%d", rec.Agg.InQA),
				fmt.Sprintf("%d", rec.Agg.WaitingToProd),
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}
	return w.Error()
}
