package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_EndToEnd creates a synthetic Amazon export + a synthetic CSV
// of bank transactions in a temp dir, runs the enrichment pipeline, and
// confirms the output file lands the expected hash→label entries.
func TestRun_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Synthetic Order History.csv — one matchable row.
	orderCSV := `ASIN,"Billing Address","Carrier Name & Tracking Number",Currency,"Gift Message","Gift Recipient Contact","Gift Sender Name","Item Serial Number","Order Date","Order ID","Order Status","Original Quantity","Payment Method Type","Product Condition","Product Name","Purchase Order Number","Ship Date","Shipment Item Subtotal","Shipment Item Subtotal Tax","Shipment Status","Shipping Address","Shipping Charge","Shipping Option","Total Amount","Total Discounts","Unit Price","Unit Price Tax",Website
B01,"a","UPS",USD,"NA","NA","NA","NA",2024-06-01,111-TEST-0001,Closed,1,"V",New,"Test Coffee Beans","NA",2024-06-02,12.34,0,Shipped,"a",0,std,12.34,0,12.34,0,Amazon.com`
	if err := os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(orderCSV), 0644); err != nil {
		t.Fatal(err)
	}

	// Synthetic bank CSV with one matching Amazon charge + one non-Amazon row.
	bankCSV := "Date,Description,Amount\n" +
		"2024-06-03,AMZN MKTP US,-12.34\n" +
		"2024-06-04,Walmart,-50.00\n"
	if err := os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte(bankCSV), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture stdout — the CLI prints its summary there.
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	done := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	if err := run(amazonDir, dataDir, 5, false, 5); err != nil {
		w.Close()
		t.Fatalf("run() error: %v", err)
	}
	w.Close()
	out := <-done

	enrichPath := filepath.Join(dataDir, "amazon_enrichment.json")
	data, err := os.ReadFile(enrichPath)
	if err != nil {
		t.Fatalf("expected enrichment file at %s: %v", enrichPath, err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 enriched entry, got %d (%v)", len(got), got)
	}
	var label string
	for _, v := range got {
		label = v
	}
	if label != "Amazon: Test Coffee Beans" {
		t.Errorf("label = %q, want %q", label, "Amazon: Test Coffee Beans")
	}
	if !strings.Contains(string(out), "Total enriched:") {
		t.Errorf("expected summary in output, got: %s", out)
	}
}

func TestRun_DryRunDoesNotWrite(t *testing.T) {
	tmp := t.TempDir()
	amazonDir := filepath.Join(tmp, "amazon")
	dataDir := filepath.Join(tmp, "data")
	_ = os.MkdirAll(filepath.Join(amazonDir, "Your Amazon Orders"), 0755)
	_ = os.MkdirAll(dataDir, 0755)

	header := `ASIN,"Order Date","Order ID","Product Name","Ship Date","Total Amount"`
	row := `B01,2024-06-01,111-DRY-0001,"X",2024-06-02,1.00`
	_ = os.WriteFile(filepath.Join(amazonDir, "Your Amazon Orders", "Order History.csv"), []byte(header+"\n"+row), 0644)
	_ = os.WriteFile(filepath.Join(dataDir, "bank.csv"), []byte("Date,Description,Amount\n2024-06-02,Amazon.com,-1.00\n"), 0644)

	// Discard stdout
	stdout := os.Stdout
	devnull, _ := os.Open(os.DevNull)
	os.Stdout = devnull
	defer func() { os.Stdout = stdout }()

	if err := run(amazonDir, dataDir, 5, true, 0); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "amazon_enrichment.json")); !os.IsNotExist(err) {
		t.Errorf("dry-run should not write enrichment file (err=%v)", err)
	}
}

func TestIsAmazonDesc(t *testing.T) {
	cases := map[string]bool{
		"AMZN MKTP US":    true,
		"Amazon.com*ABC":  true,
		"AMAZON PRIME":    true,
		"Walmart":         false,
		"AMZN":            true,
		"":                false,
		"some amazon ad":  true,
	}
	for in, want := range cases {
		if got := isAmazonDesc(in); got != want {
			t.Errorf("isAmazonDesc(%q) = %v, want %v", in, got, want)
		}
	}
}
