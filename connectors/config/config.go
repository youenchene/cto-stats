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
	CloudSpending struct {
		// Flat list of services to include (legacy/simple mode)
		Services []string `yaml:"services"`
		// Grouped services: each logical name aggregates several concrete services
		DetailedService []DetailedServiceGroup `yaml:"detailed_service"`
		// Compared services: list of comparisons between two groups of services
		ComparedService []ComparedService `yaml:"compared_service"`
	} `yaml:"cloud_spending"`
	// Backward/forward compatibility alias to support alternate YAML shape:
	// cloudspending:
	//   detailed_service:
	//     - name: "AI"
	//       services: ["Vertex AI", "Claude Sonnet 4.5"]
	//   compared_service:
	//     - name: "Old vs New"
	//       groups: [...]
	//   or (legacy flat list): ["Vertex AI", "Compute Engine", ...]
	// If provided, we map it to CloudSpending.DetailedService or Services so downstream code keeps working.
	CloudSpendingAlt struct {
		DetailedService any               `yaml:"detailed_service"`
		ComparedService []ComparedService `yaml:"compared_service"`
	} `yaml:"cloudspending"`
}

// DetailedServiceGroup defines a logical group name and the list of concrete services it aggregates.
type DetailedServiceGroup struct {
	Name     string   `yaml:"name"`
	Services []string `yaml:"services"`
}

// ComparedService defines a comparison between several groups of services.
type ComparedService struct {
	Name   string                 `yaml:"name"`
	Groups []DetailedServiceGroup `yaml:"groups"`
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
	// Normalize alternate section into canonical fields
	if c.CloudSpendingAlt.DetailedService != nil {
		switch v := c.CloudSpendingAlt.DetailedService.(type) {
		case []any:
			// Could be list of strings or list of objects
			// Try as list of objects first
			groups := make([]DetailedServiceGroup, 0)
			stringsOnly := make([]string, 0)
			for _, it := range v {
				switch vv := it.(type) {
				case map[string]any:
					name, _ := vv["name"].(string)
					// services could be []any or []string
					var sv []string
					if raw, ok := vv["services"]; ok {
						if arr, ok := raw.([]any); ok {
							for _, e := range arr {
								if s, ok := e.(string); ok {
									sv = append(sv, s)
								}
							}
						} else if arrs, ok := raw.([]string); ok {
							sv = append(sv, arrs...)
						}
					}
					if name != "" && len(sv) > 0 {
						groups = append(groups, DetailedServiceGroup{Name: name, Services: sv})
					}
				case string:
					stringsOnly = append(stringsOnly, vv)
				}
			}
			if len(groups) > 0 {
				c.CloudSpending.DetailedService = groups
			} else if len(stringsOnly) > 0 && len(c.CloudSpending.Services) == 0 {
				c.CloudSpending.Services = append([]string(nil), stringsOnly...)
			}
		case []string:
			if len(c.CloudSpending.Services) == 0 {
				c.CloudSpending.Services = append([]string(nil), v...)
			}
		}
	}
	// If only the new canonical grouped field is provided under cloud_spending, keep as is.
	if len(c.CloudSpendingAlt.ComparedService) > 0 {
		c.CloudSpending.ComparedService = c.CloudSpendingAlt.ComparedService
	}
	slog.Info(fmt.Sprintf("Loaded config: %s", path))
	return &c, nil
}
