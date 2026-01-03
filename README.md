# SimpleBudget

A personal finance dashboard and retirement planning tool built with Go, HTMX, and Plotly.js.

## Features

- **Dashboard** - KPIs, spending charts, alerts, and category drilldowns
- **Data Explorer** - Transaction search, filtering, pagination, and CSV file management
- **What-If Planner** - Retirement projections with Monte Carlo simulation and sensitivity analysis
- **Insights** - Recurring payment detection, spending trends, and income pattern analysis
- **File Manager** - Data backup, restore, and file management

## Prerequisites

Before you begin, you'll need:

1. **Make** (build automation tool)
2. **curl** (for downloading files)
3. **Git** (optional, for cloning the repository)

**Note:** Go is automatically installed by the Makefile if not found on your system.

### Installing Make and curl

#### Linux (Ubuntu/Debian)
```bash
sudo apt update
sudo apt install make curl
```

#### macOS
```bash
# Make is included with Xcode Command Line Tools
xcode-select --install

# curl is pre-installed on macOS
```

#### Windows
```bash
# Using Chocolatey
choco install make curl

# Or use Git Bash which includes make and curl
```

## Installation

### Step 1: Get the source code

```bash
# Clone the repository (if using git)
git clone https://github.com/yourusername/budget2.git
cd budget2

# Or download and extract the ZIP file
```

### Step 2: Build the application

```bash
# Build the application (Go is installed automatically if needed)
make build

# This creates a 'budget2' executable in the current directory
```

If Go is not installed, the build will automatically download and install Go 1.23.4 to `~/.local/go`. This is a one-time setup that takes about a minute.

### Step 3: Create the data directory

```bash
# Create a directory for your financial data
mkdir -p data
```

## Preparing Your Data

SimpleBudget reads transaction data from CSV files. You'll need to export transactions from your bank and format them correctly.

### Exporting from your bank

Most banks let you download transaction history as CSV files:

1. Log into your online banking
2. Navigate to your account transaction history
3. Look for "Download", "Export", or "Download Transactions"
4. Choose CSV format (not PDF or OFX)
5. Select your date range
6. Download the file

### CSV format requirements

Your CSV files must have these columns (header names are flexible):

| Column | Required | Description | Examples |
|--------|----------|-------------|----------|
| Date | Yes | Transaction date | `2024-07-05`, `07/05/2024`, `7/5/24` |
| Description | Yes | Transaction description | `WALMART GROCERY`, `DIRECT DEP PAYROLL` |
| Amount | Yes | Transaction amount | `3500.00`, `-87.34` |
| Category | No | Spending category | `Groceries`, `Paycheck`, `Entertainment` |

**Amount sign convention:**
- **Positive amounts** = money coming IN (income, deposits, refunds)
- **Negative amounts** = money going OUT (expenses, payments, purchases)

### Example CSV file

```csv
Date,Description,Amount,Category
2024-07-05,DIRECT DEP ACME CORP PAYROLL,3500.00,Paycheck
2024-07-12,WALMART GROCERY,-87.34,Groceries
2024-07-15,SHELL GAS STATION,-45.23,Gas & Fuel
2024-07-19,DIRECT DEP ACME CORP PAYROLL,3500.00,Paycheck
2024-07-20,RENT PAYMENT APT 204,-1850.00,Rent
2024-07-22,NETFLIX SUBSCRIPTION,-15.99,Entertainment
```

### Converting bank exports

Bank exports often need adjustment. Common issues:

**Issue: Amounts are reversed**
Some banks show expenses as positive. Edit your CSV to add minus signs to expenses.

**Issue: Different column names**
SimpleBudget is flexible with column names. These all work:
- `Date`, `date`, `Transaction Date`, `Posted Date`
- `Description`, `description`, `Memo`, `Details`
- `Amount`, `amount`, `Value`, `Transaction Amount`

**Issue: Multiple amount columns (Debit/Credit)**
If your bank has separate Debit and Credit columns, combine them into one Amount column:
- Credits become positive amounts
- Debits become negative amounts

### Place files in data directory

Copy your CSV files to the `data/` directory:

```bash
cp ~/Downloads/transactions.csv data/
```

You can have multiple CSV files - they'll all be loaded and deduplicated automatically.

## Running SimpleBudget

### Start the server

```bash
# Run the application
make run

# Or run directly after building
./budget2
```

### Access the dashboard

Open your web browser and go to: **http://localhost:8080**

You should see the dashboard with:
- Key financial metrics at the top
- Spending charts and trends
- Category breakdowns
- Spending alerts for unusual activity

### First-run walkthrough

1. **Dashboard** - Your main overview with income, expenses, and savings rate
2. **Explorer** - Search and filter individual transactions
3. **Insights** - View recurring payments and spending patterns
4. **What-If** - Run retirement projections and simulations
5. **File Manager** - Manage data files, create backups

## Quick Start Commands

```bash
# Build and run (recommended for first time)
make run

# Development mode (no binary, faster startup)
make dev

# Hot reload development (auto-restarts on code changes)
# Requires: go install github.com/air-verse/air@latest
make watch
```

The server runs at http://localhost:8080

## Troubleshooting

### "command not found: make"

Make is not installed.

**Fix:**
```bash
# Linux
sudo apt install make

# macOS
xcode-select --install
```

### "no data files found" or empty dashboard

No CSV files in the data directory.

**Fix:**
1. Create the data directory: `mkdir -p data`
2. Add your CSV transaction files to `data/`
3. Restart the server

### "port 8080 already in use"

Another application is using port 8080.

**Fix:** Either stop the other application, or modify the port (check config options).

### CSV parsing errors

Your CSV format doesn't match expected format.

**Fix:**
1. Ensure first row has column headers
2. Check date format is valid
3. Ensure amounts are numbers (no currency symbols like $)
4. Remove any extra commas in description text

### Charts not loading

JavaScript libraries may not have downloaded.

**Fix:**
```bash
make vendor-js
```

### Go auto-installation fails

If the automatic Go installation fails (network issues, permissions, etc.):

**Fix:** Install Go manually:
```bash
# Linux/macOS - download from https://go.dev/dl/
wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Or force reinstall via Makefile
make install-go
```

## Building

```bash
# Build single binary (embeds all static assets)
make build

# The binary is self-contained and portable
./budget2

# Build for all platforms
make build-all
```

The compiled binary includes all HTML, CSS, and JavaScript - no additional files needed.

## Development

```bash
# Format code
make fmt

# Run tests
make test

# Generate coverage report
make test-coverage

# Check available make targets
make help
```

## Project Structure

```
budget2/
├── cmd/
│   ├── server/                  # Main server application
│   │   ├── main.go              # HTTP handlers and routing
│   │   └── main_test.go         # Integration tests
│   └── validate/                # CLI validation tool
├── internal/
│   ├── config/                  # Environment configuration
│   ├── models/                  # Data structures
│   ├── services/
│   │   ├── classifier/          # Income/expense classification
│   │   ├── dataloader/          # CSV parsing and deduplication
│   │   └── retirement/          # Retirement calculator and settings
│   ├── templates/               # Template rendering with helpers
│   └── testutil/                # Test utilities and assertions
├── web/
│   ├── embed.go                 # Static file embedding
│   ├── static/                  # CSS, JS, vendor libraries
│   └── templates/               # HTML templates (layouts, pages, components)
├── testdata/                    # Test fixtures
├── data/                        # User data (gitignored)
├── Makefile
└── budget2                      # Compiled binary
```

## Technology Stack

- **Backend**: Go 1.21+ with Chi router
- **Frontend**: HTMX for dynamic updates, Plotly.js for charts
- **Styling**: Tailwind CSS via CDN
- **Storage**: File-based (CSV for transactions, JSON for settings)

## Testing

```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests
make test-integration

# Generate coverage report
make test-coverage

# Validate a running server
make validate
```

Test data is in `testdata/` with realistic sample transactions.
