package templates

import (
	"bufio"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// getFuncMap returns the template function map
func getFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatMoney":    formatMoney,
		"formatNumber":   formatNumber,
		"formatPercent":  formatPercent,
		"formatDate":     formatDate,
		"formatDateTime": formatDateTime,
		"abs":            abs,
		"add":            add,
		"sub":            sub,
		"mul":            mul,
		"div":            div,
		"mod":            mod,
		"toFloat":        toFloat,
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
		"isNegative":     func(v interface{}) bool { return toFloat(v) < 0 },
		"isPositive":     func(v interface{}) bool { return toFloat(v) > 0 },
		"isNonNegative":  isNonNegative,
		"colorClass":     colorClass,
		"percentOf":      percentOf,
		"percentDiff":    percentDiff,
		"deref":          deref,
	}
}

// loadTemplates parses all templates with strict validation
func (r *Renderer) loadTemplates() error {
	funcMap := getFuncMap()
	tmpl := template.New("").Funcs(funcMap)

	// Collect all template files
	var templateFiles []string
	for _, subdir := range []string{"layouts", "pages", "partials", "components"} {
		subPattern := filepath.Join(r.baseDir, subdir, "*.html")
		matches, err := filepath.Glob(subPattern)
		if err != nil {
			return fmt.Errorf("error globbing %s: %w", subPattern, err)
		}
		templateFiles = append(templateFiles, matches...)
	}

	if len(templateFiles) == 0 {
		return fmt.Errorf("no template files found in %s", r.baseDir)
	}

	// Parse each template file individually for better error reporting
	var parseErrors []string
	for _, file := range templateFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("  %s: failed to read: %v", file, err))
			continue
		}

		_, err = tmpl.New(filepath.Base(file)).Parse(string(content))
		if err != nil {
			// Extract detailed error info
			errMsg := formatTemplateError(file, string(content), err)
			parseErrors = append(parseErrors, errMsg)
		}
	}

	if len(parseErrors) > 0 {
		log.Printf("\n" + strings.Repeat("=", 60))
		log.Printf("TEMPLATE PARSING ERRORS")
		log.Printf(strings.Repeat("=", 60))
		for _, e := range parseErrors {
			log.Printf("%s", e)
		}
		log.Printf(strings.Repeat("=", 60) + "\n")
		return fmt.Errorf("template parsing failed with %d error(s)", len(parseErrors))
	}

	// Validate template references
	if err := r.validateTemplateReferences(tmpl, templateFiles); err != nil {
		return err
	}

	r.templates = tmpl
	log.Printf("Templates loaded successfully: %d files", len(templateFiles))
	return nil
}

// formatTemplateError formats a template error with file context
func formatTemplateError(file, content string, err error) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n  File: %s\n", file))

	// Try to extract line number from error message
	errStr := err.Error()
	lineNum := extractLineNumber(errStr)

	if lineNum > 0 {
		sb.WriteString(fmt.Sprintf("  Line: %d\n", lineNum))
		sb.WriteString(fmt.Sprintf("  Error: %s\n", errStr))
		sb.WriteString("  Context:\n")

		// Show surrounding lines
		lines := strings.Split(content, "\n")
		start := lineNum - 3
		if start < 0 {
			start = 0
		}
		end := lineNum + 2
		if end > len(lines) {
			end = len(lines)
		}

		for i := start; i < end; i++ {
			marker := "   "
			if i+1 == lineNum {
				marker = ">>>"
			}
			sb.WriteString(fmt.Sprintf("    %s %4d | %s\n", marker, i+1, lines[i]))
		}
	} else {
		sb.WriteString(fmt.Sprintf("  Error: %s\n", errStr))
	}

	return sb.String()
}

// extractLineNumber tries to extract a line number from a template error
func extractLineNumber(errStr string) int {
	// Go template errors often contain ":LINE:" pattern
	re := regexp.MustCompile(`:(\d+):`)
	matches := re.FindStringSubmatch(errStr)
	if len(matches) >= 2 {
		var lineNum int
		fmt.Sscanf(matches[1], "%d", &lineNum)
		return lineNum
	}
	return 0
}

// validateTemplateReferences checks that all {{template "name"}} calls reference defined templates
func (r *Renderer) validateTemplateReferences(tmpl *template.Template, files []string) error {
	// Get all defined template names
	definedTemplates := make(map[string]bool)
	for _, t := range tmpl.Templates() {
		if t.Name() != "" {
			definedTemplates[t.Name()] = true
		}
	}

	// Regex to find {{template "name"}} and {{define "name"}} patterns
	templateCallRe := regexp.MustCompile(`\{\{\s*template\s+"([^"]+)"`)
	defineRe := regexp.MustCompile(`\{\{\s*define\s+"([^"]+)"`)

	var refErrors []string

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		// Find all template definitions in this file (for better error messages)
		fileDefines := make(map[string]int) // template name -> line number
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if matches := defineRe.FindStringSubmatch(line); len(matches) >= 2 {
				fileDefines[matches[1]] = lineNum
			}
		}

		// Check all template calls
		scanner = bufio.NewScanner(strings.NewReader(string(content)))
		lineNum = 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			matches := templateCallRe.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if len(match) >= 2 {
					refName := match[1]
					if !definedTemplates[refName] {
						refErrors = append(refErrors, fmt.Sprintf(
							"  %s:%d: undefined template %q\n    Line: %s",
							file, lineNum, refName, strings.TrimSpace(line),
						))
					}
				}
			}
		}
	}

	if len(refErrors) > 0 {
		log.Printf("\n" + strings.Repeat("=", 60))
		log.Printf("UNDEFINED TEMPLATE REFERENCES")
		log.Printf(strings.Repeat("=", 60))
		for _, e := range refErrors {
			log.Printf("%s", e)
		}
		log.Printf(strings.Repeat("=", 60))
		log.Printf("Defined templates:")
		for name := range definedTemplates {
			if name != "" && !strings.HasSuffix(name, ".html") {
				log.Printf("  - %s", name)
			}
		}
		log.Printf(strings.Repeat("=", 60) + "\n")
		return fmt.Errorf("found %d undefined template reference(s)", len(refErrors))
	}

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

func formatNumber(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	formatted := fmt.Sprintf("%.0f", v)

	// Add thousands separators
	var result strings.Builder
	for i, c := range formatted {
		if i > 0 && (len(formatted)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}

	if negative {
		return "-" + result.String()
	}
	return result.String()
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

func add(a, b interface{}) interface{} {
	// If both are ints, return int to preserve type for comparisons
	if ai, ok := a.(int); ok {
		if bi, ok := b.(int); ok {
			return ai + bi
		}
	}
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
func div(a, b interface{}) float64 {
	af := toFloat(a)
	bf := toFloat(b)
	if bf == 0 {
		return 0
	}
	return af / bf
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
		return "text-green-600 dark:text-green-400"
	} else if v < 0 {
		return "text-red-600 dark:text-red-400"
	}
	return "text-gray-600 dark:text-gray-400"
}

func percentOf(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return (part / whole) * 100
}

// percentDiff calculates percentage difference from a reference value
func percentDiff(value, reference float64) float64 {
	if reference == 0 {
		return 0
	}
	return ((value - reference) / reference) * 100
}

// deref safely dereferences a pointer, returning 0 if nil
func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// isNonNegative returns true if v >= 0
func isNonNegative(v float64) bool {
	return v >= 0
}
