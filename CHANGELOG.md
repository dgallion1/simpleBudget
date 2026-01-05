# Changelog

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
