package github

import "time"

// Repo represents a GitHub repository (minimal fields used by the collector)
type Repo struct {
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Private bool `json:"private"`
}

// Issue represents a GitHub issue (excluding PRs which have PullRequest != nil)
type Issue struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	HTMLURL     string     `json:"html_url"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	User        *User      `json:"user"`
	Assignees   []User     `json:"assignees"`
	Labels      []Label    `json:"labels"`
	PullRequest *struct{}  `json:"pull_request"`
}

type User struct {
	Login string `json:"login"`
}

type Label struct {
	Name string `json:"name"`
}

// TimelineEvent captures various events, including project card movements
// Note: Only fields used by the collector are modeled
type TimelineEvent struct {
	Event         string         `json:"event"`
	CreatedAt     time.Time      `json:"created_at"`
	Actor         *User          `json:"actor"`
	ProjectCard   *ProjectCard   `json:"project_card"`
	ProjectColumn *ProjectColumn `json:"project_column"`
	Project       *Project       `json:"project"`
	// For moved events, GitHub often provides these names
	ProjectColumnName         string `json:"project_column_name"`
	PreviousProjectColumnName string `json:"previous_project_column_name"`
}

type ProjectCard struct {
	ID        int64  `json:"id"`
	ColumnID  int64  `json:"column_id"`
	URL       string `json:"url"`
	ColumnURL string `json:"column_url"`
}

type ProjectColumn struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ProjectID int64  `json:"project_id"`
}

type Project struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// StatusEvent is an aggregation artifact for issue open/close/reopen events
type StatusEvent struct {
	Type string    `json:"type"` // opened|closed|reopened
	At   time.Time `json:"at"`
	By   string    `json:"by,omitempty"`
}

// ProjectMoveEvent captures added/moved/removed events within classic Projects
type ProjectMoveEvent struct {
	ProjectID   int64     `json:"project_id"`
	ProjectName string    `json:"project_name,omitempty"`
	FromColumn  string    `json:"from_column,omitempty"`
	ToColumn    string    `json:"to_column,omitempty"`
	At          time.Time `json:"at"`
	By          string    `json:"by,omitempty"`
	Type        string    `json:"type"` // added|moved|removed
}

// IssueReport is the final aggregated record for output
type IssueReport struct {
	Org             string             `json:"org"`
	Repo            string             `json:"repo"`
	Number          int                `json:"number"`
	Title           string             `json:"title"`
	URL             string             `json:"url"`
	State           string             `json:"state"`
	IsBug           bool               `json:"is_bug"`
	Creator         string             `json:"creator"`
	Assignees       []string           `json:"assignees"`
	CreatedAt       time.Time          `json:"created_at"`
	ClosedAt        *time.Time         `json:"closed_at,omitempty"`
	Committer       string             `json:"committer,omitempty"`
	StatusHistory   []StatusEvent      `json:"status_history"`
	ProjectHistory  []ProjectMoveEvent `json:"project_history"`
	CurrentProjects []CurrentProject   `json:"current_projects"`
}

type CurrentProject struct {
	ProjectID   int64  `json:"project_id"`
	ProjectName string `json:"project_name,omitempty"`
	ColumnID    int64  `json:"column_id,omitempty"`
	ColumnName  string `json:"column_name,omitempty"`
}
