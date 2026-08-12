# SimpleBudget

A personal finance dashboard and retirement planning tool built with Go, HTMX, and Plotly.js.

**Your data stays on your computer.** SimpleBudget runs entirely locally with no external servers, cloud storage, or network connections. All your financial data is stored in local files that only you can access. Optional encryption keeps your data secure at rest.

## Features

- **Dashboard** - KPIs with sparklines, spending trend, category breakdown, top spending, cumulative balance, and category drilldowns
- **Data Explorer** - Transaction search, filtering, pagination, date range stepping, transaction renaming, and CSV file management
- **What-If Planner** - Retirement projections with Monte Carlo simulation, historical backtesting, per-account asset allocation, Roth conversions, tax-aware cash flow with LTCG preferential rates, Social Security taxation and optimizer-driven claiming-age projection, NIIT, delayed IRMAA lookback modeling, budget-fit tax/IRMAA breakdowns, taxable account modeling with cost-basis tracking and dividend/cap-gains distribution tax drag, configurable projection timing, canonical household people anchored to a projection start month, linked healthcare person modeling, real vs nominal dollar views, year-by-year projection explainability, named scenarios, scenario chaining for multi-phase retirement plans, and tax-deferred withdrawal delays that treat locked balances as temporary shortfalls instead of immediate depletion
- **Insights** - Recurring payment detection with fuzzy vendor matching, subscription tracking, spending trends, income pattern analysis, recency filtering anchored to the selected data window without leaking in transactions after that window, anomaly detection (robust MAD statistics per category and merchant group, high-side only, with new-merchant flags for large first-time charges; full-history baselines with date filter scoping display only), and price-creep detection (recurring charges whose amounts drifted up — median of first 3 vs last 3 occurrences, ≥6 occurrences, >5% threshold)
- **Major Expenses** - User-curated list of declared expenses (rent, mortgage, fixed-amount checks, Amazon sub-buckets, etc.) with four matching modes (keyword, amount-only, keyword + amount AND filter, pin-only manual targets), an "internal transfer" mode that drops matches at load time so brokerage/savings transfers don't show as spending (Schwab MoneyLink, Fidelity, Vanguard, E*TRADE, Coinbase, Robinhood, Interactive Brokers filtered out of the box), automatic exception flagging across three buckets (unmatched over threshold, matched-but-anomalous-amount, new merchants), full-text/amount/date search across the exceptions panel, single- and bulk-pin (one click pins every filtered exception to a chosen expense), and cross-page surfacing on Explorer (column + filter), Insights (recurring badge), and Dashboard (Spending by Major Expense pie chart)
- **File Manager** - Data backup, restore, and file management
- **Amazon Order Enrichment** - Optional CLI (`cmd/enrich-amazon`) that reads your Amazon order export (`Order History.csv` retail + `Digital Content Orders.csv` digital), matches each shipment to the corresponding bank charge by amount within a ±5-day window (with multi-shipment-per-Order-ID and Order-ID-in-description fallbacks), and writes `data/amazon_enrichment.json` so every page that displays transactions shows "Amazon: <product> +N more" instead of opaque `AMZN MKTP US` rows. Per-transaction product labels override the major-expense bucket name in display only — grouping/aggregation under your "Amazon" major expense is unchanged. See [CHANGELOG.md](CHANGELOG.md) for the matching strategy and limits.
- **Encryption** - Optional password-based encryption for all data files

See [CHANGELOG.md](CHANGELOG.md) for version history and recent updates.

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

If Go is not installed, the build will automatically download and install Go 1.25 to `~/.local/go`. This is a one-time setup that takes about a minute.

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

### Uploading your bank export

1. Start SimpleBudget: `make run`
2. Open http://localhost:8080
3. Go to **File Manager** tab
4. Click the file input and select your CSV file
5. Click **Upload**

You can upload multiple CSV files - they'll all be loaded and deduplicated automatically.

### What SimpleBudget handles automatically

- **Flexible column names**: Works with common bank export formats (see below)
- **Debit/Credit columns**: Automatically combined into a single amount
- **Currency symbols**: `$87.34` → `87.34`
- **Comma formatting**: `1,234.56` → `1234.56`
- **Parentheses for negatives**: `(100.00)` → `-100.00`
- **Multiple date formats**: `2024-07-05`, `07/05/2024`, `7/5/2024`, `Jan 2, 2006`, etc.
- **Duplicate transactions**: Automatically removed when importing multiple files

### Supported column names

SimpleBudget recognizes these common column name variations:

| Required | Accepted names |
|----------|---------------|
| Date | `Date`, `Transaction Date`, `Posted Date`, `Posting Date` |
| Description | `Description`, `Memo`, `Details`, `Payee`, `Merchant`, `Narrative` |
| Amount | `Amount`, `Value`, `Transaction Amount`, `Sum` |

| Optional | Accepted names |
|----------|---------------|
| Category | `Category`, `Type`, `Category Name` |
| Debit | `Debit`, `Withdrawal`, `Money Out`, `Expense` |
| Credit | `Credit`, `Deposit`, `Money In`, `Income` |

**Debit/Credit handling**: If your bank uses separate Debit and Credit columns instead of a single Amount column, SimpleBudget automatically combines them (credits become positive, debits become negative).

### Manual adjustments you may need

**Amounts are reversed (expenses shown as positive)**
Some banks show expenses as positive numbers. If your spending appears as income, open the CSV in a spreadsheet and multiply the Amount column by -1.

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
- Key financial metrics at the top (income, expenses, net savings, savings rate)
- Monthly income vs expenses chart
- Spending by category breakdown
- Month-over-month spending trend
- Top spending merchants
- Cumulative balance over time

### First-run walkthrough

1. **Dashboard** - Your main overview with income, expenses, and savings rate
2. **Explorer** - Search and filter individual transactions, double-click any description to assign a custom name (e.g., rename "Check #996574" to "Plumber repair"), step through date ranges with arrow buttons, and filter state persists across tab changes
3. **Insights** - View current recurring payments, subscriptions, and spending patterns using full-history detection with recency-aware filtering tied to the selected end date and excluding future transactions outside that window; the page also surfaces Anomalies and Price Creep sections with their detection semantics
4. **What-If** - Run retirement projections with per-account allocations, a projection start month, canonical people with birth months, linked healthcare rows, Monte Carlo, and historical backtesting, including tax-deferred withdrawal delays that treat locked balances as temporary shortfalls rather than immediate depletion and worst-year ranking based on how quickly each sequence fails, including same-year failures
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
wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz
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

## Talking to your plan (MCP)

`cmd/whatif-mcp` serves the what-if planner and financial analytics over MCP on stdio, so you can ask
questions about a plan in Claude Code — what a number means, why it moved, and
what happens under a different assumption — and have it re-run the engine to
check. Six of its eight tools are read-only: `list_scenarios`, `get_analysis`,
`get_months`, `run_scenario`, `get_anomalies`, and `get_price_creep` only load and copy scenarios and transaction history. `open_page`
opens the what-if page (starting `cmd/server` if nothing is listening), and
`apply_changes` **writes to the saved plan** — it saves changed assumptions
through the running server's `POST /whatif/apply`. There is also a
`whatif://assumptions` resource describing what the engine does not model.

The MCP server talks HTTP to a `cmd/server` instance on localhost, default
`http://localhost:8080`, overridable with `BUDGET_SERVER_URL`. If nothing is
listening there, `open_page` and `apply_changes` start one — on the port that
URL names, and only when it is a loopback address; a non-loopback
`BUDGET_SERVER_URL` names a machine this process cannot start anything on, so
it refuses instead. It resolves its
own data directory from the `-data` flag, else `BUDGET_DATA_DIR`, else
`./data/settings`, and refuses to write if the server it finds is serving a
different settings directory than the one it reads. Locked or encrypted storage and misconfigured data directories (not shaped `<data-dir>/settings`) surface as clear errors. Before its first write to
a scenario in a session, it snapshots that scenario to a `.bak` file outside
the data directory; there is no in-app undo, so restoring that file by hand is
the recovery path for an unwanted change.

`get_anomalies` flags unusual expense transactions (outflows only, TransactionType == Outflow AND Amount < 0) by three methods: amounts far outside a merchant's or category's typical range (mad_merchant, mad_category using robust z-score against absolute amounts), or an outsized first-ever charge from a brand-new merchant (new_merchant). Detection always runs over the complete transaction history — peer-group baselines and each merchant's first-ever occurrence never change with the window — and accepts optional `start_date` and `end_date` (YYYY-MM-DD) to filter which already-detected flags are returned. Returns count and anomaly rows with date, description, category, amount (signed, negative for expenses), method, severity (high when score > 6), and score.

`get_price_creep` finds recurring merchant charges whose amounts have drifted upward (outflows only). For each merchant with at least 6 occurrences, it compares the median of the first 3 charges to the median of the last 3 and reports when the increase exceeds 5%; decreases and single outliers never report. Returns count and rows with merchant name, first amount, current amount, percent change, first and last dates, and occurrence count.

The repo ships a `.mcp.json`, so Claude Code picks it up from the repo root
and runs it with `go run ./cmd/whatif-mcp`, which triggers `go mod download`
on a fresh clone — real network egress at first launch, from the Go toolchain
rather than from the server itself.

## Project Structure

```
budget2/
├── cmd/
│   ├── server/                  # Main server application
│   │   ├── main.go              # HTTP handlers and routing
│   │   └── main_test.go         # Integration tests
│   ├── validate/                # CLI validation tool
│   └── whatif-mcp/              # MCP server (stdio) for the what-if planner
├── internal/
│   ├── config/                  # Environment configuration
│   ├── models/                  # Data structures
│   ├── services/
│   │   ├── classifier/          # Income/expense classification
│   │   ├── dataloader/          # CSV parsing and deduplication
│   │   ├── retirement/          # Retirement calculator and settings
│   │   └── storage/             # Encrypted file storage layer
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

- **Backend**: Go 1.25+ with Chi router
- **Frontend**: HTMX for dynamic updates, Plotly.js for charts
- **Styling**: Tailwind CSS via CDN
- **Storage**: File-based (CSV for transactions, JSON for settings)
- **Encryption**: Age (filippo.io/age) with multiple auth methods (password, SSH, Age identity, YubiKey)

## What-If Retirement Planner

The What-If Planner helps you model retirement scenarios with sophisticated projections.

### Portfolio Model

- **3-Bucket System**: Track Tax-Deferred (401k/IRA), Roth, and Taxable accounts separately
- **Per-Account Allocation**: Set different stock/bond/cash mixes for each account type
- **Tax-Efficient Withdrawals**: Automatic ordering (RMD → Taxable → Roth → Tax-Deferred)

### Simulation Methods

| Method | Description |
|--------|-------------|
| **Monte Carlo** | 1000 randomized return sequences with crash modeling |
| **Historical Backtest** | Test against 97 years of actual market data (1928-2024) |

### Key Features

- **RMD Calculations**: IRS-compliant Required Minimum Distributions starting at age 73
- **Roth Conversions**: Model annual conversion strategies with tax impact
- **Spending Phases**: Go-Go/Slow-Go/No-Go retirement spending patterns with spending trajectory preview showing per-year spend, income, need, RMD, and withdrawal rate
- **Social Security Optimizer**: Compare claiming ages 62-70 and optionally select claim ages that feed directly into projection income. Optimizer-derived primary and spouse SS streams appear at the top of the Income Sources list as read-only rows so projection inputs and what's listed never drift. Manual Social Security income sources (anything matching "social security" / "ssi") are excluded while the optimizer is active to avoid double-counting and are tagged inline as "excluded — handled by SS Optimizer" with a one-click delete. When a person has already claimed (claim age <= current age), the entered amount is treated as the actual benefit received — no actuarial adjustment is applied, the UI label changes to "Your Monthly Benefit," and the underlying PIA is back-derived for spousal calculations. Spousal benefits use the SSA spousal reduction formula (steeper than the worker's own: 25/36 of 1% per month for the first 36 months early, then 5/12 of 1%) and do not earn delayed retirement credits past FRA. Spousal top-up applies to the spouse who hasn't yet claimed regardless of whether the primary has already filed.
- **Healthcare Costs**: Model costs for multiple household members with Medicare transitions
- **Big Ticket Items**: One-time events (inheritance, home sale, large purchases)
- **Projection Start Month**: Anchor ages and chained scenarios to a specific `YYYY-MM` start month instead of duplicated saved ages
- **Canonical Household People**: Define primary, spouse, and additional people with birth months; displayed ages are derived from the active start month
- **Linked Healthcare Entries**: Healthcare rows can either stay manual or follow a canonical person’s name and age automatically
- **Scenario Chaining**: Link multiple scenarios to run sequentially — e.g., live off a pension until 70, then start Social Security and change withdrawal strategy. Balances carry over between phases while all other assumptions (expenses, income, allocation) switch to the chained scenario's settings.

### Asset Allocation

Returns are derived from historical data:
- **Stocks**: ~10.5% mean return (S&P 500 historical)
- **Bonds**: ~5.2% mean return (10-year Treasury)
- **Cash**: ~3.5% mean return (3-month T-bill)

The Investment Return slider overrides these calculations with a flat rate if set.

## Data Encryption

SimpleBudget supports optional encryption for all your financial data using the Age encryption library. When enabled, all CSV transaction files and JSON settings are encrypted at rest.

### Authentication Methods

SimpleBudget supports multiple authentication methods for encryption:

| Method | Description | Best For |
|--------|-------------|----------|
| **Password** | Traditional password-based encryption (scrypt) | Simple setup, easy to remember |
| **Age Identity** | Age X25519 key pair stored in a file | Key file backup, advanced users |
| **SSH Key** | Use existing SSH keys (ed25519, RSA) | Reuse existing SSH infrastructure |
| **YubiKey** | Hardware security key via age-plugin-yubikey | Maximum security, hardware-backed |

### Enabling Encryption

1. Go to **File Manager** tab
2. Scroll to the **Data Encryption** section
3. Select your preferred authentication method (tabs)
4. Configure the method:
   - **Password**: Enter and confirm a password (minimum 8 characters)
   - **Age Key**: Generate a new identity or select an existing one
   - **SSH Key**: Select from detected keys in `~/.ssh/`
   - **YubiKey**: Detected automatically if `age-plugin-yubikey` is installed
5. Click **Enable Encryption**

Once enabled:
- All existing data files are encrypted in place
- New files are automatically encrypted when saved
- Authentication is required via web interface on startup

### Unlocking on Startup

When encryption is enabled, the server starts normally but redirects all pages to a web-based unlock screen:

```bash
$ ./budget2
SimpleBudget v1.0.0
Encrypted storage detected - unlock via web interface at /unlock
Server starting on :8080
```

Open http://localhost:8080 and authenticate using your configured method:
- **Password**: Enter your password
- **Age Identity**: Automatic (reads from configured file)
- **SSH Key**: Enter passphrase if key is encrypted, otherwise automatic
- **YubiKey**: Touch your key when prompted

### What Gets Encrypted

| Encrypted | Not Encrypted |
|-----------|---------------|
| CSV transaction files | Cache files (plotly.js) |
| JSON settings (whatif.json) | Encryption marker files |
| User settings | `.encryption-config.json` (method metadata only) |

### Security Notes

- **Password requirements**: Minimum 8 characters (for password method)
- **No recovery**: If you lose your credentials, your data cannot be recovered
- **Backups mirror storage state**: Both manual downloads (`/backup`) and automatic snapshots preserve files in their on-disk form. With encryption enabled, the resulting ZIP contains age-encrypted blobs that can only be restored into an unlocked encrypted store with the same credentials. With encryption disabled, the ZIP is plaintext.
- **Break-glass plaintext export**: A separate, deliberate "Plaintext Export" button in the File Manager decrypts every file into a plaintext ZIP. Password method requires re-entering the password; other methods require typing `EXPORT`. Useful for migration to a new machine or for restoring without the original key. The downloaded file is unencrypted — keep it secure or delete it after use.
- **Cross-platform**: Works on Linux, macOS, and Windows
- **No fallback**: Each encryption setup uses a single auth method - keep backups of your keys

### YubiKey Setup

To use YubiKey authentication:

1. Install the age-plugin-yubikey:
   ```bash
   # Using Go
   go install github.com/str4d/age-plugin-yubikey@latest

   # Or download from releases
   ```

2. The YubiKey tab will appear in File Manager when the plugin is detected

3. Physical touch is required each time you decrypt data

### Disabling Encryption

1. Go to **File Manager** tab
2. Enter your current credentials (password, SSH passphrase, or touch YubiKey)
3. Click **Disable Encryption**

All files will be decrypted in place.

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

# Static analysis
make vet            # go vet — suspicious constructs
make static         # staticcheck — correctness, performance, simplification
make vuln           # govulncheck — reachable dependency vulnerabilities

# Concurrency and fuzzing
make race           # go test -race — data race detection
make fuzz           # auto-discover and run fuzz tests (default 30s per target)
make fuzz FUZZTIME=1m PKG=./internal/somepackage  # override duration and package

# Validate a running server
make validate
```

The pre-commit hook runs `vet`, `staticcheck`, and `test` automatically on every commit.

Test data is in `testdata/` with realistic sample transactions.
`make validate` checks the current dashboard contract, including the `monthly`, `category`, `spending-trend`, `merchants`, and `cumulative` chart endpoints.
`make fuzz` auto-discovers packages with `Fuzz*` tests. If none exist yet, it exits successfully with a guidance message instead of failing.
