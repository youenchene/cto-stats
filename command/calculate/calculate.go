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
	PutInReadyStartDatetime   *time.Time
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
	issuesScope := fs.Bool("issues", false, "Process issues scope: calculate issue-based KPIs (cycle time, throughput, stocks)")
	prScope := fs.Bool("pr", false, "Process pull-requests scope: change-requests KPIs only")
	cloudSpendingScope := fs.Bool("cloudspending", false, "Process cloud spending scope: aggregate cost data")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Cloud spending scope is independent
	if *cloudSpendingScope {
		return runCloudSpendingCalculate()
	}

	// Backward compatibility: if no scope specified, process both
	if !*issuesScope && !*prScope {
		*issuesScope = true
		*prScope = true
	}

	// Read config path from environment variable CONFIG_PATH; default to ./config.yml
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}

	var projCfgByID map[string]config.Project
	projCfgByID = map[string]config.Project{}
	if *issuesScope {
		// For issues calculations, a config file is required for project mappings
		if _, err := os.Stat(cfgPath); err != nil {
			return fmt.Errorf("calculate: config file required for --issues (set CONFIG_PATH or provide ./config.yml): %w", err)
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("calculate: failed to load config: %w", err)
		}
		// Build a project lookup by ID for quick access
		for _, p := range cfg.GitHub.Projects {
			projCfgByID[p.ID] = p
		}
	}

	// Read inputs from data/
	base := "data"

	var (
		issues     map[string]issueRow
		statusByID map[string][]statusEventRow
		projByID   map[string][]projectEventRow
	)
	var err error
	if *issuesScope {
		issues, err = readIssues(filepath.Join(base, "issue.csv"))
		if err != nil {
			return err
		}
		statusByID, err = readStatus(filepath.Join(base, "issue_status_event.csv"))
		if err != nil {
			return err
		}
		projByID, err = readProject(filepath.Join(base, "issue_project_event.csv"))
		if err != nil {
			return err
		}
	}

	// Build output
	var allIssues []calculatedIssue
	if *issuesScope {
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
				row.PutInReadyStartDatetime = choose(pc.PutInReadyColumns)

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
				row.PutInReadyStartDatetime = firstMoveToAny(projEvents, []string{"Ready", "In Ready", "Ready for Dev"})
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
	}

	// PR scope calculations (do not require config)
	if *prScope {
		// weekly PR change-requests stats (avg, median, p90) by PR open week
		if err := writePRChangeRequestsWeekly(filepath.Join(base, "pr_change_requests_week.csv"), base); err != nil {
			return err
		}
		// per-repo PR change-requests stats (median per repo) and distribution
		if err := writePRChangeRequestsPerRepo(filepath.Join(base, "pr_change_requests_repo.csv"), base); err != nil {
			return err
		}
		if err := writePRChangeRequestsRepoDist(filepath.Join(base, "pr_change_requests_repo_dist.csv"), base); err != nil {
			return err
		}
	}

	if *issuesScope {
		slog.Info(fmt.Sprintf("calculate.done (issues)"))
	}
	if *prScope {
		slog.Info(fmt.Sprintf("calculate.done (pr)"))
	}
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
	headers := []string{"id", "name", "project_id", "project_name", "creationdatetime", "leadtimestartdatetime", "cycletimestartdatetime", "putinreadystartdatetime", "devstartdatetime", "reviewstartdatetime", "qastartdatetime", "waitingtopodstartdateime", "enddatetime", "bug", "type"}
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
			formatTime(r.PutInReadyStartDatetime),
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
		TimeToPRAvg  float64
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
		var tprSum float64
		var tprCnt int
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
			// Time to PR = review_start - dev_start (in days)
			if r.DevStartDatetime != nil && r.ReviewStartDatetime != nil {
				dev := r.DevStartDatetime.UTC()
				rev := r.ReviewStartDatetime.UTC()
				if !rev.Before(dev) {
					tpr := rev.Sub(dev).Hours() / 24.0
					tprSum += tpr
					tprCnt++
				}
			}
		}
		var leadAvg, cycleAvg, tprAvg float64
		if leadCnt > 0 {
			leadAvg = leadSum / float64(leadCnt)
		}
		if cycleCnt > 0 {
			cycleAvg = cycleSum / float64(cycleCnt)
		}
		if tprCnt > 0 {
			tprAvg = tprSum / float64(tprCnt)
		}
		outs = append(outs, outRow{Month: m, IssueCount: len(issues), LeadDaysAvg: leadAvg, LeadCount: leadCnt, CycleDaysAvg: cycleAvg, CycleCount: cycleCnt, TimeToPRAvg: tprAvg})
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
	headers := []string{"month", "issues_count", "leadtime_days_avg", "lead_count", "cycletime_days_avg", "cycle_count", "time_to_pr"}
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
			fmt.Sprintf("%.6f", r.TimeToPRAvg),
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
	// Remove the last week (current week) from the output
	if len(keys) > 0 {
		keys = keys[:len(keys)-1]
		centers = centers[:len(centers)-1]
		ucls = ucls[:len(ucls)-1]
		lcls = lcls[:len(lcls)-1]
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
		InReady       int
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
	stageFlags := func(r calculatedIssue) (openedBug bool, inBacklog bool, inReady bool, inDev bool, inReview bool, inQA bool, waiting bool) {
		if r.EndDatetime != nil {
			return false, false, false, false, false, false, false
		}
		openedBug = r.Bug
		// Stage logic: furthest known stage wins (exclusive buckets)
		if r.WaitingToPodStartDatetime != nil {
			return openedBug, false, false, false, false, false, true
		}
		if r.QAStartDatetime != nil {
			return openedBug, false, false, false, true, false, false
		}
		if r.ReviewStartDatetime != nil {
			return openedBug, false, false, true, false, false, false
		}
		if r.DevStartDatetime != nil {
			return openedBug, false, false, true, false, false, false
		}
		if r.PutInReadyStartDatetime != nil {
			return openedBug, false, true, false, false, false, false
		}
		// Backlog (only early dates present: creation/lead/cycle)
		return openedBug, true, false, false, false, false, false
	}
	for _, r := range rows {
		if r.EndDatetime != nil {
			continue // only not-closed
		}
		key := r.ProjectID + "\u0000" + r.ProjectName
		rec := byProj[key]
		rec.ProjectID = r.ProjectID
		rec.ProjectName = r.ProjectName
		ob, ib, iready, id, ir, iq, iw := stageFlags(r)
		if ob {
			rec.Agg.OpenedBugs++
		}
		if ib {
			rec.Agg.InBacklogs++
		}
		if iready {
			rec.Agg.InReady++
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
	headers := []string{"project_id", "project_name", "opened_bugs", "in_backlogs", "in_ready", "in_dev", "in_review", "in_qa", "waiting_to_prod"}
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
			fmt.Sprintf("%d", rec.Agg.InReady),
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
		cands := []*time.Time{r.LeadTimeStartDatetime, r.CycleTimeStartDatetime, r.PutInReadyStartDatetime, r.DevStartDatetime, r.ReviewStartDatetime, r.QAStartDatetime, r.WaitingToPodStartDatetime, r.EndDatetime}
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
		headers := []string{"year", "week", "project_id", "project_name", "opened_bugs", "in_backlogs", "in_ready", "in_dev", "in_review", "in_qa", "waiting_to_prod"}
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
	type agg struct{ OpenedBugs, InBacklogs, InReady, InDev, InReview, InQA, WaitingToProd int }
	type rec struct {
		ProjectID, ProjectName string
		Agg                    agg
	}
	// Helper: determine stage at cutoff
	stageAt := func(r calculatedIssue, cutoff time.Time) (openedBug bool, inBacklog bool, inReady bool, inDev bool, inReview bool, inQA bool, waiting bool) {
		cu := cutoff
		// Not yet created
		if timeUTC(r.CreationDatetime).After(cu) {
			return false, false, false, false, false, false, false
		}
		// If ended before or at cutoff, it is not in stock
		if r.EndDatetime != nil && !r.EndDatetime.UTC().After(cu) {
			return false, false, false, false, false, false, false
		}
		openedBug = r.Bug
		// Helper to check ts <= cutoff
		le := func(t *time.Time) bool { return t != nil && !t.UTC().After(cu) }
		// Furthest stage reached as of cutoff (no later stage timestamp <= cutoff)
		// Waiting
		if le(r.WaitingToPodStartDatetime) {
			return openedBug, false, false, false, false, false, true
		}
		// QA
		if le(r.QAStartDatetime) && !le(r.WaitingToPodStartDatetime) {
			return openedBug, false, false, false, true, false, false
		}
		// Review
		if le(r.ReviewStartDatetime) && !le(r.QAStartDatetime) && !le(r.WaitingToPodStartDatetime) {
			return openedBug, false, false, true, false, false, false
		}
		// Dev
		if le(r.DevStartDatetime) && !le(r.ReviewStartDatetime) && !le(r.QAStartDatetime) && !le(r.WaitingToPodStartDatetime) {
			return openedBug, false, false, true, false, false, false
		}
		// In Ready
		if le(r.PutInReadyStartDatetime) && !le(r.DevStartDatetime) && !le(r.ReviewStartDatetime) && !le(r.QAStartDatetime) && !le(r.WaitingToPodStartDatetime) {
			return openedBug, false, true, false, false, false, false
		}
		// Backlog if created and no later stage as of cutoff
		return openedBug, true, false, false, false, false, false
	}
	// Aggregate per week per project
	byWeekProj := map[wk]map[string]rec{}
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 7) {
		// Sunday end-of-day cutoff: Monday+6 days 23:59:59.999...
		cutoff := time.Date(cur.Year(), cur.Month(), cur.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC).AddDate(0, 0, 6)
		y, w := cur.ISOWeek()
		projMap := map[string]rec{}
		for _, r := range rows {
			ob, ib, iready, id, ir, iq, iw := stageAt(r, cutoff)
			if !(ob || ib || iready || id || ir || iq || iw) {
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
			if iready {
				rr.Agg.InReady++
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
	headers := []string{"year", "week", "project_id", "project_name", "opened_bugs", "in_backlogs", "in_ready", "in_dev", "in_review", "in_qa", "waiting_to_prod"}
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
				fmt.Sprintf("%d", rec.Agg.InReady),
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

// PR change-requests weekly calculation
// Reads PRs from pr.csv and reviews from pr_review.csv in baseDir, computes per-week stats
// for PRs opened in each ISO week: average, median, and 90th percentile of the number of
// CHANGES_REQUESTED reviews per PR.
func writePRChangeRequestsWeekly(outPath string, baseDir string) error {
	// Collect PR created_at keyed by org/repo#number
	type pr struct {
		Org, Repo, Number string
		CreatedAt         time.Time
	}
	prs := map[string]pr{}
	// Open unified PR file
	prPath := filepath.Join(baseDir, "pr.csv")
	if f, err := os.Open(prPath); err == nil {
		defer f.Close()
		r := csv.NewReader(f)
		head, err := r.Read()
		if err == nil {
			idx := indexMap(head)
			required := []string{"org", "repo", "number", "created_at"}
			for _, col := range required {
				if _, ok := idx[col]; !ok {
					return fmt.Errorf("pr.csv missing column %s", col)
				}
			}
			for {
				rec, err := r.Read()
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					return err
				}
				org := rec[idx["org"]]
				repo := rec[idx["repo"]]
				num := rec[idx["number"]]
				created, _ := time.Parse(time.RFC3339, rec[idx["created_at"]])
				k := key(org, repo, num)
				prs[k] = pr{Org: org, Repo: repo, Number: num, CreatedAt: created}
			}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// If pr.csv doesn't exist, write empty output headers and return
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		defer w.Flush()
		if err := w.Write([]string{"year", "week", "repo", "avg", "median", "p90", "pr_count", "cr_total"}); err != nil {
			return err
		}
		return w.Error()
	} else {
		return err
	}
	// Read reviews and count CHANGES_REQUESTED per PR
	reqCount := map[string]int{}
	{
		path := filepath.Join(baseDir, "pr_review.csv")
		f, err := os.Open(path)
		if err == nil {
			defer f.Close()
			r := csv.NewReader(f)
			head, err := r.Read()
			if err == nil {
				idx := indexMap(head)
				for {
					rec, err := r.Read()
					if err != nil {
						if err.Error() == "EOF" {
							break
						}
						return err
					}
					org := rec[idx["org"]]
					repo := rec[idx["repo"]]
					num := rec[idx["number"]]
					state := strings.TrimSpace(strings.ToUpper(rec[idx["state"]]))
					if state == "CHANGES_REQUESTED" {
						k := key(org, repo, num)
						reqCount[k]++
					}
				}
			}
		}
	}
	// Group PRs by ISO week of CreatedAt and repo
	type wk struct{ Year, Week int }
	byWeekRepo := map[wk]map[string][]int{}
	for _, p := range prs {
		y, w := p.CreatedAt.UTC().ISOWeek()
		cnt := reqCount[key(p.Org, p.Repo, p.Number)]
		k := wk{Year: y, Week: w}
		m := byWeekRepo[k]
		if m == nil {
			m = map[string][]int{}
			byWeekRepo[k] = m
		}
		m[p.Repo] = append(m[p.Repo], cnt)
	}
	// Prepare ordered week keys
	var weeks []wk
	for k := range byWeekRepo {
		weeks = append(weeks, k)
	}
	sort.Slice(weeks, func(i, j int) bool {
		if weeks[i].Year != weeks[j].Year {
			return weeks[i].Year < weeks[j].Year
		}
		return weeks[i].Week < weeks[j].Week
	})
	// Write CSV
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"year", "week", "repo", "avg", "median", "p90", "pr_count", "cr_total"}); err != nil {
		return err
	}
	// Helper to write a row given values
	writeVals := func(y int, week int, repo string, vals []int) error {
		if len(vals) == 0 {
			return nil
		}
		sort.Ints(vals)
		var sum int
		for _, v := range vals {
			sum += v
		}
		avg := float64(sum) / float64(len(vals))
		n := len(vals)
		var med float64
		if n%2 == 1 {
			med = float64(vals[n/2])
		} else {
			med = (float64(vals[n/2-1]) + float64(vals[n/2])) / 2.0
		}
		rank := int(math.Ceil(0.9 * float64(n)))
		if rank < 1 {
			rank = 1
		}
		if rank > n {
			rank = n
		}
		p90 := float64(vals[rank-1])
		row := []string{
			fmt.Sprintf("%d", y),
			fmt.Sprintf("%02d", week),
			repo,
			fmt.Sprintf("%.6f", avg),
			fmt.Sprintf("%.6f", med),
			fmt.Sprintf("%.6f", p90),
			fmt.Sprintf("%d", n),
			fmt.Sprintf("%d", sum),
		}
		return w.Write(row)
	}
	for _, k := range weeks {
		m := byWeekRepo[k]
		// Collect repos sorted
		var repos []string
		var all []int
		for repo, vals := range m {
			repos = append(repos, repo)
			all = append(all, vals...)
		}
		sort.Strings(repos)
		for _, repo := range repos {
			if err := writeVals(k.Year, k.Week, repo, m[repo]); err != nil {
				return err
			}
		}
		// Write ALL aggregate for line chart convenience
		if err := writeVals(k.Year, k.Week, "ALL", all); err != nil {
			return err
		}
	}
	return w.Error()
}

// PR change-requests per-repo calculation
// Reads PRs from pr.csv and reviews from pr_review.csv in baseDir, computes per-repo
// median number of CHANGES_REQUESTED per PR and writes one line per repo.
func writePRChangeRequestsPerRepo(outPath string, baseDir string) error {
	// Read PRs
	type pr struct{ Org, Repo, Number string }
	prsByRepo := map[string][]pr{}
	prPath := filepath.Join(baseDir, "pr.csv")
	f, err := os.Open(prPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return err
			}
			out, err := os.Create(outPath)
			if err != nil {
				return err
			}
			defer out.Close()
			w := csv.NewWriter(out)
			defer w.Flush()
			if err := w.Write([]string{"repo", "median", "pr_count", "cr_total"}); err != nil {
				return err
			}
			return w.Error()
		}
		return err
	}
	defer f.Close()
	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		return err
	}
	idx := indexMap(head)
	for {
		rec, err := r.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
		p := pr{Org: rec[idx["org"]], Repo: rec[idx["repo"]], Number: rec[idx["number"]]}
		prsByRepo[p.Repo] = append(prsByRepo[p.Repo], p)
	}
	// Read reviews -> count CHANGES_REQUESTED per PR
	reqCount := map[string]int{}
	{
		path := filepath.Join(baseDir, "pr_review.csv")
		if rf, err := os.Open(path); err == nil {
			defer rf.Close()
			rr := csv.NewReader(rf)
			head, err := rr.Read()
			if err == nil {
				idx := indexMap(head)
				for {
					rec, err := rr.Read()
					if err != nil {
						if err.Error() == "EOF" {
							break
						}
						return err
					}
					state := strings.TrimSpace(strings.ToUpper(rec[idx["state"]]))
					if state == "CHANGES_REQUESTED" {
						k := key(rec[idx["org"]], rec[idx["repo"]], rec[idx["number"]])
						reqCount[k]++
					}
				}
			}
		}
	}
	// Build counts per repo
	type stat struct {
		repo string
		vals []int
	}
	stats := make([]stat, 0, len(prsByRepo))
	for repo, list := range prsByRepo {
		var vals []int
		for _, p := range list {
			vals = append(vals, reqCount[key(p.Org, p.Repo, p.Number)])
		}
		stats = append(stats, stat{repo: repo, vals: vals})
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].repo < stats[j].repo })
	// Write CSV
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	w := csv.NewWriter(out)
	defer w.Flush()
	if err := w.Write([]string{"repo", "median", "pr_count", "cr_total"}); err != nil {
		return err
	}
	for _, s := range stats {
		vals := append([]int(nil), s.vals...)
		sort.Ints(vals)
		n := len(vals)
		if n == 0 {
			continue
		}
		sum := 0
		for _, v := range vals {
			sum += v
		}
		var med float64
		if n%2 == 1 {
			med = float64(vals[n/2])
		} else {
			med = (float64(vals[n/2-1]) + float64(vals[n/2])) / 2.0
		}
		row := []string{s.repo, fmt.Sprintf("%.6f", med), fmt.Sprintf("%d", n), fmt.Sprintf("%d", sum)}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// PR change-requests per-repo distribution
// Writes rows: repo, cr (number of change requests), pr_count (number of PRs with that count)
func writePRChangeRequestsRepoDist(outPath string, baseDir string) error {
	// Reuse the same reading of PRs
	type pr struct{ Org, Repo, Number string }
	var prs []pr
	prPath := filepath.Join(baseDir, "pr.csv")
	f, err := os.Open(prPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return err
			}
			out, err := os.Create(outPath)
			if err != nil {
				return err
			}
			defer out.Close()
			w := csv.NewWriter(out)
			defer w.Flush()
			if err := w.Write([]string{"repo", "cr", "pr_count"}); err != nil {
				return err
			}
			return w.Error()
		}
		return err
	}
	defer f.Close()
	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		return err
	}
	idx := indexMap(head)
	for {
		rec, err := r.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
		prs = append(prs, pr{Org: rec[idx["org"]], Repo: rec[idx["repo"]], Number: rec[idx["number"]]})
	}
	// Count CHANGES_REQUESTED per PR
	reqCount := map[string]int{}
	{
		path := filepath.Join(baseDir, "pr_review.csv")
		if rf, err := os.Open(path); err == nil {
			defer rf.Close()
			rr := csv.NewReader(rf)
			head, err := rr.Read()
			if err == nil {
				idx := indexMap(head)
				for {
					rec, err := rr.Read()
					if err != nil {
						if err.Error() == "EOF" {
							break
						}
						return err
					}
					state := strings.TrimSpace(strings.ToUpper(rec[idx["state"]]))
					if state == "CHANGES_REQUESTED" {
						k := key(rec[idx["org"]], rec[idx["repo"]], rec[idx["number"]])
						reqCount[k]++
					}
				}
			}
		}
	}
	// Build histogram per repo
	byRepo := map[string]map[int]int{}
	for _, p := range prs {
		cnt := reqCount[key(p.Org, p.Repo, p.Number)]
		m := byRepo[p.Repo]
		if m == nil {
			m = map[int]int{}
			byRepo[p.Repo] = m
		}
		m[cnt]++
	}
	// Order repos and cr keys
	repos := make([]string, 0, len(byRepo))
	for repo := range byRepo {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	w := csv.NewWriter(out)
	defer w.Flush()
	if err := w.Write([]string{"repo", "cr", "pr_count"}); err != nil {
		return err
	}
	for _, repo := range repos {
		m := byRepo[repo]
		// order cr ascending
		var crs []int
		for cr := range m {
			crs = append(crs, cr)
		}
		sort.Ints(crs)
		for _, cr := range crs {
			row := []string{repo, fmt.Sprintf("%d", cr), fmt.Sprintf("%d", m[cr])}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}
	return w.Error()
}

// runCloudSpendingCalculate aggregates cloud spending data
func runCloudSpendingCalculate() error {
	slog.Info("cloudspending.calculate.start")

	// Read config for service filter
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "./config.yml"
	}

	var serviceFilter []string
	var groups []config.DetailedServiceGroup
	if _, err := os.Stat(cfgPath); err == nil {
		cfg, err := config.Load(cfgPath)
		if err == nil {
			serviceFilter = cfg.CloudSpending.Services
			if len(cfg.CloudSpending.DetailedService) > 0 {
				groups = cfg.CloudSpending.DetailedService
			}
		}
	}

	// Read cloud costs CSV
	inputPath := filepath.Join("data", "cloud_costs.csv")
	records, err := readCloudCosts(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read cloud costs: %w", err)
	}

	if len(records) == 0 {
		slog.Warn("cloudspending.calculate.no_data")
		return fmt.Errorf("no cloud costs data found in %s", inputPath)
	}

	// Aggregate per provider per month
	monthlyPath := filepath.Join("data", "cloud_spending_monthly.csv")
	if err := writeCloudSpendingMonthly(monthlyPath, records); err != nil {
		return fmt.Errorf("failed to write monthly aggregation: %w", err)
	}
	slog.Info("cloudspending.calculate.monthly.done", "output", monthlyPath)

	// Aggregate per service group per month (if groups provided) or per service (filtered)
	servicesPath := filepath.Join("data", "cloud_spending_services.csv")
	if err := writeCloudSpendingServices(servicesPath, records, groups, serviceFilter); err != nil {
		return fmt.Errorf("failed to write services aggregation: %w", err)
	}
	slog.Info("cloudspending.calculate.services.done", "output", servicesPath)

	slog.Info("cloudspending.calculate.done")
	return nil
}

type cloudCostRecord struct {
	Provider string
	Service  string
	Month    time.Time
	Cost     float64
	Currency string
}

// readCloudCosts reads the cloud_costs.csv file
func readCloudCosts(path string) ([]cloudCostRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}

	idx := indexMap(header)
	// Determine optional currency column
	_, hasCurrency := idx["currency"]
	var records []cloudCostRecord

	for {
		row, err := r.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}

		month, err := time.Parse("2006-01-02", row[idx["month"]])
		if err != nil {
			continue
		}

		var cost float64
		fmt.Sscanf(row[idx["cost"]], "%f", &cost)

		// Fallback currency if not provided in CSV
		currency := ""
		if hasCurrency {
			currency = row[idx["currency"]]
		}
		records = append(records, cloudCostRecord{
			Provider: row[idx["provider"]],
			Service:  row[idx["service"]],
			Month:    month,
			Cost:     cost,
			Currency: currency,
		})
	}

	return records, nil
}

// writeCloudSpendingMonthly aggregates costs per provider per month
func writeCloudSpendingMonthly(path string, records []cloudCostRecord) error {
	// Aggregate by provider, month and currency to avoid mixing currencies
	type key struct {
		Provider string
		Month    string
		Currency string
	}
	agg := make(map[key]float64)

	for _, r := range records {
		k := key{
			Provider: r.Provider,
			Month:    r.Month.Format("2006-01"),
			Currency: strings.TrimSpace(r.Currency),
		}
		agg[k] += r.Cost
	}

	// Sort by month and provider
	type row struct {
		Month    string
		Provider string
		Cost     float64
		Currency string
	}
	var rows []row
	for k, cost := range agg {
		rows = append(rows, row{
			Month:    k.Month,
			Provider: k.Provider,
			Cost:     cost,
			Currency: k.Currency,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Month != rows[j].Month {
			return rows[i].Month < rows[j].Month
		}
		if rows[i].Provider != rows[j].Provider {
			return rows[i].Provider < rows[j].Provider
		}
		return rows[i].Currency < rows[j].Currency
	})

	// Write CSV
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"month", "provider", "cost", "currency"}); err != nil {
		return err
	}

	for _, r := range rows {
		if err := w.Write([]string{r.Month, r.Provider, fmt.Sprintf("%.2f", r.Cost), r.Currency}); err != nil {
			return err
		}
	}

	return w.Error()
}

// writeCloudSpendingServices aggregates costs per logical group per month if groups provided,
// else per service (optionally filtered by serviceFilter).
func writeCloudSpendingServices(path string, records []cloudCostRecord, groups []config.DetailedServiceGroup, serviceFilter []string) error {
	// Build quick lookup: service -> group name
	serviceToGroup := make(map[string]string)
	if len(groups) > 0 {
		for _, g := range groups {
			gname := strings.TrimSpace(g.Name)
			for _, s := range g.Services {
				serviceToGroup[strings.TrimSpace(s)] = gname
			}
		}
	}

	// Build filter set (only used when no groups are defined)
	filterSet := make(map[string]bool)
	for _, s := range serviceFilter {
		filterSet[strings.TrimSpace(s)] = true
	}

	// Aggregate by provider, groupOrService, month and currency
	type key struct {
		Provider string
		Name     string // group name or service name
		Month    string
		Currency string
	}
	agg := make(map[key]float64)

	for _, r := range records {
		month := r.Month.Format("2006-01")
		currency := strings.TrimSpace(r.Currency)
		name := r.Service
		if len(serviceToGroup) > 0 {
			// Skip services that are not part of any group
			gname, ok := serviceToGroup[r.Service]
			if !ok || gname == "" {
				continue
			}
			name = gname
		} else if len(filterSet) > 0 && !filterSet[r.Service] {
			// No groups: apply flat filter
			continue
		}

		k := key{
			Provider: r.Provider,
			Name:     name,
			Month:    month,
			Currency: currency,
		}
		agg[k] += r.Cost
	}

	// Sort
	type row struct {
		Month    string
		Provider string
		Name     string
		Cost     float64
		Currency string
	}
	var rows []row
	for k, cost := range agg {
		rows = append(rows, row{
			Month:    k.Month,
			Provider: k.Provider,
			Name:     k.Name,
			Cost:     cost,
			Currency: k.Currency,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Month != rows[j].Month {
			return rows[i].Month < rows[j].Month
		}
		if rows[i].Provider != rows[j].Provider {
			return rows[i].Provider < rows[j].Provider
		}
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Currency < rows[j].Currency
	})

	// Write CSV
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Header: if grouped, use "group" column; else keep legacy "service"
	if len(serviceToGroup) > 0 {
		if err := w.Write([]string{"month", "provider", "group", "cost", "currency"}); err != nil {
			return err
		}
		for _, r := range rows {
			if err := w.Write([]string{r.Month, r.Provider, r.Name, fmt.Sprintf("%.2f", r.Cost), r.Currency}); err != nil {
				return err
			}
		}
	} else {
		if err := w.Write([]string{"month", "provider", "service", "cost", "currency"}); err != nil {
			return err
		}
		for _, r := range rows {
			if err := w.Write([]string{r.Month, r.Provider, r.Name, fmt.Sprintf("%.2f", r.Cost), r.Currency}); err != nil {
				return err
			}
		}
	}

	return w.Error()
}
