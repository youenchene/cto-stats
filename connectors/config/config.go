package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the structure of config.yaml used by the tool.
// Only the fields currently needed by commands are modeled.
type Config struct {
	GitHub struct {
		Org      string    `yaml:"org"`
		Projects []Project `yaml:"projects"`
	} `yaml:"github"`
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
	PutInReadyColumns      []string `yaml:"put_in_ready_columns"`
	WaitingToProdStartCols []string `yaml:"waitingtoprod_start_columns"`
	InProdStartColumns     []string `yaml:"inprod_start_columns"`
}

// Load parses the YAML configuration file at path.
func Load(path string) (*Config, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	slog.Info(fmt.Sprintf("Loaded config: %s", path))
	return &c, nil
}
