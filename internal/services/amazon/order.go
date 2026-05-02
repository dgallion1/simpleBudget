// Package amazon parses Amazon order-history exports and matches them
// against bank transactions to produce enriched descriptions.
//
// Two source files are supported:
//
//   - Order History.csv (retail orders)
//   - Digital Content Orders.csv (Kindle, digital purchases, etc.)
//
// Each CSV row is a product line; multi-item shipments produce multiple
// rows sharing one Order ID. We group rows into "shipment groups" keyed
// by (Order ID, Ship Date) for retail or by Order ID for digital, since
// each shipment group corresponds to one bank charge.
package amazon

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Source identifies which Amazon CSV a shipment originated from.
type Source string

const (
	SourceRetail  Source = "retail"
	SourceDigital Source = "digital"
)

// Shipment is a single bank-visible Amazon charge: one shipment's worth
// of products that posted as a single line on the user's statement.
type Shipment struct {
	OrderID   string    // e.g., "111-3021529-4101845"
	OrderDate time.Time // when the user placed the order
	ShipDate  time.Time // when items shipped (retail) or fulfilled (digital)
	Total     float64   // dollar amount the bank sees for this shipment
	Products  []string  // product names, longest-first ordering preserved
	Source    Source
}

// LoadDir reads both Amazon CSVs from a standard export directory.
// Missing files are treated as empty (Amazon doesn't always include
// digital orders in every export). Returns the combined shipment list.
func LoadDir(dir string) ([]Shipment, error) {
	var out []Shipment

	retailPath := filepath.Join(dir, "Your Amazon Orders", "Order History.csv")
	if _, err := os.Stat(retailPath); err == nil {
		f, err := os.Open(retailPath)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", retailPath, err)
		}
		shipments, err := ParseOrderHistory(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", retailPath, err)
		}
		out = append(out, shipments...)
	}

	digitalPath := filepath.Join(dir, "Your Amazon Orders", "Digital Content Orders.csv")
	if _, err := os.Stat(digitalPath); err == nil {
		f, err := os.Open(digitalPath)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", digitalPath, err)
		}
		shipments, err := ParseDigitalContentOrders(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", digitalPath, err)
		}
		out = append(out, shipments...)
	}

	return out, nil
}

// ParseOrderHistory reads a retail Order History.csv stream and returns
// one Shipment per (Order ID, Ship Date) group. Rows with a missing or
// non-positive Total Amount are skipped (cancelled / declined orders).
func ParseOrderHistory(r io.Reader) ([]Shipment, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := indexHeader(header)

	type key struct {
		orderID  string
		shipDate string
	}
	groups := map[key]*Shipment{}
	order := []key{} // preserve first-seen order for stable output

	for lineNum := 2; ; lineNum++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		orderID := col.get(row, "Order ID")
		product := col.get(row, "Product Name")
		amount := parseAmount(col.get(row, "Total Amount"))
		if orderID == "" || amount <= 0 {
			continue
		}

		orderDate := parseTime(col.get(row, "Order Date"))
		shipDate := parseTime(col.get(row, "Ship Date"))
		if shipDate.IsZero() {
			shipDate = orderDate
		}

		k := key{orderID: orderID, shipDate: shipDate.Format("2006-01-02")}
		s, ok := groups[k]
		if !ok {
			s = &Shipment{
				OrderID:   orderID,
				OrderDate: orderDate,
				ShipDate:  shipDate,
				Source:    SourceRetail,
			}
			groups[k] = s
			order = append(order, k)
		}
		s.Total += amount
		if product != "" {
			s.Products = append(s.Products, product)
		}
	}

	out := make([]Shipment, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out, nil
}

// ParseDigitalContentOrders reads a Digital Content Orders.csv stream.
// Each row is one digital item; we group by Order ID. Rows with
// Transaction Amount of 0 (gift-card-funded, free, etc.) are skipped
// because they never hit the user's bank statement.
func ParseDigitalContentOrders(r io.Reader) ([]Shipment, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := indexHeader(header)

	groups := map[string]*Shipment{}
	order := []string{}

	for lineNum := 2; ; lineNum++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		orderID := col.get(row, "Order ID")
		product := col.get(row, "Product Name")
		amount := parseAmount(col.get(row, "Transaction Amount"))
		if orderID == "" || amount <= 0 {
			continue
		}

		orderDate := parseTime(col.get(row, "Order Date"))
		fulfilled := parseTime(col.get(row, "Fulfilled Date"))
		if fulfilled.IsZero() {
			fulfilled = orderDate
		}

		s, ok := groups[orderID]
		if !ok {
			s = &Shipment{
				OrderID:   orderID,
				OrderDate: orderDate,
				ShipDate:  fulfilled,
				Source:    SourceDigital,
			}
			groups[orderID] = s
			order = append(order, orderID)
		}
		s.Total += amount
		if product != "" {
			s.Products = append(s.Products, product)
		}
	}

	out := make([]Shipment, 0, len(order))
	for _, id := range order {
		out = append(out, *groups[id])
	}
	return out, nil
}

// columnIndex maps header names (case-insensitive) to row positions.
type columnIndex map[string]int

func indexHeader(header []string) columnIndex {
	idx := make(columnIndex, len(header))
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return idx
}

func (c columnIndex) get(row []string, name string) string {
	i, ok := c[strings.ToLower(name)]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// parseAmount handles Amazon's quirks: leading apostrophes, single-quote
// wrapping ("'-1.98'"), commas, "Not Applicable", empty strings.
// Negative values become 0 — they represent discounts/refunds embedded
// in pricing fields and shouldn't count as charges.
func parseAmount(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "'")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "$", "")
	if s == "" || strings.EqualFold(s, "Not Applicable") {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "Not Applicable") {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"01/02/2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// SortShipmentsByDate is a deterministic ordering (newest first) helpful
// for test snapshots and the CLI report.
func SortShipmentsByDate(s []Shipment) {
	sort.Slice(s, func(i, j int) bool {
		if !s[i].ShipDate.Equal(s[j].ShipDate) {
			return s[i].ShipDate.After(s[j].ShipDate)
		}
		return s[i].OrderID < s[j].OrderID
	})
}
