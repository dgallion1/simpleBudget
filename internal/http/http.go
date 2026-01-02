package http

import (
	"log"
	"net/http"
	"time"

	"budget2/internal/templates"
)

// RenderTemplate renders a full page template with data
func RenderTemplate(w http.ResponseWriter, renderer *templates.Renderer, templateName string, data map[string]interface{}) {
	if renderer != nil {
		renderer.Render(w, templateName, data)
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>" + templateName + "</h1><p>Templates not loaded. Check configuration.</p></body></html>"))
	}
}

// RenderPartial renders a partial template with data
func RenderPartial(w http.ResponseWriter, renderer *templates.Renderer, partialName string, data map[string]interface{}) {
	if renderer != nil {
		renderer.RenderPartial(w, partialName, data)
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<div><!-- Partial " + partialName + " not loaded --></div>"))
	}
}

// ErrorResponse sends an error response
func ErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	log.Printf("Error: %s (status %d)", message, statusCode)
	http.Error(w, message, statusCode)
}

// ParseDateRange parses start and end date query parameters with defaults
func ParseDateRange(startStr, endStr string, minDate, maxDate time.Time) (start, end time.Time) {
	// Parse start date
	if startStr != "" {
		start, _ = time.Parse("2006-01-02", startStr)
	} else {
		// Default to YTD
		start = time.Date(time.Now().Year(), 1, 1, 0, 0, 0, 0, time.Local)
		// If YTD range starts after our data ends, default to all-time
		if !maxDate.IsZero() && start.After(maxDate) {
			start = minDate
		} else if start.Before(minDate) {
			start = minDate
		}
	}

	// Parse end date
	if endStr != "" {
		end, _ = time.Parse("2006-01-02", endStr)
	} else {
		end = maxDate
	}

	return start, end
}
