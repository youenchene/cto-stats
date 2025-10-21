package web

import (
	"encoding/csv"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
)

// Run starts a small Echo web server exposing CSV-as-JSON APIs.
//
// Usage:
//
//	github-stats web [-addr :8080] [-data ./data]
//
// Endpoints:
//
//	GET /api/cycle_times          -> <data>/cycle_time.csv
//	GET /api/stocks               -> <data>/stocks.csv
//	GET /api/stocks/week          -> <data>/stocks_week.csv
//	GET /api/throughtput/month    -> <data>/throughput_month.csv (404 if missing)
func Run(args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "http listen address (host:port)")
	dataDir := fs.String("data", "./data", "directory containing CSV files")
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

	serveCSV("/api/cycle_times", "cycle_time.csv")
	serveCSV("/api/stocks", "stocks.csv")
	serveCSV("/api/stocks/week", "stocks_week.csv")
	serveCSV("/api/throughtput/month", "throughput_month.csv")

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
