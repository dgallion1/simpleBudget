package amazon

import (
	"strings"
	"testing"
	"time"
)

const retailHeader = `ASIN,"Billing Address","Carrier Name & Tracking Number",Currency,"Gift Message","Gift Recipient Contact","Gift Sender Name","Item Serial Number","Order Date","Order ID","Order Status","Original Quantity","Payment Method Type","Product Condition","Product Name","Purchase Order Number","Ship Date","Shipment Item Subtotal","Shipment Item Subtotal Tax","Shipment Status","Shipping Address","Shipping Charge","Shipping Option","Total Amount","Total Discounts","Unit Price","Unit Price Tax",Website`

func TestParseOrderHistory_SingleItem(t *testing.T) {
	csv := retailHeader + "\n" +
		`B01N,"addr","UPS",USD,"NA","NA","NA","NA",2022-03-06T15:41:50Z,111-3021529-4101845,Closed,1,"Visa",New,"Rosemary Whole Organic 1 LB","NA",2022-03-07T17:33:59Z,11.21,0,Shipped,"addr",0,std,11.21,0,11.21,0,Amazon.com`

	got, err := ParseOrderHistory(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 shipment, got %d", len(got))
	}
	s := got[0]
	if s.OrderID != "111-3021529-4101845" {
		t.Errorf("OrderID = %q", s.OrderID)
	}
	if s.Total != 11.21 {
		t.Errorf("Total = %.2f, want 11.21", s.Total)
	}
	if len(s.Products) != 1 || s.Products[0] != "Rosemary Whole Organic 1 LB" {
		t.Errorf("Products = %#v", s.Products)
	}
	if s.ShipDate.Format("2006-01-02") != "2022-03-07" {
		t.Errorf("ShipDate = %v", s.ShipDate)
	}
	if s.Source != SourceRetail {
		t.Errorf("Source = %v", s.Source)
	}
}

func TestParseOrderHistory_GroupsMultiItemShipment(t *testing.T) {
	// Same Order ID + Ship Date → one shipment with summed total.
	csv := retailHeader + "\n" +
		`B01,"a","UPS",USD,"NA","NA","NA","NA",2024-01-01,111-AAA,Closed,1,"V",New,"Coffee Beans","NA",2024-01-02,10.00,0,Shipped,"a",0,std,10.00,0,10.00,0,Amazon.com` + "\n" +
		`B02,"a","UPS",USD,"NA","NA","NA","NA",2024-01-01,111-AAA,Closed,1,"V",New,"Filters","NA",2024-01-02,5.00,0,Shipped,"a",0,std,5.00,0,5.00,0,Amazon.com`

	got, err := ParseOrderHistory(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 grouped shipment, got %d", len(got))
	}
	if got[0].Total != 15.00 {
		t.Errorf("Total = %.2f, want 15.00", got[0].Total)
	}
	if len(got[0].Products) != 2 {
		t.Errorf("Products len = %d", len(got[0].Products))
	}
}

func TestParseOrderHistory_SplitShipmentsByShipDate(t *testing.T) {
	// Same Order ID, different Ship Date → two charges, two shipments.
	csv := retailHeader + "\n" +
		`B01,"a","UPS",USD,"NA","NA","NA","NA",2024-01-01,111-BBB,Closed,1,"V",New,"Item A","NA",2024-01-02,10.00,0,Shipped,"a",0,std,10.00,0,10.00,0,Amazon.com` + "\n" +
		`B02,"a","UPS",USD,"NA","NA","NA","NA",2024-01-01,111-BBB,Closed,1,"V",New,"Item B","NA",2024-01-05,7.50,0,Shipped,"a",0,std,7.50,0,7.50,0,Amazon.com`

	got, err := ParseOrderHistory(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 shipments, got %d", len(got))
	}
}

func TestParseOrderHistory_SkipsCancelledOrZero(t *testing.T) {
	csv := retailHeader + "\n" +
		`B01,"a","UPS",USD,"NA","NA","NA","NA",2024-01-01,111-CCC,Cancelled,1,"V",New,"Item","NA",2024-01-02,0,0,Cancelled,"a",0,std,0,0,0,0,Amazon.com`

	got, err := ParseOrderHistory(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 shipments for zero-total row, got %d", len(got))
	}
}

const digitalHeader = `ASIN,"Affected Item Quantity","Order Date","Order ID","Product Name","Transaction Amount","Fulfilled Date"`

func TestParseDigitalContentOrders_SkipsZeroTransaction(t *testing.T) {
	csv := digitalHeader + "\n" +
		`B003,1,2023-12-17T01:27:00Z,D01-FREE-1,"Free Sample",0,2023-12-17T01:27:00Z` + "\n" +
		`B004,1,2024-02-01T10:00:00Z,D01-PAID-1,"Kindle Book Title",9.99,2024-02-01T10:00:00Z`

	got, err := ParseDigitalContentOrders(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 paid digital order, got %d", len(got))
	}
	if got[0].OrderID != "D01-PAID-1" {
		t.Errorf("OrderID = %q", got[0].OrderID)
	}
	if got[0].Total != 9.99 {
		t.Errorf("Total = %.2f, want 9.99", got[0].Total)
	}
	if got[0].Source != SourceDigital {
		t.Errorf("Source = %v", got[0].Source)
	}
}

func TestParseAmount(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"11.21", 11.21},
		{"  11.21  ", 11.21},
		{"'-1.98'", 0}, // negative pricing-discount field → 0
		{"$1,234.50", 1234.50},
		{"Not Applicable", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := parseAmount(tc.in); got != tc.want {
			t.Errorf("parseAmount(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want string // expected YYYY-MM-DD or "" for zero
	}{
		{"2022-03-06T15:41:50Z", "2022-03-06"},
		{"2024-01-15", "2024-01-15"},
		{"Not Applicable", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := parseTime(tc.in)
		if tc.want == "" {
			if !got.IsZero() {
				t.Errorf("parseTime(%q) = %v, want zero", tc.in, got)
			}
			continue
		}
		if got.Format("2006-01-02") != tc.want {
			t.Errorf("parseTime(%q) = %v, want %s", tc.in, got, tc.want)
		}
	}
}

func TestSortShipmentsByDate(t *testing.T) {
	s := []Shipment{
		{OrderID: "B", ShipDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{OrderID: "A", ShipDate: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
	}
	SortShipmentsByDate(s)
	if s[0].OrderID != "A" {
		t.Errorf("expected newest first; got %v", s[0].OrderID)
	}
}
