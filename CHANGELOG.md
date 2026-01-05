# Changelog

## v1.2.0 - Enhanced Backtesting & Asset Allocation

### New Features

#### Configurable Asset Allocation
- **Stock/Bond/Cash allocation**: Set your own portfolio mix instead of fixed 60/40
- Stocks use S&P 500 historical returns
- Bonds use 10-year Treasury yields
- Cash uses 3-month T-bill rates
- Bond percentage computed automatically (100% - stocks - cash)

#### Historical T-Bill Returns
- Added 97 years of cash/money market returns (1928-2024)
- Source: NYU Stern/Damodaran historical returns database
- Enables accurate modeling of conservative portfolios

#### Inflation-Adjusted Results
- Backtesting now shows both nominal and real (inflation-adjusted) balances
- Real balance represents purchasing power in start-year dollars
- Cumulative inflation tracked throughout each simulation

#### Monte Carlo Asset Allocation
- Monte Carlo simulation now uses your stock/bond/cash allocation
- Separate return generation for each asset class with realistic volatility
- Stocks: ~11.7% mean, 19% standard deviation
- Bonds: ~5% mean, 8% standard deviation
- Cash: ~3.3% mean (low volatility)
- Flight-to-safety behavior: bonds rally during stock crashes

### UI Improvements
- New Asset Allocation section in Rate Assumptions card
- "Final (Real)" column in historical backtest results table
- Explanatory notes about inflation-adjusted values

---

## v1.1.0 - Multi-Account Tax Support

### New Features

#### 3-Bucket Portfolio Model
- **Roth Account Support**: Portfolio now tracks Tax-Deferred, Roth, and Taxable accounts separately
- Configurable allocation percentages for each account type
- Tax-efficient withdrawal ordering: RMD → Taxable → Roth → Tax-Deferred

#### Tax Bracket Modeling
- Full 2024 federal tax brackets for all filing statuses (Single, Married Filing Jointly, etc.)
- Inflation-adjusted brackets for multi-year projections
- State income tax rate configuration
- Marginal and effective tax rate calculations

#### Roth Conversion Strategy
- Model annual Roth conversions with configurable amounts
- Set start/end years for conversion window
- Conversions automatically move funds from Tax-Deferred to Roth bucket
- Track tax impact of conversions

#### Big Ticket Items
- Add one-time financial events (inheritance, home sale, large purchase)
- Configure as income or expense with year of occurrence
- Tax treatment options: None, Ordinary Income, Capital Gains
- Integrated into both standard projection and Monte Carlo simulation

#### Historical Backtesting
- Test your retirement plan against 97 years of actual market history (1928-2024)
- Uses real S&P 500 returns, bond yields, and inflation rates
- Identifies worst starting years (1929, 1966, 1973, 2000, 2008)
- Compare historical success rate vs Monte Carlo simulation
- Expandable table showing all historical sequences

### Technical Details
- New `tax.go` with TaxCalculator for federal/state tax calculations
- New `historical_data.go` with embedded market data
- New `backtest.go` for historical sequence testing
- Added comprehensive test coverage for tax and backtesting

---

## v1.0.0 - Initial Release

SimpleBudget is a local-first personal finance dashboard and retirement planning tool. All data stays on your computer - no cloud, no accounts, complete privacy.

### Features

#### Dashboard
- Real-time KPI tracking: income, expenses, net savings, savings rate
- Interactive spending charts with Plotly.js
- Category breakdown with drill-down analysis
- Spending alerts for unusual activity
- CSV export of financial metrics

#### Data Explorer
- Search and filter transactions by date, category, amount, description
- Multi-file CSV import with automatic deduplication
- Flexible parsing handles most bank export formats
- File management with toggle inclusion and deletion

#### What-If Retirement Planner
- 30-year portfolio projections with Monte Carlo simulation (1000 runs)
- Sustainability scoring (0-100) with success probability
- Multiple income sources with COLA adjustments
- Multiple expense categories with inflation tracking
- Healthcare cost modeling for multiple household members
- RMD (Required Minimum Distribution) calculations
- Sequence risk and failure point analysis
- Go-Go/Slow-Go/No-Go spending phase modeling
- Auto-sync income patterns from transaction data

#### Insights
- Automatic recurring payment detection (subscriptions, bills)
- Income pattern analysis (weekly, biweekly, monthly)
- Category spending trends
- Spending velocity tracking

#### Security & Encryption
- **Password** - Simple scrypt-based encryption
- **Age Identity** - X25519 key pair for advanced users
- **SSH Key** - Use existing ed25519 or RSA keys
- **YubiKey** - Hardware security key support

All transaction files and settings are encrypted at rest.

#### Backup & Restore
- Create downloadable ZIP backups
- Restore from previous backups
- Test data for learning the interface

### Technical Details

- Single binary with embedded web assets
- No external database required (CSV/JSON file storage)
- Built with Go, HTMX, Tailwind CSS, Plotly.js
- Cross-platform: Linux, macOS (Intel & Apple Silicon), Windows

### Supported Platforms

| Platform | Architecture | Download |
|----------|--------------|----------|
| Linux | x64 | `budget2_1.0.0_linux_amd64.tar.gz` |
| Linux | ARM64 | `budget2_1.0.0_linux_arm64.tar.gz` |
| macOS | Intel | `budget2_1.0.0_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `budget2_1.0.0_darwin_arm64.tar.gz` |
| Windows | x64 | `budget2_1.0.0_windows_amd64.zip` |

### Getting Started

1. Download the appropriate archive for your platform
2. Extract and run `budget2` (or `budget2.exe` on Windows)
3. Open http://localhost:8080 in your browser
4. Import your bank's CSV transaction export

### Privacy

- 100% local - no data leaves your computer
- No telemetry, analytics, or external connections
- Standard file formats (CSV/JSON) - no vendor lock-in
