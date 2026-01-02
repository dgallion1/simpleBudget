// Package main provides a CLI tool for validating budget server endpoints.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type endpoint struct {
	path        string
	method      string
	contentType string
	contains    []string
}

var endpoints = []endpoint{
	// Main pages
	{path: "/dashboard", method: "GET", contentType: "text/html", contains: []string{"Dashboard", "Total Income"}},
	{path: "/explorer", method: "GET", contentType: "text/html", contains: []string{"Explorer", "Search"}},
	{path: "/whatif", method: "GET", contentType: "text/html", contains: []string{"What-If", "Portfolio"}},
	{path: "/insights", method: "GET", contentType: "text/html", contains: []string{"Insights"}},

	// Dashboard partials
	{path: "/dashboard/kpis", method: "GET", contentType: "text/html", contains: []string{"Total Income"}},
	{path: "/dashboard/alerts", method: "GET", contentType: "text/html", contains: nil},
	{path: "/dashboard/charts/data/monthly", method: "GET", contentType: "application/json", contains: nil},
	{path: "/dashboard/charts/data/category", method: "GET", contentType: "application/json", contains: nil},
	{path: "/dashboard/charts/data/cashflow", method: "GET", contentType: "application/json", contains: nil},
	{path: "/dashboard/charts/data/merchants", method: "GET", contentType: "application/json", contains: nil},
	{path: "/dashboard/charts/data/weekly", method: "GET", contentType: "application/json", contains: nil},
	{path: "/dashboard/charts/data/cumulative", method: "GET", contentType: "application/json", contains: nil},

	// Explorer
	{path: "/explorer/transactions", method: "GET", contentType: "text/html", contains: nil},
	{path: "/explorer/files", method: "GET", contentType: "text/html", contains: nil},

	// Insights partials
	{path: "/insights/recurring", method: "GET", contentType: "text/html", contains: nil},
	{path: "/insights/trends", method: "GET", contentType: "text/html", contains: nil},
	{path: "/insights/trends/chart", method: "GET", contentType: "application/json", contains: nil},
	{path: "/insights/velocity", method: "GET", contentType: "text/html", contains: nil},
	{path: "/insights/income", method: "GET", contentType: "text/html", contains: nil},

	// What-if
	{path: "/whatif/chart/projection", method: "GET", contentType: "application/json", contains: nil},

	// API
	{path: "/api/health", method: "GET", contentType: "application/json", contains: []string{`"status":"ok"`}},
}

type result struct {
	endpoint endpoint
	status   int
	duration time.Duration
	err      error
	body     string
}

func main() {
	url := flag.String("url", "http://localhost:8080", "Base URL of the server to validate")
	verbose := flag.Bool("v", false, "Verbose output")
	timeout := flag.Int("timeout", 10, "Request timeout in seconds")
	flag.Parse()

	client := &http.Client{
		Timeout: time.Duration(*timeout) * time.Second,
	}

	fmt.Printf("Validating server at %s\n", *url)
	fmt.Printf("Testing %d endpoints...\n\n", len(endpoints))

	var passed, failed int
	var results []result

	for _, ep := range endpoints {
		r := validateEndpoint(client, *url, ep, *verbose)
		results = append(results, r)

		if r.err != nil {
			failed++
			fmt.Printf("FAIL %s %s\n", ep.method, ep.path)
			fmt.Printf("     Error: %v\n", r.err)
		} else if r.status != http.StatusOK {
			failed++
			fmt.Printf("FAIL %s %s\n", ep.method, ep.path)
			fmt.Printf("     Status: %d (expected 200)\n", r.status)
		} else {
			passed++
			if *verbose {
				fmt.Printf("PASS %s %s (%v)\n", ep.method, ep.path, r.duration)
			}
		}
	}

	fmt.Printf("\n========================================\n")
	fmt.Printf("Results: %d passed, %d failed\n", passed, failed)

	if failed > 0 {
		os.Exit(1)
	}
}

func validateEndpoint(client *http.Client, baseURL string, ep endpoint, verbose bool) result {
	start := time.Now()

	req, err := http.NewRequest(ep.method, baseURL+ep.path, nil)
	if err != nil {
		return result{endpoint: ep, err: fmt.Errorf("failed to create request: %w", err)}
	}

	resp, err := client.Do(req)
	if err != nil {
		return result{endpoint: ep, err: fmt.Errorf("request failed: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return result{endpoint: ep, err: fmt.Errorf("failed to read body: %w", err)}
	}

	duration := time.Since(start)

	r := result{
		endpoint: ep,
		status:   resp.StatusCode,
		duration: duration,
		body:     string(body),
	}

	// Validate content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, ep.contentType) {
		r.err = fmt.Errorf("wrong content type: got %q, expected %q", ct, ep.contentType)
		return r
	}

	// Validate JSON if expected
	if ep.contentType == "application/json" {
		var js interface{}
		if err := json.Unmarshal(body, &js); err != nil {
			r.err = fmt.Errorf("invalid JSON: %w", err)
			return r
		}
	}

	// Validate required content
	for _, needle := range ep.contains {
		if !strings.Contains(string(body), needle) {
			r.err = fmt.Errorf("missing expected content: %q", needle)
			return r
		}
	}

	return r
}
