package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"cto-stats/domain/cloudspending"
)

// Client handles GCP Cloud Billing API requests
type Client struct {
	projectID      string
	billingAccount string
	location       string
	httpClient     *http.Client
}

// NewClient creates a new GCP Cloud Billing API client
func NewClient(projectID, billingAccount, serviceAccountJSON string, location string) *Client {
	// Build an OAuth2-enabled HTTP client using ADC or the provided service account JSON
	ctx := context.Background()
	scopes := []string{
		"https://www.googleapis.com/auth/cloud-billing.readonly",
		"https://www.googleapis.com/auth/bigquery.readonly",
	}

	var creds *google.Credentials
	var err error

	// Accept either raw JSON or a path to a JSON file; if empty, fall back to ADC
	if strings.TrimSpace(serviceAccountJSON) != "" {
		var keyJSON []byte
		s := strings.TrimSpace(serviceAccountJSON)
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			keyJSON = []byte(s)
		} else {
			// Treat as a filesystem path
			if b, readErr := os.ReadFile(s); readErr == nil {
				keyJSON = b
			} else {
				// If reading fails, leave keyJSON nil to try ADC below
				keyJSON = nil
			}
		}
		if len(keyJSON) > 0 {
			creds, err = google.CredentialsFromJSON(ctx, keyJSON, scopes...)
		}
	}

	if creds == nil || err != nil {
		// Fallback to ADC (e.g., GOOGLE_APPLICATION_CREDENTIALS or metadata server)
		creds, _ = google.FindDefaultCredentials(ctx, scopes...)
	}

	var httpClient *http.Client
	if creds != nil {
		httpClient = oauth2.NewClient(ctx, creds.TokenSource)
	} else {
		// Last resort: unauthenticated client (requests will fail), but keep timeout
		httpClient = &http.Client{}
	}
	httpClient.Timeout = 30 * time.Second

	return &Client{
		projectID:      projectID,
		billingAccount: billingAccount,
		location:       strings.TrimSpace(location),
		httpClient:     httpClient,
	}
}

// Note: Authentication is now handled by the oauth2 transport in httpClient

// bigQueryRequest represents a BigQuery query request
type bigQueryRequest struct {
	Query        string `json:"query"`
	UseLegacySQL bool   `json:"useLegacySQL"`
	MaxResults   int    `json:"maxResults,omitempty"`
	TimeoutMs    int    `json:"timeoutMs,omitempty"`
	Location     string `json:"location,omitempty"`
}

// bigQueryResponse represents a BigQuery query response
type bigQueryResponse struct {
	Schema struct {
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	} `json:"schema"`
	Rows []struct {
		F []struct {
			V interface{} `json:"v"`
		} `json:"f"`
	} `json:"rows"`
	TotalRows string `json:"totalRows"`
}

// FetchCosts retrieves cost data grouped by service for the last N months
func (c *Client) FetchCosts(ctx context.Context) ([]cloudspending.CostRecord, error) {

	// Format dates for BigQuery (YYYYMMDD)

	slog.Info("phase.gcp.costs.fetch.start")
	// Build BigQuery SQL query
	// Note: This assumes billing export is set up to BigQuery
	// Table format: PROJECT_ID.DATASET.gcp_billing_export_v1_BILLING_ACCOUNT_ID
	query := fmt.Sprintf(`
		SELECT
			FORMAT_DATE('%%Y%%m01', DATE(usage_start_time)) AS month,
			service.description AS service_name,
			SUM(cost) AS total_cost,
			currency
		FROM
			`+"`%[1]s.billing_export.gcp_billing_export_*`"+`
		GROUP BY
			month, service_name, currency
		ORDER BY
			month, service_name
	`, c.projectID)

	reqBody := bigQueryRequest{
		Query:        query,
		UseLegacySQL: false,
		MaxResults:   10000,
		TimeoutMs:    30000,
	}
	if c.location != "" {
		reqBody.Location = c.location
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make BigQuery API request
	url := fmt.Sprintf("https://bigquery.googleapis.com/bigquery/v2/projects/%s/queries", c.projectID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch costs: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// include small snippet of query and location for diagnostics
		snippet := query
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("API request failed: %d %s (location=%s, query~=%q)", resp.StatusCode, string(body), c.location, snippet)
	}

	var queryResp bigQueryResponse
	if err := json.Unmarshal(body, &queryResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Parse response into CostRecords
	return c.parseResponse(&queryResp, string(body))
}

// parseResponse converts BigQuery response to CostRecord slice
func (c *Client) parseResponse(resp *bigQueryResponse, rawData string) ([]cloudspending.CostRecord, error) {
	// Find column indices
	monthIdx := -1
	serviceIdx := -1
	costIdx := -1
	currencyIdx := -1

	for i, field := range resp.Schema.Fields {
		switch field.Name {
		case "month":
			monthIdx = i
		case "service_name":
			serviceIdx = i
		case "total_cost":
			costIdx = i
		case "currency":
			currencyIdx = i
		}
	}

	if monthIdx == -1 || serviceIdx == -1 || costIdx == -1 {
		return nil, fmt.Errorf("missing required columns in response")
	}

	var records []cloudspending.CostRecord
	for _, row := range resp.Rows {
		if len(row.F) <= monthIdx || len(row.F) <= serviceIdx || len(row.F) <= costIdx {
			continue
		}

		// Parse month (YYYYMM01)
		monthStr, ok := row.F[monthIdx].V.(string)
		if !ok {
			continue
		}
		monthTime, err := time.Parse("20060102", monthStr)
		if err != nil {
			continue
		}

		// Parse service name
		service, ok := row.F[serviceIdx].V.(string)
		if !ok {
			continue
		}

		// Parse cost (can be string or number)
		var cost float64
		switch v := row.F[costIdx].V.(type) {
		case float64:
			cost = v
		case string:
			if _, err := fmt.Sscanf(v, "%f", &cost); err != nil {
				continue
			}
		default:
			continue
		}

		// Parse currency
		currency := "USD"
		if currencyIdx >= 0 && len(row.F) > currencyIdx {
			if curr, ok := row.F[currencyIdx].V.(string); ok && curr != "" {
				currency = curr
			}
		}

		records = append(records, cloudspending.CostRecord{
			Provider: "gcp",
			Service:  service,
			Month:    monthTime,
			Cost:     cost,
			Currency: currency,
			RawData:  rawData,
		})
	}

	return records, nil
}
