package testutil

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// ResponseAssertion provides fluent assertions for HTTP responses
type ResponseAssertion struct {
	t        *testing.T
	resp     *http.Response
	body     string
	bodyRead bool
}

// AssertResponse creates a new ResponseAssertion for the given response
func AssertResponse(t *testing.T, resp *http.Response) *ResponseAssertion {
	t.Helper()
	return &ResponseAssertion{
		t:    t,
		resp: resp,
	}
}

// readBody lazily reads the response body
func (ra *ResponseAssertion) readBody() string {
	if !ra.bodyRead {
		defer ra.resp.Body.Close()
		body, err := io.ReadAll(ra.resp.Body)
		if err != nil {
			ra.t.Fatalf("Failed to read response body: %v", err)
		}
		ra.body = string(body)
		ra.bodyRead = true
	}
	return ra.body
}

// Status asserts the response has the expected status code
func (ra *ResponseAssertion) Status(code int) *ResponseAssertion {
	ra.t.Helper()
	if ra.resp.StatusCode != code {
		ra.t.Errorf("Expected status %d, got %d", code, ra.resp.StatusCode)
	}
	return ra
}

// StatusOK asserts the response has status 200
func (ra *ResponseAssertion) StatusOK() *ResponseAssertion {
	return ra.Status(http.StatusOK)
}

// StatusRedirect asserts the response has a redirect status (3xx)
func (ra *ResponseAssertion) StatusRedirect() *ResponseAssertion {
	ra.t.Helper()
	if ra.resp.StatusCode < 300 || ra.resp.StatusCode >= 400 {
		ra.t.Errorf("Expected redirect status (3xx), got %d", ra.resp.StatusCode)
	}
	return ra
}

// ContentType asserts the response has the expected content type
func (ra *ResponseAssertion) ContentType(expected string) *ResponseAssertion {
	ra.t.Helper()
	ct := ra.resp.Header.Get("Content-Type")
	if !strings.Contains(ct, expected) {
		ra.t.Errorf("Expected Content-Type containing %q, got %q", expected, ct)
	}
	return ra
}

// ContentTypeHTML asserts the response is HTML
func (ra *ResponseAssertion) ContentTypeHTML() *ResponseAssertion {
	return ra.ContentType("text/html")
}

// ContentTypeJSON asserts the response is JSON
func (ra *ResponseAssertion) ContentTypeJSON() *ResponseAssertion {
	return ra.ContentType("application/json")
}

// Contains asserts the response body contains the given string
func (ra *ResponseAssertion) Contains(substr string) *ResponseAssertion {
	ra.t.Helper()
	body := ra.readBody()
	if !strings.Contains(body, substr) {
		ra.t.Errorf("Expected body to contain %q, but it didn't.\nBody (first 500 chars): %s",
			substr, truncate(body, 500))
	}
	return ra
}

// ContainsAll asserts the response body contains all the given strings
func (ra *ResponseAssertion) ContainsAll(substrs ...string) *ResponseAssertion {
	ra.t.Helper()
	body := ra.readBody()
	for _, substr := range substrs {
		if !strings.Contains(body, substr) {
			ra.t.Errorf("Expected body to contain %q, but it didn't.\nBody (first 500 chars): %s",
				substr, truncate(body, 500))
		}
	}
	return ra
}

// NotContains asserts the response body does not contain the given string
func (ra *ResponseAssertion) NotContains(substr string) *ResponseAssertion {
	ra.t.Helper()
	body := ra.readBody()
	if strings.Contains(body, substr) {
		ra.t.Errorf("Expected body NOT to contain %q, but it did", substr)
	}
	return ra
}

// Matches asserts the response body matches the given regex pattern
func (ra *ResponseAssertion) Matches(pattern string) *ResponseAssertion {
	ra.t.Helper()
	body := ra.readBody()
	matched, err := regexp.MatchString(pattern, body)
	if err != nil {
		ra.t.Fatalf("Invalid regex pattern %q: %v", pattern, err)
	}
	if !matched {
		ra.t.Errorf("Expected body to match pattern %q, but it didn't.\nBody (first 500 chars): %s",
			pattern, truncate(body, 500))
	}
	return ra
}

// HasElement asserts the response body contains an HTML element with the given ID
func (ra *ResponseAssertion) HasElement(id string) *ResponseAssertion {
	ra.t.Helper()
	body := ra.readBody()
	// Look for id="elementId" or id='elementId'
	pattern := `id=["']` + regexp.QuoteMeta(id) + `["']`
	matched, _ := regexp.MatchString(pattern, body)
	if !matched {
		ra.t.Errorf("Expected body to contain element with id=%q, but it didn't", id)
	}
	return ra
}

// HasClass asserts the response body contains an element with the given class
func (ra *ResponseAssertion) HasClass(class string) *ResponseAssertion {
	ra.t.Helper()
	body := ra.readBody()
	// Look for class containing the class name
	pattern := `class=["'][^"']*\b` + regexp.QuoteMeta(class) + `\b[^"']*["']`
	matched, _ := regexp.MatchString(pattern, body)
	if !matched {
		ra.t.Errorf("Expected body to contain element with class=%q, but it didn't", class)
	}
	return ra
}

// Body returns the response body as a string
func (ra *ResponseAssertion) Body() string {
	return ra.readBody()
}

// truncate truncates a string to the given length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
