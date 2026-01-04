# SimpleBudget

A personal finance dashboard and retirement planning tool built with Go, HTMX, and Plotly.js.

**Your data stays on your computer.** SimpleBudget runs entirely locally with no external servers, cloud storage, or network connections. All your financial data is stored in local files that only you can access. Optional encryption keeps your data secure at rest.

## Features

- **Dashboard** - KPIs, spending charts, alerts, and category drilldowns
- **Data Explorer** - Transaction search, filtering, pagination, and CSV file management
- **What-If Planner** - Retirement projections with Monte Carlo simulation and sensitivity analysis
- **Insights** - Recurring payment detection, spending trends, and income pattern analysis
- **File Manager** - Data backup, restore, and file management
- **Encryption** - Optional password-based encryption for all data files

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

- **Backend**: Go 1.21+ with Chi router
- **Frontend**: HTMX for dynamic updates, Plotly.js for charts
- **Styling**: Tailwind CSS via CDN
- **Storage**: File-based (CSV for transactions, JSON for settings)
- **Encryption**: Age (filippo.io/age) with scrypt password-based key derivation

## Data Encryption

SimpleBudget supports optional encryption for all your financial data using the Age encryption library. When enabled, all CSV transaction files and JSON settings are encrypted at rest.

### Enabling Encryption

Encryption is enabled programmatically. Once enabled:
- All existing data files are encrypted in place
- New files are automatically encrypted when saved
- A password is required on every startup

### Password on Startup

When encryption is enabled, you'll be prompted for your password:

```bash
$ ./budget2
SimpleBudget v1.0.0
Encrypted storage detected
Enter encryption password: ********
Encrypted storage unlocked successfully
Server starting on :8080
```

Or use an environment variable for headless/automated deployments:

```bash
BUDGET_ENCRYPTION_PASSWORD=yourpassword ./budget2
```

### What Gets Encrypted

| Encrypted | Not Encrypted |
|-----------|---------------|
| CSV transaction files | Cache files (plotly.js) |
| JSON settings (whatif.json) | Encryption marker files |
| User settings | Backup downloads (for portability) |

### Security Notes

- **Password requirements**: Minimum 8 characters
- **No recovery**: If you forget your password, your data cannot be recovered
- **Backups are unencrypted**: Downloaded backup ZIPs are plain files for portability
- **Cross-platform**: Works on Linux, macOS, and Windows

### Disabling Encryption

To remove encryption, call the disable function with your current password. All files will be decrypted in place.

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
