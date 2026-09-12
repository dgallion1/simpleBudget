package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestLifetimeAccountSpecificAssetReturns(t *testing.T) {
	p := MonthReturns{AssetClassMonthly: &AssetClassMonthReturns{Stock: .10, Bond: .02, Cash: .01}}
	stock := models.LifetimeAccount{StockPercent: 100}
	cash := models.LifetimeAccount{CashPercent: 100}
	if got := lifetimeAccountReturn(stock, p); got != .10 {
		t.Fatalf("stock return=%v", got)
	}
	if got := lifetimeAccountReturn(cash, p); got != .01 {
		t.Fatalf("cash return=%v", got)
	}
}
func TestLifetimeWithdrawTaxableBasisExact(t *testing.T) {
	a := LifetimeAccountRuntime{Account: models.LifetimeAccount{TaxTreatment: "taxable", LegalType: "brokerage"}, Value: 1000, Basis: 600}
	cash, gain, _ := withdrawLifetime(&a, 250)
	if cash != 250 || gain != 100 || math.Abs(a.Basis-450) > .001 || a.Value != 750 {
		t.Fatalf("withdrawal=%v gain=%v state=%+v", cash, gain, a)
	}
}

func TestLifetimeGrossRMDReinvestmentAndSeparateTaxConservation(t *testing.T) {
	traditional := LifetimeAccountRuntime{Account: models.LifetimeAccount{ID: "ira", TaxTreatment: "traditional"}, Value: 100000}
	brokerage := LifetimeAccountRuntime{Account: models.LifetimeAccount{ID: "broker", LegalType: "brokerage", TaxTreatment: "taxable"}}
	cash := LifetimeAccountRuntime{Account: models.LifetimeAccount{ID: "cash", LegalType: "cash", TaxTreatment: "taxable"}, Value: 8000}
	opening := traditional.Value + brokerage.Value + cash.Value
	gross, _, _ := withdrawLifetime(&traditional, 40000)
	depositLifetime(&brokerage, gross)
	paid, _, _ := withdrawLifetime(&cash, 8000)
	closing := traditional.Value + brokerage.Value + cash.Value
	if gross != 40000 || brokerage.Basis != 40000 || paid != 8000 || opening-closing != 8000 {
		t.Fatalf("gross=%v basis=%v tax=%v wealth change=%v", gross, brokerage.Basis, paid, opening-closing)
	}
}
