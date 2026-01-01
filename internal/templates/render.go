package templates

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Renderer handles template rendering
type Renderer struct {
	templates *template.Template
	debug     bool
	baseDir   string
}

// New creates a new template renderer
func New(templateDir string, debug bool) (*Renderer, error) {
	r := &Renderer{
		debug:   debug,
		baseDir: templateDir,
	}

	if err := r.loadTemplates(); err != nil {
		return nil, err
	}

	return r, nil
}

// loadTemplates parses all templates
func (r *Renderer) loadTemplates() error {
	// Define template functions
	funcMap := template.FuncMap{
		"formatMoney":    formatMoney,
		"formatPercent":  formatPercent,
		"formatDate":     formatDate,
		"formatDateTime": formatDateTime,
		"abs":            abs,
		"add":            add,
		"sub":            sub,
		"mul":            mul,
		"div":            div,
		"mod":            mod,
		"seq":            seq,
		"dict":           dict,
		"json":           jsonMarshal,
		"toJSON":         jsonMarshal,
		"lower":          strings.ToLower,
		"upper":          strings.ToUpper,
		"title":          strings.Title,
		"contains":       strings.Contains,
		"hasPrefix":      strings.HasPrefix,
		"hasSuffix":      strings.HasSuffix,
		"trimSpace":      strings.TrimSpace,
		"split":          strings.Split,
		"join":           strings.Join,
		"safeHTML":       safeHTML,
		"safeJS":         safeJS,
		"now":            time.Now,
		"isNegative":     func(v float64) bool { return v < 0 },
		"isPositive":     func(v float64) bool { return v > 0 },
		"colorClass":     colorClass,
		"percentOf":      percentOf,
		"deref":          deref,
	}

	// Create base template with functions
	tmpl := template.New("").Funcs(funcMap)

	// Parse templates from each subdirectory
	for _, subdir := range []string{"layouts", "pages", "partials", "components"} {
		subPattern := filepath.Join(r.baseDir, subdir, "*.html")
		parsed, err := tmpl.ParseGlob(subPattern)
		if err != nil {
			// Ignore if directory doesn't exist or is empty
			log.Printf("Note: no templates in %s", subdir)
		} else {
			tmpl = parsed
		}
	}

	r.templates = tmpl
	return nil
}

// Reload reloads templates (useful for development)
func (r *Renderer) Reload() error {
	return r.loadTemplates()
}

// Render renders a full page with the base layout
func (r *Renderer) Render(w http.ResponseWriter, name string, data interface{}) error {
	// In debug mode, reload templates on each request
	if r.debug {
		if err := r.loadTemplates(); err != nil {
			log.Printf("Error reloading templates: %v", err)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := r.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("Error rendering template %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return err
	}

	return nil
}

// RenderPartial renders a partial template (no base layout)
func (r *Renderer) RenderPartial(w http.ResponseWriter, name string, data interface{}) error {
	if r.debug {
		if err := r.loadTemplates(); err != nil {
			log.Printf("Error reloading templates: %v", err)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := r.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("Error rendering partial %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return err
	}

	return nil
}

// RenderToString renders a template to a string
func (r *Renderer) RenderToString(name string, data interface{}) (string, error) {
	var buf strings.Builder
	if err := r.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ExecuteTemplate executes a template to a writer
func (r *Renderer) ExecuteTemplate(w io.Writer, name string, data interface{}) error {
	return r.templates.ExecuteTemplate(w, name, data)
}

// Template functions

func formatMoney(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	formatted := fmt.Sprintf("%.2f", v)

	// Add thousands separators
	parts := strings.Split(formatted, ".")
	intPart := parts[0]
	var result strings.Builder

	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}

	if len(parts) > 1 {
		result.WriteRune('.')
		result.WriteString(parts[1])
	}

	if negative {
		return "-$" + result.String()
	}
	return "$" + result.String()
}

func formatPercent(v float64) string {
	if v > 0 {
		return fmt.Sprintf("+%.1f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2, 2006")
}

func formatDateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2, 2006 3:04 PM")
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func add(a, b interface{}) float64 {
	af := toFloat(a)
	bf := toFloat(b)
	return af + bf
}
func sub(a, b interface{}) float64 {
	af := toFloat(a)
	bf := toFloat(b)
	return af - bf
}
func mul(a, b interface{}) float64 {
	af := toFloat(a)
	bf := toFloat(b)
	return af * bf
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float64:
		return val
	case float32:
		return float64(val)
	default:
		return 0
	}
}
func div(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}
func mod(a, b int) int {
	if b == 0 {
		return 0
	}
	return a % b
}

// seq generates a sequence of integers
func seq(start, end int) []int {
	if end < start {
		return nil
	}
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}

// dict creates a map from key-value pairs
func dict(values ...interface{}) map[string]interface{} {
	if len(values)%2 != 0 {
		return nil
	}
	result := make(map[string]interface{})
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			continue
		}
		result[key] = values[i+1]
	}
	return result
}

func jsonMarshal(v interface{}) template.JS {
	// Simple JSON encoding for template use
	switch val := v.(type) {
	case []float64:
		parts := make([]string, len(val))
		for i, f := range val {
			parts[i] = fmt.Sprintf("%.2f", f)
		}
		return template.JS("[" + strings.Join(parts, ",") + "]")
	case []string:
		parts := make([]string, len(val))
		for i, s := range val {
			parts[i] = fmt.Sprintf(`"%s"`, s)
		}
		return template.JS("[" + strings.Join(parts, ",") + "]")
	default:
		return template.JS("null")
	}
}

func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

func safeJS(s string) template.JS {
	return template.JS(s)
}

func colorClass(v float64) string {
	if v > 0 {
		return "text-green-600"
	} else if v < 0 {
		return "text-red-600"
	}
	return "text-gray-600"
}

func percentOf(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return (part / whole) * 100
}

// deref safely dereferences a pointer, returning 0 if nil
func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
