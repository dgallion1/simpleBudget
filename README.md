# SimpleBudget

A personal finance dashboard and retirement planning tool built with Go, HTMX, and Plotly.js.

## Features

- **Dashboard** - KPIs, spending charts, alerts, and category drilldowns
- **Data Explorer** - Transaction search, filtering, pagination, and CSV file management
- **What-If Planner** - Retirement projections with Monte Carlo simulation and sensitivity analysis
- **Insights** - Recurring payment detection, spending trends, and income pattern analysis

## Quick Start

```bash
# Build and run
make run

# Development mode (no binary)
make dev

# Hot reload development
make watch
```

Server runs at http://localhost:8080

## Building

```bash
# Build single binary (embeds all static assets)
make build

# The binary is self-contained and portable
./budget2
```

## Project Structure

```
budget2/
├── cmd/server/main.go           # HTTP handlers and routing
├── internal/
│   ├── config/                  # Environment configuration
│   ├── models/                  # Data structures
│   ├── services/
│   │   ├── classifier/          # Income/expense classification
│   │   ├── dataloader/          # CSV parsing and deduplication
│   │   └── retirement/          # Retirement calculator and settings
│   └── templates/               # Template rendering with helpers
├── web/
│   ├── embed.go                 # Static file embedding
│   ├── static/                  # CSS, JS, vendor libraries
│   └── templates/               # HTML templates (layouts, pages, components)
├── data/                        # User data (gitignored)
│   ├── *.csv                    # Transaction files
│   ├── settings/                # What-if settings (JSON)
│   └── uploads/                 # Uploaded files
├── Makefile
└── budget2                      # Compiled binary
```

## Data Files

Place CSV transaction files in the `data/` directory. Expected columns:
- Date
- Description
- Amount
- Category (optional)

## Technology Stack

- **Backend**: Go 1.25+ with Chi router
- **Frontend**: HTMX for dynamic updates, Plotly.js for charts
- **Styling**: Tailwind CSS via CDN
- **Storage**: File-based (CSV for transactions, JSON for settings)

## Development

```bash
# Format code
make fmt

# Run tests
make test

# Tidy dependencies
make tidy

# Download vendor JS libraries
make vendor-js
```
