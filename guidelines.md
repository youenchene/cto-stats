# Junie Agent Guidelines

These guidelines are for the Junie AI agent contributing to this repository. They are tailored to the current codebase and should guide day-to-day changes, debugging, and feature work while keeping modifications minimal and safe.

## Project Overview

- Purpose: expose metrics from CSV files as JSON via a small Go web server and render a CTO dashboard UI (Vite + React + TypeScript) consuming those APIs.
- Backend: Go (Echo web framework) in `command/web/web.go`, invoked via `go run . web`.
- UI: Vite React app under `ui/` using React Query and i18next. Build artifacts live in `ui/dist` and can be served by the Go server.
- Data: CSV inputs under `./data` (committed samples exist). The server reads and exposes them read-only as JSON arrays of objects (string values only).

## Run, Build, and Serve

Backend API and static UI:

- Build UI
  - cd ui
  - pnpm install
  - pnpm build
  - Output: `ui/dist` (Vite)
- Run server (from repo root):
  - go run . web -addr :8080 -data ./data -ui ./ui/dist
  - Static files at `/` served when `-ui` points to a folder containing `index.html`.
- API endpoints (from `command/web/web.go`):
  - GET /api/cycle_times -> `data/cycle_time.csv`
  - GET /api/stocks -> `data/stocks.csv`
  - GET /api/stocks/week -> `data/stocks_week.csv`
  - GET /api/throughput/week -> `data/throughput_week.csv` (404 if missing)
- SPA routing: for non-/api 404s, the server falls back to `index.html` if UI is enabled.

## Data Contracts (CSV -> JSON)

- `readCSV` returns `[]map[string]string`. All values stay strings; conversion happens in UI.
- CSV headers must be first row. Rows can be sparse; only matching indices are mapped.
- Current UI expects these common headers (fallbacks supported):
  - Cycle/Lead time rows (`/api/cycle_times`):
    - Period label: any header containing "year" or "month" (case-insensitive) is used for x-axis labeling.
    - Lead time: one of `leadtime_days_avg`, `lead_time`, `lead`, `leadtime`.
    - Cycle time: one of `cycletime_days_avg`, `cycle_time`, `cycle`, `cycletime`.
  - Stocks rows (current snapshot `/api/stocks`): numeric fields used for totals:
    - `opened_bugs`, `waiting_to_prod`, `in_review`, `in_qa`, `in_dev`, `in_backlogs`.
  - Stocks per week (`/api/stocks/week`):
    - Time label: `year` and `week`, or `year_week`/`year-week` alternatives.
    - Grouping: `project_name` (empty/whitespace -> translated label "Unassigned").
    - Numeric fields: same as stocks snapshot.
  - Throughput per week (`/api/throughput/week`):
    - Time label: prefer `year` + `week`; fallbacks: `year_week`, `year-week`, `year-month`, `year_month`.
    - Main value: `throughput` or `main` or `value`.
    - Optional control limits: `lcl`, `ucl`.

When adding new datasets or changing headers, ensure UI components either add fallbacks or the API normalizes keys without breaking existing behavior.

## UI Conventions

- Frameworks:
  - React + TypeScript, Vite build, React Query for data fetching, i18next for i18n.
- Data fetching (`ui/src/api.ts`):
  - All endpoints return `Row[]` where `Row = Record<string, string>`.
  - Use `useQuery` with stable `queryKey`s (`cycle_times`, `stocks`, `stocks_week`, `throughput_week`).
  - Throw on non-OK responses; components should handle loading/error states if added.
- Data parsing:
  - Convert strings to numbers in-component using utilities like `parseNumber` (see `ui/src/lib/utils`). Avoid implicit `Number(...)` on empty strings; treat unknown as `null` or `0` depending on context, consistent with existing usage.
- Components:
  - Big numbers: display `—` when value is `null`.
  - Charts: custom `LineChart`, `Sparkline`, and `StackedBarChart` under `ui/src/components/`.
  - `StackedBarChart` expects `labels: string[]` and `stacks: { name: string; values: number[] }[]`. Width defaults to 900; it renders dynamic x-ticks and a hover tooltip with a translated total label.
- i18n:
  - Translation keys in `ui/src/locales/en/translation.json` (e.g., `common.appTitle`, `units.days`, `stocks.labels.opened_bugs`).
  - When adding UI text, add keys to translation files and consume via `useTranslation().t(key)`.
- Layout:
  - Tailwind-like utility classes are used in JSX. Keep styles consistent and minimal.

## Backend Conventions

- File: `command/web/web.go` using Echo v4.
- Register new CSV-backed endpoints via the existing `serveCSV(route, filename)` helper. Keep responses as arrays of objects with string values.
- Error handling:
  - If CSV is missing: return 404 JSON `{ error, path, message }`.
  - Other errors: 500 with error message and path.
- Static UI:
  - Enable SPA fallback only for non-API 404s.
- Do not coerce types in the backend; keep transformation logic in the UI unless there is a strong reason.

## Minimal-Change Principle

- Prefer the smallest possible diff that achieves the goal.
- Match existing patterns and naming. Extend with fallbacks rather than rename existing keys.
- Favor localized changes within one file over broad refactors.
- Avoid breaking API contracts or CSV expectations used by the UI.

## When Introducing New Metrics or Views

1. Data
   - Add a new CSV file under `data/` with clear headers.
   - If it extends an existing dataset, prefer compatible headers or add UI fallbacks.
2. Backend
   - Add a new `serveCSV` route mapping to the CSV filename.
   - Document the endpoint in the `web.go` comment header.
3. UI
   - Create or extend components in `ui/src/components/`.
   - Add a `useQuery` hook in `ui/src/api.ts` with a stable `queryKey`.
   - Parse strings safely; show placeholders for missing values.
   - Add i18n keys for any labels.
4. Build & Serve
   - `pnpm build` the UI and run `go run . web -ui ui/dist -data data` to verify.

## Testing and Verification

- Manual checks:
  - Start server and confirm endpoints respond with arrays of objects.
  - Verify 404 behavior for missing `throughput_week.csv`.
  - Open the UI and check:
    - Lead & Cycle Times sparkline values and current numbers render.
    - Stocks section shows stacked bars for each metric and totals match the latest `stocks.csv` sums.
    - Throughput line and optional LCL/UCL render if values exist.
- Data edge cases:
  - Empty CSV: backend returns `[]`; UI should handle gracefully.
  - Unknown headers: UI fallbacks should cover or render `—`/0 without crashing.

## Code Style & Quality

- TypeScript: strict null awareness in rendering paths; avoid assumptions about data presence.
- React: keep components pure, compute derived arrays with `useMemo` only when necessary; avoid unnecessary re-renders.
- Go: keep functions small, clear error messages, and no global state.
- Internationalization: never hardcode user-facing strings; add to `translation.json`.

## Error Handling & UX

- Network errors: React Query will surface errors; if adding error UI, keep it unobtrusive and consistent.
- Missing datasets: tolerate absence where documented (e.g., throughput week optional) with empty states.

## Performance Notes

- CSVs are expected small; `ReadAll` is acceptable. If data grows significantly, consider streaming or pagination, but do not preemptively optimize.
- Charts adjust ticks based on width; keep widths reasonable and allow horizontal scrolling for large data series (as done in Stocks block).

## Security & Config

- Server binds to `-addr`, default `:8080`. Exposes read-only CSV data; ensure deployment environment restricts access appropriately if data is sensitive.
- No authentication/authorization is implemented. Add only if explicitly required.

## Contribution Checklist (for Junie)

- [ ] Understand the goal and choose the minimal scope.
- [ ] Verify CSV headers and update UI fallbacks only if necessary.
- [ ] Keep backend values as strings; parse in UI.
- [ ] Add/adjust i18n keys for any new labels.
- [ ] Update `web.go` doc comment if adding/modifying endpoints.
- [ ] Build UI and run the server locally to validate behavior.
- [ ] Test with missing/empty CSV where applicable.
- [ ] Keep diffs small and focused.

## Useful Paths

- Backend server: `command/web/web.go`
- Entry point: `main.go` (subcommand `web`)
- UI app root: `ui/src/App.tsx`
- API hooks: `ui/src/api.ts`
- Charts: `ui/src/components/`
- Translations: `ui/src/locales/en/translation.json`
- Sample data: `data/`

## Versioning & Dependencies

- Go modules tracked via `go.mod` / `go.sum`.
- UI dependencies pinned by `ui/pnpm-lock.yaml`.
- Avoid broad dependency upgrades unless necessary for a fix.

---
These guidelines should evolve with the codebase. When you change assumptions (headers, routes, component contracts), update this document accordingly to keep future changes predictable and safe.