package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"cto-stats/domain/cloudspending"
)

// Client handles Azure Cost Management API requests
type Client struct {
	subscriptionID string
	tenantID       string
	clientID       string
	clientSecret   string
	httpClient     *http.Client
	token          string
	tokenExpiry    time.Time
}

// NewClient creates a new Azure Cost Management API client
func NewClient(subscriptionID, tenantID, clientID, clientSecret string) *Client {
	return &Client{
		subscriptionID: subscriptionID,
		tenantID:       tenantID,
		clientID:       clientID,
		clientSecret:   clientSecret,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// tokenResponse represents the OAuth2 token response from Azure AD
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// authenticate obtains an access token from Azure AD
func (c *Client) authenticate(ctx context.Context) error {
	// Skip if token is still valid
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return nil
	}

	url := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", c.tenantID)
	data := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s&scope=https://management.azure.com/.default",
		c.clientID, c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBufferString(data))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to authenticate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authentication failed: %d %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	c.token = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return nil
}

// costQueryRequest represents the request body for Azure Cost Management Query API
type costQueryRequest struct {
	Type       string         `json:"type"`
	Timeframe  string         `json:"timeframe"`
	TimePeriod *timePeriod    `json:"timePeriod,omitempty"`
	Dataset    datasetRequest `json:"dataset"`
}

type timePeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type datasetRequest struct {
	Granularity string            `json:"granularity"`
	Aggregation map[string]aggDef `json:"aggregation"`
	Grouping    []groupingDef     `json:"grouping"`
}

type aggDef struct {
	Name     string `json:"name"`
	Function string `json:"function"`
}

type groupingDef struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// costQueryResponse represents the response from Azure Cost Management Query API
type costQueryResponse struct {
	Properties struct {
		Columns []columnDef `json:"columns"`
		Rows    [][]any     `json:"rows"`
	} `json:"properties"`
}

type columnDef struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// FetchCosts retrieves cost data grouped by service for the last N months
func (c *Client) FetchCosts(ctx context.Context, months int) ([]cloudspending.CostRecord, error) {
	if err := c.authenticate(ctx); err != nil {
		return nil, err
	}

	// Calculate date range (last N months)
	to := time.Now()
	from := to.AddDate(0, -months, 0)

	// Format dates as YYYY-MM-DD
	fromStr := from.Format("2006-01-02")
	toStr := to.Format("2006-01-02")

	// Build query request
	reqBody := costQueryRequest{
		Type:      "ActualCost",
		Timeframe: "Custom",
		TimePeriod: &timePeriod{
			From: fromStr,
			To:   toStr,
		},
		Dataset: datasetRequest{
			Granularity: "Monthly",
			Aggregation: map[string]aggDef{
				"totalCost": {
					Name:     "Cost",
					Function: "Sum",
				},
			},
			Grouping: []groupingDef{
				{Type: "Dimension", Name: "ServiceName"},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make API request
	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.CostManagement/query?api-version=2023-03-01",
		c.subscriptionID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
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
		return nil, fmt.Errorf("API request failed: %d %s", resp.StatusCode, string(body))
	}

	var queryResp costQueryResponse
	if err := json.Unmarshal(body, &queryResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Parse response into CostRecords
	return c.parseResponse(&queryResp, string(body))
}

// parseResponse converts Azure API response to CostRecord slice
func (c *Client) parseResponse(resp *costQueryResponse, rawData string) ([]cloudspending.CostRecord, error) {
	// Find column indices
	costIdx := -1
	currencyIdx := -1
	serviceIdx := -1
	dateIdx := -1

	for i, col := range resp.Properties.Columns {
		switch col.Name {
		case "Cost":
			costIdx = i
		case "Currency":
			currencyIdx = i
		case "ServiceName":
			serviceIdx = i
		case "BillingMonth":
			dateIdx = i
		}
	}

	if costIdx == -1 || serviceIdx == -1 || dateIdx == -1 {
		return nil, fmt.Errorf("missing required columns in response")
	}

	var records []cloudspending.CostRecord
	for _, row := range resp.Properties.Rows {
		if len(row) <= costIdx || len(row) <= serviceIdx || len(row) <= dateIdx {
			continue
		}

		// Parse cost
		cost, ok := row[costIdx].(float64)
		if !ok {
			continue
		}

		// Parse service name
		service, ok := row[serviceIdx].(string)
		if !ok {
			continue
		}

		// Parse date (format: YYYYMMDD or YYYY-MM-DD)
		dateVal, ok := row[dateIdx].(float64)
		if !ok {
			dateStr, ok := row[dateIdx].(string)
			if !ok {
				continue
			}
			// Parse string date
			monthTime, err := time.Parse("20060102", dateStr)
			if err != nil {
				monthTime, err = time.Parse("2006-01-02", dateStr)
				if err != nil {
					continue
				}
			}

			currency := "USD"
			if currencyIdx >= 0 && len(row) > currencyIdx {
				if curr, ok := row[currencyIdx].(string); ok {
					currency = curr
				}
			}

			records = append(records, cloudspending.CostRecord{
				Provider: "azure",
				Service:  service,
				Month:    monthTime,
				Cost:     cost,
				Currency: currency,
				RawData:  rawData,
			})
			continue
		}

		// Parse numeric date (YYYYMMDD as float)
		dateStr := fmt.Sprintf("%.0f", dateVal)
		monthTime, err := time.Parse("20060102", dateStr)
		if err != nil {
			continue
		}

		currency := "USD"
		if currencyIdx >= 0 && len(row) > currencyIdx {
			if curr, ok := row[currencyIdx].(string); ok {
				currency = curr
			}
		}

		records = append(records, cloudspending.CostRecord{
			Provider: "azure",
			Service:  service,
			Month:    monthTime,
			Cost:     cost,
			Currency: currency,
			RawData:  rawData,
		})
	}

	return records, nil
}
