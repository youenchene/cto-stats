package config

// Config represents the structure of config.yaml used by the tool.
// Only the fields currently needed by commands are modeled.
type Config struct {
	GitHub struct {
		Org      string    `yaml:"org"`
		Projects []Project `yaml:"projects"`
	} `yaml:"github"`
	CloudSpending struct {
		Services []string `yaml:"services"`
	} `yaml:"cloud_spending"`
}

type Project struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	Exclude bool     `yaml:"exclude"`
	Types   []string `yaml:"types"`

	LeadTimeColumns        []string `yaml:"lead_time_columns"`
	CycleTimeColumns       []string `yaml:"cycle_time_columns"`
	DevStartColumns        []string `yaml:"dev_start_columns"`
	ReviewStartColumns     []string `yaml:"review_start_columns"`
	QAStartColumns         []string `yaml:"qa_start_columns"`
	WaitingToProdStartCols []string `yaml:"waitingtoprod_start_columns"`
	InProdStartColumns     []string `yaml:"inprod_start_columns"`
}
