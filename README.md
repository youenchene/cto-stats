# CTO Stats

A minimal toolkit to generate a statistic web dashboard about **the productivity of a software organization**. 

It allows you to see how the organization (the system) is **behaving** and help to identify **bottlenecks** in the **system**.

The generated KPI are : 
 - **Lead time** and **cycle time** trend
 - **Stocks** per week : **Red Bin** for bug and **stocks** per **development process steps**.
 - **Statistics controlled** weekly **throughput** (issues closed per week)
 - **Cloud spending follow-up**: monthly Azure & GCP costs overall and per service/group

Current sources are :
 - github issues of a github organization.
 - github projects of a github organization.
 - Azure Cost Management API (for cloud spending)
 - GCP Cloud Billing API (for cloud spending)


The tools is a CLI with three subcommands :
  - **import**: fetches data from GitHub and writes raw CSVs to ./data. You can scope what is imported with `--issues`, `--pr`, and/or `--cloudspending`.
  - **calculate**: computes aggregates and writes CSVs to ./data. You can scope what is calculated with `--issues`, `--pr`, and/or `--cloudspending`.
  - **web**: launch web dashboard.

[View full size image](docs/screen-v0.1.png)

![Dashboard screenshot](docs/screen-v0.1-tn.png)

## Indicators definitions

### Cycle time

The time taken from when a team starts working on a task until it’s ready for delivery (e.g., code merged and tested and in production).

### Lead time

The total duration from a request being made (e.g., ticket created) until it’s delivered to the user (e.g., deployed to production).

### Time To PR

The interval between starting work on a task and submitting it for review via a pull request.

### Stocks

This is the history of Work In progress (WIP) stocks. It is the number of tasks that are in each status at any given time.

It's recalculated backwards so you can vizualize the history of WIP stocks.

The stocks are :
- **Red Bin** for bugs
- **Development process steps** :
  - **Waiting to Production**
  - **In QA**
  - **In Review**
  - **In Development**
  - **In Ready**
  - **In Backlog**

The development process steps are here to visualize the stock distribution and to find bottlenecks. It should be analyzed in a **pull* way (Production ==> QA ==> Review ==> Development ==> Ready ==> Backlog).

### Throughput Control Chart

The measurable output rate of a system over time, analyzed within statistically defined boundaries—**Lower Control Limit (LCL)** and **Upper Control Limit (UCL)**—to distinguish normal variation (**common causes**) from anomalies (**special causes**) requiring intervention.

More information here : https://deming.org/a-beginners-guide-to-control-charts/

### Change Request count per week (stacked by repo)

Basic indicator to identify Change request event per week on pull requests.

### Cloud Spending Follow-Up

Tracks cloud infrastructure spending over time from Azure and GCP. Two visualizations are provided:
- **Overall costs per month**: Shows total spending per cloud provider (Azure & GCP) aggregated monthly.
- **Per service/group costs per month**: Shows spending breakdown by logical groups (preferred) or by individual services, as configured in `config.yml`.

This helps identify cost trends, compare spending across providers, and track specific services that contribute most to cloud expenses.

## How to run - user mode

### Prequisites

Environment variables:
- **GITHUB_TOKEN**: a GitHub token with read access to the organization (required for GitHub data)
- **CONFIG_PATH**: (optional) path to config.yml (defaults to `./config.yml`)

Cloud Spending (optional, only needed for `--cloudspending` scope):
- **AZURE_SUBSCRIPTION_ID**: Azure subscription ID (supports multiple subscriptions separated by commas, e.g., `sub-id-1,sub-id-2`)
- **AZURE_TENANT_ID**: Azure AD tenant ID
- **AZURE_CLIENT_ID**: Azure service principal client ID
- **AZURE_CLIENT_SECRET**: Azure service principal client secret
- **GCP_PROJECT_ID**: GCP project ID
- **GCP_BILLING_ACCOUNT**: GCP billing account ID (format: `billingAccounts/XXXXXX-XXXXXX-XXXXXX`)
- **GCP_SERVICE_ACCOUNT_JSON**: GCP service account JSON key (as a string or path to JSON file)


#### Service Account Permissions on GCP

- Big Query Data Viewer
- Big Query Job User

## Usage

- Set `GITHUB_TOKEN` (required)
- Optionally set `CONFIG_PATH` to point to your YAML configuration file (defaults to `./config.yml` if present)

Examples:

```bash
# Import issues for an org (explicit org)
GITHUB_TOKEN=ghp_xxx go run . import -org your-org

# Import using org from config.yml
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . import

# Import with filters
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . import -since 2025-01-01 -repo repoA,repoB

# Import only issues scope (issues + timelines + project moves)
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . import --issues

# Import only PR scope (pull requests + reviews for change requests)
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . import --pr

# Import both scopes explicitly (default when no scope is provided)
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . import --issues --pr

# Import cloud spending data (last 24 months from Azure & GCP)
AZURE_SUBSCRIPTION_ID=xxx AZURE_TENANT_ID=xxx AZURE_CLIENT_ID=xxx AZURE_CLIENT_SECRET=xxx \
GCP_PROJECT_ID=xxx GCP_BILLING_ACCOUNT=billingAccounts/XXX GCP_SERVICE_ACCOUNT_JSON='{"type":"service_account",...}' \
go run . import --cloudspending

# Calculate KPIs (requires config file for project column mappings)
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . calculate

# Calculate only issue-based KPIs (lead/cycle times, throughput, stocks)
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . calculate --issues

# Calculate only PR change-requests KPIs (weekly, per-repo)
GITHUB_TOKEN=ghp_xxx go run . calculate --pr

# Calculate cloud spending aggregations (monthly and per-service/group)
CONFIG_PATH=./config.yml go run . calculate --cloudspending

# Serve the dashboard
GITHUB_TOKEN=ghp_xxx go run . web -addr :8080 -data ./data -ui ./ui/dist
```

Notes about scopes:
- `--issues` scope handles issues, status timelines, and project moves; these power lead/cycle time, throughput, and stocks.
- `--pr` scope is only about pull requests and change requests (reviews with CHANGES_REQUESTED) and powers the PR charts.
- `--cloudspending` scope fetches and aggregates cloud costs from Azure and GCP APIs; powers the Cloud Spending Follow-Up dashboard.
- If you omit all scope flags for issues/PR commands, both `--issues` and `--pr` are processed (backward compatible default).
- The `--cloudspending` scope is independent and must be explicitly specified.

## How to run - developer mode

### Prequisites

Environment variables:
- GITHUB_TOKEN: a GitHub token with read access to the organization

### Build

```bash
cd ui
pnpm install
pnpm build
```

### Usage

#### 1st step is to import data :

Minimal command :
```bash
go run . import -org your-org 
```

You can restrict the import to a specific date range and/or a list of repositories :
```bash
go run . import -org your-org -since 2025-01-01 -repo repoA,repoB
```

#### 2nd step si to calculate KPI :

```bash
go run . calculate
```

As a bonus, it generated a consolidated CSV file in ./data/calculated_issue.csv that you can reuse if you want to.

### 3rd step is to show up the web dashboard :

Minimal commad is

```bash
go run . web
```

Web dashboard is available at http://localhost:8080/

You can also specify the port, the data folder and the UI folder :
```bash
go run . web -addr :8080 -data ./data -ui ./ui/dist
```


### Dev mode

```bash
cd ui
pnpm run dev
```

Dev Dashboard is now available at http://localhost:5173/

### Extra Documentations

Available API endpoints :
- GET /api/cycle_times → data/cycle_time.csv
- GET /api/stocks → data/stocks.csv
- GET /api/stocks/week → data/stocks_week.csv
- GET /api/throughput/week → data/throughput_week.csv
- GET /api/pr/change_requests → data/pr_change_requests_week.csv
- GET /api/pr/change_requests/repo → data/pr_change_requests_repo.csv
- GET /api/pr/change_requests/repo_dist → data/pr_change_requests_repo_dist.csv
- GET /api/cloud_spending/monthly → data/cloud_spending_monthly.csv
- GET /api/cloud_spending/services → data/cloud_spending_services.csv

Cloud Spending CSV formats:

- data/cloud_spending_monthly.csv
  - Headers: `month,provider,cost,currency`
  - Rows are aggregated by month, provider, and currency (no cross-currency mixing).

- data/cloud_spending_services.csv
  - Headers depend on your configuration:
    - Grouped mode: `month,provider,group,cost,currency`
    - Legacy flat mode: `month,provider,service,cost,currency`
  - Rows are aggregated by month, provider, and group/service, and currency (no cross-currency mixing).
  - If you configure `cloudspending.detailed_service` with groups, only services belonging to the defined groups are included (and exposed under the `group` column). If you configure a flat list (legacy), only those services are included (under the `service` column).

### Configuration (config.yml)

The `config.yml` file allows customization of GitHub project mappings and cloud spending service filters.

**Cloud Spending Configuration (preferred grouped mode):**

Define logical groups that aggregate several concrete services. The UI will display one chart per group.

```yaml
cloudspending:
  detailed_service:
    - name: "AI"
      services:
        - "Vertex AI"
        - "Claude Sonnet 4.5"
    - name: "Databases"
      services:
        - "Cloud SQL"
        - "BigQuery"
```

Legacy/alternate shapes also supported (backward compatible):

1) Flat list under `cloudspending.detailed_service` (strings). This behaves like a simple filter list and the CSV exposes the `service` column.

```yaml
cloudspending:
  detailed_service:
    - "Vertex AI"
    - "Compute Engine"
    - "Cloud Run"
```

2) Flat list under `cloud_spending.services` (older config key). Same behavior as above.

```yaml
cloud_spending:
  services:
    - "Virtual Machines"
    - "Azure Kubernetes Service"
    - "Storage Accounts"
    - "Compute Engine"
    - "Cloud Storage"
    - "Google Kubernetes Engine"
```

Notes:
- If groups are configured, the services CSV uses a `group` column and only includes services that belong to a configured group.
- If only flat lists are provided, the services CSV uses a `service` column and includes only those services.
- The monthly overall CSV is unaffected by filters/groups; it always shows total cost per provider.
- Amounts are shown with their original currency. If multiple currencies exist in your dataset, aggregations are kept per currency (no conversion).

## How to build and run with Docker

The repository includes a multi‑stage `Dockerfile` that:
- Builds the web UI with `pnpm run build` (Vite)
- Builds the Go binary `cto-stats`
- Produces a small Alpine image that:
  - Reads configuration from an external `config.yml` (mounted)
  - Stores and serves CSV data from `/data` (make sure you MOUNT it to persist between updates)
  - Runs a daily cron job at 05:00 to execute `import` then `calculate`
  - Starts the web server on port 8080 and serves the UI at `/`

### Build the image

```bash
docker build -t cto-stats:latest .
```

### Run the container (with persistent data)

Use a named volume (recommended):

```bash
docker run -d \
  --name cto-stats \
  -e GITHUB_TOKEN=ghp_xxx \
  -e TZ=Europe/Paris \
  -v $(pwd)/config.yml:/config/config.yml:ro \
  -v cto_stats_data:/data \
  -p 8080:8080 \
  cto-stats:latest
```

To include cloud spending data, add the Azure and GCP environment variables:

```bash
docker run -d \
  --name cto-stats \
  -e GITHUB_TOKEN=ghp_xxx \
  -e TZ=Europe/Paris \
  -e AZURE_SUBSCRIPTION_ID=xxx \
  -e AZURE_TENANT_ID=xxx \
  -e AZURE_CLIENT_ID=xxx \
  -e AZURE_CLIENT_SECRET=xxx \
  -e GCP_PROJECT_ID=xxx \
  -e GCP_BILLING_ACCOUNT=billingAccounts/XXX \
  -e GCP_SERVICE_ACCOUNT_JSON='{"type":"service_account",...}' \
  -v $(pwd)/config.yml:/config/config.yml:ro \
  -v cto_stats_data:/data \
  -p 8080:8080 \
  cto-stats:latest
```

Or bind‑mount a host directory:

```bash
mkdir -p $(pwd)/cto-stats-data

docker run -d \
  --name cto-stats \
  -e GITHUB_TOKEN=ghp_xxx \
  -e TZ=Europe/Paris \
  -e AZURE_SUBSCRIPTION_ID=xxx \
  -e AZURE_TENANT_ID=xxx \
  -e AZURE_CLIENT_ID=xxx \
  -e AZURE_CLIENT_SECRET=xxx \
  -e GCP_PROJECT_ID=xxx \
  -e GCP_BILLING_ACCOUNT=billingAccounts/XXX \
  -e GCP_SERVICE_ACCOUNT_JSON='{"type":"service_account",...}' \
  -v $(pwd)/config.yml:/config/config.yml:ro \
  -v $(pwd)/cto-stats-data:/data \
  -p 8080:8080 \
  cto-stats:latest
```

Notes:
- The image declares volumes for `/config` and `/data`; mounting `/data` is REQUIRED if you want CSVs to persist across container rebuilds/updates.
- On container start, the image launches the web server immediately. If `IMPORT_ON_START=true` (default), an initial `import` then `calculate` is started in the background so the UI is available right away.
  - Disable this initial background run with `-e IMPORT_ON_START=false`.
  - Background job logs can be viewed at `/var/log/init-jobs.log` (and daily cron logs at `/var/log/cron.log`).
- A cron job runs every day at 05:00 container local time to refresh data (`import` then `calculate`).
  - Set timezone with `-e TZ=Europe/Paris` (or your preferred IANA TZ).
  - By default, the cron job runs `import --issues --pr` and `calculate --issues --pr`. To include cloud spending data, you must provide the Azure/GCP environment variables and the job will automatically include `--cloudspending` scope.
- The web UI is available at http://localhost:8080/ (APIs under `/api/*`).
- Cloud spending environment variables (Azure and GCP) are optional. If not provided, only GitHub data will be imported.
- **Multiple Azure subscriptions**: To combine costs from multiple Azure subscriptions, provide comma-separated subscription IDs: `-e AZURE_SUBSCRIPTION_ID="sub-id-1,sub-id-2"`. All costs will be aggregated in the same CSV. Ensure your service principal has `Cost Management Reader` role on all subscriptions.
- `CONFIG_PATH` defaults to `/config/config.yml`. You can override if you mount elsewhere:

```bash
docker run -d \
  --name cto-stats \
  -e GITHUB_TOKEN=ghp_xxx \
  -e CONFIG_PATH=/config/my-config.yml \
  -v $(pwd)/my-config.yml:/config/my-config.yml:ro \
  -v cto_stats_data:/data \
  -p 8080:8080 \
  cto-stats:latest
```

### Data location and backup

- Inside the container, all CSVs are written to `/data` by `import` and `calculate`, and the web server reads from `/data`.
- With a named volume, you can backup/restore via:
  ```bash
  docker run --rm -v cto_stats_data:/data -v $(pwd):/backup alpine sh -c 'cd /data && tar czf /backup/cto-stats-data.tgz .'
  ```
- With a bind‑mount, the files are directly available on the host at the path you mounted (e.g., `./cto-stats-data`).
