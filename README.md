# CTO Stats

A minimal toolkit to generate a statistic web dashboard about **the productivity of a software organization**. 

It allows you to see how the organization (the system) is **behaving** and help to identify **bottlenecks** in the **system**.

The generated KPI are : 
 - **Lead time** and **cycle time** trend
 - **Stocks** per week : **Red Bin** for bug and **stocks** per **development process steps**.
 - **Statistics controlled** weekly **throughput** (issues closed per week)

Current sources are :
 - github issues of a github organization.
 - github projects of a github organization.


The tools is a CLI with three subcommands :
  - **import**: fetches issues and timelines from GitHub and writes raw CSVs to ./data - that you can reuse apart if you want to.
  - **calculate**: computes aggregates (cycle time, throughput, WIP stocks) and writes CSVs to ./data  - that you can reuse apart if you want to.
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

## How to run - user mode

### Prequisites

Environment variables:
- GITHUB_TOKEN: a GitHub token with read access to the organization
- CONFIG_PATH: (optional) path to config.yml

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

# Calculate KPIs (requires config file for project column mappings)
GITHUB_TOKEN=ghp_xxx CONFIG_PATH=./config.yml go run . calculate

# Serve the dashboard
GITHUB_TOKEN=ghp_xxx go run . web -addr :8080 -data ./data -ui ./ui/dist
```

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

Or bind‑mount a host directory:

```bash
mkdir -p $(pwd)/cto-stats-data

docker run -d \
  --name cto-stats \
  -e GITHUB_TOKEN=ghp_xxx \
  -e TZ=Europe/Paris \
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
- The web UI is available at http://localhost:8080/ (APIs under `/api/*`).
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
