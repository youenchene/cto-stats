package cloudspending

import "time"

// CostRecord represents a single cost entry for a cloud service in a specific month
type CostRecord struct {
	Provider string    // "azure" or "gcp"
	Service  string    // Service name (e.g., "Virtual Machines", "Cloud Storage")
	Month    time.Time // First day of the month
	Cost     float64   // Cost in the billing currency
	Currency string    // Currency code (e.g., "USD", "EUR")
	RawData  string    // JSON string of raw response data for debugging
}

// MonthlyCost represents aggregated cost per provider per month
type MonthlyCost struct {
	Provider string
	Month    time.Time
	Cost     float64
	Currency string
}

// ServiceCost represents aggregated cost per service per month
type ServiceCost struct {
	Provider string
	Service  string
	Month    time.Time
	Cost     float64
	Currency string
}
