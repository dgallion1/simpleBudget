# Changelog

## v1.7.0 - Scenario Chaining & Bug Fixes

### New Features

#### Scenario Chaining
- Chain multiple retirement scenarios to run sequentially with different assumptions at each life phase
- Example: run "Early Retirement" plan from age 60-70, then switch to "Post Social Security" plan from 70 onward
- Portfolio balances carry over between phases — only assumptions change (expenses, income, allocation, healthcare)
- Configure chains in the new "Scenario Chain" card on the What-If page: pick a scenario and a transition age
- Unlimited chain links with ascending transition ages
- Chain-aware across all analysis outputs: projection chart, Monte Carlo, historical backtest, sensitivity, and failure-point analysis
- Budget-fit, present-value, and RMD panels show a note when chain is active (chain support coming in a future release)
- Referential integrity: scenarios referenced in a chain cannot be deleted
- Chain validation on every save: if changing your current age invalidates a chain, it is automatically removed

#### Spending Phase Dollar Amounts
- Spending phase sliders now show both percentage and equivalent monthly dollar amount (e.g., "70% $6,440/mo")
- Updates live as the slider moves

### Bug Fixes
- **Projection depletion during tax-deferred delay**: A temporary shortfall where accessible accounts couldn't cover expenses but locked accounts still had funds was incorrectly treated as permanent depletion, stopping the projection early. Now only true depletion (total balance <= 0) stops the projection.
- **Invalid allocation blocking settings**: If per-account stocks + cash exceeded 100%, all settings changes were rejected with a 400 error. Now values are clamped automatically instead of blocking.
- **Dashboard date filter**: KPIs and alerts now update when the date range is changed
- **Insights date filter**: Date inputs now trigger page refresh on change
- **Recurring payment detection**: Recurring payments are now detected from full transaction history, so short date ranges no longer show $0

## v1.6.0 - Explorer Enhancements

### New Features

#### Transaction Renaming
- Double-click any transaction description to assign a custom display name
- Useful for cryptic entries like "Check #996574" - rename to "Plumber repair"
- Aliases stored in `aliases.json`, encrypted when encryption is enabled
- Original description shown in parentheses next to the alias
- Search matches both original description and custom name
- Clear the name to revert to the original description

#### Date Range Stepping
- Back/forward arrow buttons next to the 3M/6M/12M/All quick-range buttons
- Steps the entire date window forward or backward by its current duration
- Clamped to the min/max bounds of your data

#### Filter Persistence
- Explorer filter state (dates, search, category, sort) persists across tab changes
- Uses sessionStorage so settings survive navigation to other pages and back
- "Clear Filters" resets both the filters and the saved state

## v1.5.0 - Scenarios, Subscriptions & Budget Transparency

### New Features

#### What-If Scenarios
- Named scenarios let you explore different retirement plans without losing your current setup
- Create "Job Loss", "Early Retirement", etc. - each starts as a copy of the active plan
- Switch between scenarios instantly via dropdown at the top of the What-If page
- Rename or delete non-default scenarios; "Current Plan" is always preserved

#### Subscription Tracking (Insights)
- New dedicated Subscriptions section on the Insights page
- Automatically classifies recurring payments as subscriptions vs bills/retail
- Shows monthly subscription total with per-service breakdown
- KPI card added showing subscription cost at a glance

#### Monthly Budget Snapshot (What-If)
- Budget analysis now shows itemized expense and income breakdowns
- Each expense source listed with amount and notes (e.g., "ends year 3", "employer covered")
- Each income source listed with amount and start year
- Net cash flow summary at bottom

#### Healthcare Coverage Type Editing
- Coverage type (Employer/ACA/Medicare) is now a dropdown you can change directly
- Previously was a static label requiring removal and re-adding the person

### Improvements

- Recurring payment detection now uses fuzzy vendor matching — transactions with similar names (e.g., "Lucid" and "Lucidmotors.com") are merged into a single vendor group, so payments aren't missed due to inconsistent bank descriptions
- Amount-based recurring payment detection — payments with identical amounts at regular intervals are detected even when descriptions differ (e.g., check payments and direct bill pay to the same vendor)
- Go version updated to 1.26
- Income source number inputs now update results as you type (not just on blur)
- CSV upload merges new rows into existing files instead of overwriting (prevents data loss)

---

## v1.4.0 - Bug Fixes & Spouse Age Tracking

### Bug Fixes

#### Fixed Monthly Return Calculation
- **Critical fix**: Corrected compound interest calculation for monthly portfolio returns
- Old calculation used simple division (annual/12) instead of geometric conversion
- This was inflating projected returns by ~1.4%/year, compounding to 60% higher final balances
- Now uses correct formula: `monthly = (1 + annual)^(1/12) - 1`

#### RMD Calculations for Couples
- RMD calculations now correctly use the older spouse's age for joint accounts
- Fixed template type mismatch in healthcare timeline comparison

#### Per-Account Allocation Improvements
- Fixed issue where explicit 0% stock allocations were ignored
- Sensitivity analysis now uses effective return rate when in allocation mode
- Asset allocation changes now properly update projection charts

### New Features

#### Spouse Age Tracking
- Added spouse age tracking for retirement spending phases
- Enables more accurate RMD and healthcare cost projections for couples

### UI Improvements
- Show dollar amounts next to account types in asset allocation section
- Moved Income Sources card higher in what-if sidebar for better visibility
- Improved chart loading when settings change

---

## v1.3.0 - Per-Account Asset Allocation

### New Features

#### Per-Account Asset Allocation
- **Independent allocation per account type**: Tax-Deferred, Roth, and Taxable accounts can each have different stock/bond/cash allocations
- Example: Conservative 60/40 for Tax-Deferred, aggressive 90/10 for Taxable brokerage account
- Returns derived from historical means (~10.5% stocks, ~5.2% bonds, ~3.5% cash)
- Auto-rebalancing maintains target allocation each year
- Investment Return slider now acts as override (if set, applies flat rate to all accounts)

#### Enhanced Monte Carlo
- Monte Carlo simulation now uses per-account allocation
- Generates separate stock/bond/cash return sequences, then blends per account
- More realistic modeling of diversified portfolios with different risk profiles

### UI Improvements
- New "Asset Allocation by Account" section replaces single global allocation
- Each account shows Stocks/Cash inputs with calculated Bonds display
- Bond percentage updates dynamically as you adjust

---

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
