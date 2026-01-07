# Retirement Calculation Methodology

This document outlines the technical and financial methodology used by the SimpleBudget Retirement Calculator. 

## 1. Growth & Return Modeling

### Geometric vs. Simple Returns
SimpleBudget uses the **Geometric Conversion** formula to derive monthly returns from annual rates.
- **Formula**: $r_{monthly} = (1 + r_{annual})^{1/12} - 1$
- **Rationale**: Simple division ($r/12$) mathematically overestimates compound growth by approximately 1.4% per year for a typical 10% return. This geometric approach ensures that local monthly projections accurately compound back to the expected annual performance.

### Asset Allocation Blending
When per-account allocation is enabled, the calculator generates annual returns for each asset class (Stocks, Bonds, Cash) and blends them monthly based on the target allocation.
- **Stocks**: S&P 500 Historical Mean (~10.5%)
- **Bonds**: 10-Year Treasury Yield (~5.2%) 
- **Cash**: 3-Month T-Bill Rate (~3.5%)

## 2. Withdrawal Logic (Tax Optimization)

The calculator implements a tax-optimized withdrawal sequence to maximize portfolio longevity:

1. **Required Minimum Distributions (RMDs)**: Mandated withdrawals from Tax-Deferred accounts (calculated first to cover expenses).
2. **Taxable Accounts**: Preferred next to recover cost basis and benefit from Long-Term Capital Gains rates.
3. **Roth Accounts**: Withdrawn third to maximize the duration of tax-free growth.
4. **Tax-Deferred Accounts**: Withdrawn last as they are taxed as ordinary income.

## 3. RMD Compliance
Projections follow the **IRS SECURE 2.0 Act** requirements:
- **Start Age**: 73
- **Factor Table**: Uses the **IRS Uniform Lifetime Table (Table III)** from Publication 590-B.
- **Frequency**: RMDs are calculated based on the prior year-end balance and distributed monthly to mirror a typical retiree's income stream.

## 4. Risk Analysis

### Monte Carlo Simulation
The engine runs 1,000 simulations using the following dynamics:
- **Volatility**: Annual stock volatility of ~15% and bond volatility of ~5%.
- **Crash Modeling**: Includes a ~5% annual probability of a market "crash" (-30% average) with a subsequent mean-reversion "recovery boost."
- **Sequence of Returns Risk**: Explicitly tracks "Early Crashes" (years 1-5) as these have the highest mathematical impact on sustainability.

### Adaptive Spending
If discretionary expenses are configured, the calculator can model "Spending Adaptation," where discretionary costs are automatically reduced by a configurable percentage during market downturns to preserve principal.

---
*Last Technical Audit: 2026-01-06*
