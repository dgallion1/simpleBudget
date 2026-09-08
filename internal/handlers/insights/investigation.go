package insights

import (
	"budget2/internal/models"
	"budget2/internal/services/anomalies"
	insightssvc "budget2/internal/services/insights"
	"budget2/internal/services/merchants"
	"budget2/internal/services/metrics"
	"budget2/internal/services/pricecreep"
	"math"
	"net/url"
	"sort"
)

// InvestigationView leaves raw detector and compatibility data intact.
type InvestigationView struct {
	Current, Previous            float64
	Change                       models.ChangeCell
	Contributors                 []models.CategoryTrend
	Grouping                     string
	GroupIDs                     map[string]string
	Findings, Preview, Remaining []FindingView
	Groups                       []RecurringGroupView
	Monthly, Annual              float64
}
type FindingView struct {
	Transaction                models.Transaction
	Type, Label, Evidence, URL string
	Creep                      *pricecreep.Creep
}
type RecurringRowView struct {
	models.RecurringPayment
	MonthlyEstimate, AnnualEstimate, PaymentEstimate float64
}
type RecurringGroupView struct {
	ID, Label       string
	Rows            []RecurringRowView
	Monthly, Annual float64
}

func buildInvestigation(ts *models.TransactionSet, data *models.InsightsData) InvestigationView {
	p := *data.Period
	v := InvestigationView{Grouping: "Categories", GroupIDs: map[string]string{}}
	if loader != nil {
		if defs, err := loader.LoadMajorExpenses(); err == nil && len(defs) > 0 {
			v.Grouping = "Major Expenses (unmatched outflows excluded)"
			for _, d := range defs {
				v.GroupIDs[d.Name] = d.ID
			}
		}
	}
	v.Current = metrics.ReportingMoney(metrics.SignedNet(insightssvc.TransactionsForPeriod(ts, p.SelectedStart, p.SelectedEnd).FilterByType(models.Outflow)))
	if p.HistoryAvailable {
		v.Previous = metrics.ReportingMoney(metrics.SignedNet(insightssvc.TransactionsForPeriod(ts, p.PreviousStart, p.PreviousEnd).FilterByType(models.Outflow)))
		v.Change = insightssvc.ChangeDisplay(v.Previous, v.Current)
		v.Contributors = append([]models.CategoryTrend(nil), data.CategoryTrends...)
		sort.Slice(v.Contributors, func(i, j int) bool {
			a, b := v.Contributors[i], v.Contributors[j]
			if math.Abs(a.Change.Amount) == math.Abs(b.Change.Amount) {
				return a.Category < b.Category
			}
			return math.Abs(a.Change.Amount) > math.Abs(b.Change.Amount)
		})
		if len(v.Contributors) > 3 {
			v.Contributors = v.Contributors[:3]
		}
	}
	v.Findings = buildFindings(ts, p, anomalies.Detect(*ts), pricecreep.Detect(*ts))
	n := min(5, len(v.Findings))
	v.Preview = v.Findings[:n]
	v.Remaining = v.Findings[n:]
	v.Groups = recurringGroups(data)
	for _, g := range v.Groups {
		v.Monthly += g.Monthly
		v.Annual += g.Annual
	}
	v.Monthly = metrics.ReportingMoney(v.Monthly)
	v.Annual = metrics.ReportingMoney(v.Annual)
	return v
}

// Use the detector's exact active expense grouping, including subset merges.
func buildFindings(ts *models.TransactionSet, p models.PeriodContext, flags []anomalies.Anomaly, creeps []pricecreep.Creep) []FindingView {
	active := ts.Active()
	selected := insightssvc.TransactionsForPeriod(active, p.SelectedStart, p.SelectedEnd)
	byHash := map[string]models.Transaction{}
	for _, t := range selected.Transactions {
		if t.Hash != "" {
			byHash[t.Hash] = t
		}
	}
	seen := map[string]bool{}
	out := []FindingView{}
	add := func(t models.Transaction, kind, label, evidence string, c *pricecreep.Creep) {
		key := t.Hash + "/" + kind
		if t.Hash == "" || seen[key] {
			return
		}
		seen[key] = true
		q := url.Values{"start": {p.SelectedStart.Format("2006-01-02")}, "end": {p.SelectedEnd.Format("2006-01-02")}, "search": {t.Description}, "category": {t.Category}, "type": {"Outflow"}}
		out = append(out, FindingView{Transaction: t, Type: kind, Label: label, Evidence: evidence, Creep: c, URL: "/explorer?" + q.Encode()})
	}
	for _, a := range flags {
		if t, ok := byHash[a.Hash]; ok {
			add(t, "anomaly", anomalyMethodLabel(a.Method), a.Severity+" severity", nil)
		}
	}
	expenses := []models.Transaction{}
	for _, t := range active.Transactions {
		if t.TransactionType == models.Outflow && t.Amount < 0 {
			expenses = append(expenses, t)
		}
	}
	groups := merchants.GroupTransactions(expenses)
	for _, c := range creeps {
		var chosen models.Transaction
		for _, t := range groups[c.GroupKey] {
			if t.Hash == "" {
				continue
			}
			if chosen.Hash == "" || t.Date.After(chosen.Date) || (t.Date.Equal(chosen.Date) && t.Hash > chosen.Hash) {
				chosen = t
			}
		}
		if _, ok := byHash[chosen.Hash]; ok {
			add(chosen, "price-creep", "Price creep", "Median of first three vs last three charges", &c)
		}
	}
	// Largest absolute amount first (SV1): the review list exists to surface
	// what matters most, and sign is irrelevant to how much a row deserves a
	// look. Equal amounts keep the older deterministic order below.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if av, bv := math.Abs(a.Transaction.Amount), math.Abs(b.Transaction.Amount); av != bv {
			return av > bv
		}
		if !a.Transaction.Date.Equal(b.Transaction.Date) {
			return a.Transaction.Date.After(b.Transaction.Date)
		}
		if a.Transaction.Hash != b.Transaction.Hash {
			return a.Transaction.Hash < b.Transaction.Hash
		}
		return a.Type < b.Type
	})
	return out
}

func recurringGroups(data *models.InsightsData) []RecurringGroupView {
	groups := []RecurringGroupView{{ID: "subscriptions", Label: "Detected subscriptions"}, {ID: "bills", Label: "Recurring bills"}, {ID: "other", Label: "Other recurring spending"}}
	for i, rows := range [][]models.RecurringPayment{data.Subscriptions, data.RecurringPayments, data.OtherRecurring} {
		for _, r := range rows {
			row := RecurringRowView{RecurringPayment: r, MonthlyEstimate: metrics.ReportingMoney(r.AnnualCost / 12), AnnualEstimate: metrics.ReportingMoney(r.AnnualCost), PaymentEstimate: metrics.ReportingMoney(r.Amount)}
			groups[i].Rows = append(groups[i].Rows, row)
			groups[i].Monthly += row.MonthlyEstimate
			groups[i].Annual += row.AnnualEstimate
		}
		groups[i].Monthly = metrics.ReportingMoney(groups[i].Monthly)
		groups[i].Annual = metrics.ReportingMoney(groups[i].Annual)
	}
	return groups
}
