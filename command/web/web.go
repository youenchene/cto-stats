package web

import (
	"encoding/csv"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// Run starts a small Echo web server exposing CSV-as-JSON APIs and an optional SPA dashboard.
//
// Usage:
//
//	github-stats web [-addr :8080] [-data ./data] [-ui ./ui/dist]
//
// Endpoints:
//
//	GET /api/cycle_times          -> <data>/cycle_time.csv
//	GET /api/stocks               -> <data>/stocks.csv
//	GET /api/stocks/week          -> <data>/stocks_week.csv
//	GET /api/throughtput/week     -> <data>/throughput_week.csv (404 if missing)
//
// When -ui points to a built Vite app (index.html exists), static files are served at / and
// unknown routes fall back to index.html for SPA routing.
func Run(args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "http listen address (host:port)")
	dataDir := fs.String("data", "./data", "directory containing CSV files")
	uiDir := fs.String("ui", "./ui/dist", "directory containing built UI (Vite dist)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	e := echo.New()

	// Helper to register a GET endpoint serving a specific CSV file
	serveCSV := func(route string, filename string) {
		e.GET(route, func(c echo.Context) error {
			path := filepath.Join(*dataDir, filename)
			rows, err := readCSV(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return c.JSON(http.StatusNotFound, map[string]any{
						"error":   "file not found",
						"path":    path,
						"message": "CSV file is missing",
					})
				}
				return c.JSON(http.StatusInternalServerError, map[string]any{
					"error":   err.Error(),
					"path":    path,
					"message": "failed to read CSV",
				})
			}
			return c.JSON(http.StatusOK, rows)
		})
	}

	// APIs
	serveCSV("/api/cycle_times", "cycle_time.csv")
	serveCSV("/api/stocks", "stocks.csv")
	serveCSV("/api/stocks/week", "stocks_week.csv")
	serveCSV("/api/throughput/week", "throughput_week.csv")
	serveCSV("/api/pr/change_requests", "pr_change_requests_week.csv")
	serveCSV("/api/pr/change_requests/repo", "pr_change_requests_repo.csv")
	serveCSV("/api/pr/change_requests/repo_dist", "pr_change_requests_repo_dist.csv")
	serveCSV("/api/cloud_spending/monthly", "cloud_spending_monthly.csv")
	serveCSV("/api/cloud_spending/services", "cloud_spending_services.csv")
	serveCSV("/api/cloud_spending/compared", "cloud_spending_compared.csv")

	// Static UI (optional)
	indexPath := filepath.Join(*uiDir, "index.html")
	if fi, err := os.Stat(indexPath); err == nil && !fi.IsDir() {
		// Serve built assets under /
		e.Static("/", *uiDir)
		// Root path -> index.html
		e.GET("/", func(c echo.Context) error { return c.File(indexPath) })

		// Fallback to index.html for non-API 404s (SPA routing) while keeping static assets working
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			// If it's a 404 and not under /api, serve the SPA index instead
			if he, ok := err.(*echo.HTTPError); ok && he.Code == http.StatusNotFound {
				p := c.Request().URL.Path
				if !strings.HasPrefix(p, "/api") {
					// Try to serve index.html
					_ = c.File(indexPath)
					return
				}
			}
			e.DefaultHTTPErrorHandler(err, c)
		}
	}

	return e.Start(*addr)
}

// readCSV loads a CSV file and returns a slice of objects keyed by headers.
// Values are kept as strings to avoid lossy or incorrect type coercion.
func readCSV(path string) ([]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	// Read all rows; CSVs are expected to be small.
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return []map[string]string{}, nil
	}

	headers := records[0]
	res := make([]map[string]string, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) == 0 {
			continue
		}
		obj := make(map[string]string, len(headers))
		for j := 0; j < len(headers) && j < len(row); j++ {
			obj[headers[j]] = row[j]
		}
		res = append(res, obj)
	}
	return res, nil
}
