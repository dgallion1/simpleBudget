// Package templates loads the html/template files under web/templates,
// registers shared func-map helpers (currency, dates, JSON-encoding for
// HTMX attribute payloads, etc.), and exposes Renderer.Render /
// RenderPartial for the handler packages. Also defines the canonical
// PageData shape used by full-page templates.
package templates

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Renderer handles template rendering
type Renderer struct {
	templates *template.Template
	debug     bool
	baseDir   string
	fsys      fs.FS // embedded filesystem (nil = use os filesystem)
}

// New creates a new template renderer using the filesystem
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

// NewFromFS creates a new template renderer using an embedded filesystem
func NewFromFS(fsys fs.FS, debug bool) (*Renderer, error) {
	r := &Renderer{
		debug: debug,
		fsys:  fsys,
	}

	if err := r.loadTemplates(); err != nil {
		return nil, err
	}

	return r, nil
}

// getFuncMap returns the template function map
func getFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatMoney":                         formatMoney,
		"budgetGapRoundingAdjustment":         budgetGapRoundingAdjustment,
		"formatDollars":                       formatWholeDollars,
		"conversionSummary":                   conversionSummary,
		"formatNumber":                        formatNumber,
		"formatPercent":                       formatPercent,
		"formatMultiplier":                    formatMultiplier,
		"formatDate":                          formatDate,
		"formatDateTime":                      formatDateTime,
		"abs":                                 abs,
		"add":                                 add,
		"sub":                                 sub,
		"mul":                                 mul,
		"div":                                 div,
		"mod":                                 mod,
		"toFloat":                             toFloat,
		"seq":                                 seq,
		"dict":                                dict,
		"slice":                               sliceOf,
		"htmlSelected":                        htmlSelected,
		"json":                                jsonMarshal,
		"toJSON":                              jsonMarshal,
		"lower":                               strings.ToLower,
		"upper":                               strings.ToUpper,
		"title":                               cases.Title(language.English).String,
		"contains":                            strings.Contains,
		"hasPrefix":                           strings.HasPrefix,
		"hasSuffix":                           strings.HasSuffix,
		"trimSpace":                           strings.TrimSpace,
		"split":                               strings.Split,
		"join":                                strings.Join,
		"safeHTML":                            safeHTML,
		"safeHTMLAttr":                        safeHTMLAttr,
		"safeJS":                              safeJS,
		"now":                                 time.Now,
		"isNegative":                          func(v interface{}) bool { return toFloat(v) < 0 },
		"isPositive":                          func(v interface{}) bool { return toFloat(v) > 0 },
		"isNonNegative":                       isNonNegative,
		"colorClass":                          colorClass,
		"successRateTextClass":                successRateTextClass,
		"successRateBarClass":                 successRateBarClass,
		"verdictBandClass":                    verdictBandClass,
		"verdictLabelClass":                   verdictLabelClass,
		"verdictValueClass":                   verdictValueClass,
		"percentOf":                           percentOf,
		"percentDiff":                         percentDiff,
		"deref":                               deref,
		"urlEncode":                           url.PathEscape,
		"withRange":                           withRange,
		"socialSecurityProjectionActive":      retirement.SocialSecurityProjectionActive,
		"ssPortfolioEligible":                 analysis.SSPortfolioEligible,
		"hasManualSocialSecurityIncomeSource": retirement.HasManualSocialSecurityIncomeSource,
		"projectedSSEntries":                  retirement.ProjectedSSEntries,
		"isSocialSecurityIncomeSource":        engine.IsSocialSecurityIncomeSource,
		"startYear":                           engine.ParseStartYear,
	}
}

// loadTemplates parses all templates with strict validation
func (r *Renderer) loadTemplates() error {
	funcMap := getFuncMap()
	tmpl := template.New("").Funcs(funcMap)

	// Collect all template files
	var templateFiles []string

	// Direct subdirectories
	for _, subdir := range []string{"layouts", "pages", "partials", "components"} {
		var matches []string
		var err error

		if r.fsys != nil {
			// Use embedded filesystem
			pattern := subdir + "/*.html"
			matches, err = fs.Glob(r.fsys, pattern)
		} else {
			// Use OS filesystem
			subPattern := filepath.Join(r.baseDir, subdir, "*.html")
			matches, err = filepath.Glob(subPattern)
		}

		if err != nil {
			return fmt.Errorf("error globbing %s: %w", subdir, err)
		}
		templateFiles = append(templateFiles, matches...)
	}

	// Nested component subdirectories (e.g., components/whatif/*.html,
	// components/shared/*.html — the U7 shared partials)
	for _, subdir := range []string{"components/whatif", "components/shared"} {
		var matches []string
		var err error

		if r.fsys != nil {
			pattern := subdir + "/*.html"
			matches, err = fs.Glob(r.fsys, pattern)
		} else {
			subPattern := filepath.Join(r.baseDir, subdir, "*.html")
			matches, err = filepath.Glob(subPattern)
		}

		if err != nil {
			return fmt.Errorf("error globbing %s: %w", subdir, err)
		}
		templateFiles = append(templateFiles, matches...)
	}

	source := r.baseDir
	if r.fsys != nil {
		source = "embedded"
	}
	if len(templateFiles) == 0 {
		return fmt.Errorf("no template files found in %s", source)
	}

	// Parse each template file individually for better error reporting
	var parseErrors []string
	for _, file := range templateFiles {
		var content []byte
		var err error

		if r.fsys != nil {
			content, err = fs.ReadFile(r.fsys, file)
		} else {
			content, err = os.ReadFile(file)
		}

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
		log.Print("\n" + strings.Repeat("=", 60))
		log.Print("TEMPLATE PARSING ERRORS")
		log.Print(strings.Repeat("=", 60))
		for _, e := range parseErrors {
			log.Print(e)
		}
		log.Print(strings.Repeat("=", 60) + "\n")
		return fmt.Errorf("template parsing failed with %d error(s)", len(parseErrors))
	}

	// Validate template references
	if err := r.validateTemplateReferences(tmpl, templateFiles); err != nil {
		return fmt.Errorf("validating template references: %w", err)
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
		// matches[1] is regex-guaranteed digits; on the impossible parse
		// failure lineNum stays 0, which is the same as the fallback below.
		_, _ = fmt.Sscanf(matches[1], "%d", &lineNum)
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
		var content []byte
		var err error

		if r.fsys != nil {
			content, err = fs.ReadFile(r.fsys, file)
		} else {
			content, err = os.ReadFile(file)
		}
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
		log.Print("\n" + strings.Repeat("=", 60))
		log.Print("UNDEFINED TEMPLATE REFERENCES")
		log.Print(strings.Repeat("=", 60))
		for _, e := range refErrors {
			log.Print(e)
		}
		log.Print(strings.Repeat("=", 60))
		log.Print("Defined templates:")
		for name := range definedTemplates {
			if name != "" && !strings.HasSuffix(name, ".html") {
				log.Printf("  - %s", name)
			}
		}
		log.Print(strings.Repeat("=", 60) + "\n")
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
	// Belt (ruling CB7-2026-09-03c): normalize IEEE negative zero to +0
	// before the sign check. `v < 0` is false for -0.0, so an upstream -0.0
	// (an exactly-cancelling window, e.g. metrics.SignedNet's own
	// pre-normalization result before that fix, or any other caller that
	// forgot to normalize) would otherwise fall through to the POSITIVE
	// branch below and let fmt.Sprintf("%.2f", v) honor the sign bit
	// anyway, rendering the literal "$-0.00". `-0.0 == 0` is true in Go, so
	// assigning the literal 0 clears the sign bit.
	if v == 0 {
		v = 0
	}
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

// conversionSummary formats a one-line Avg/Min/Max/Total summary for a
// slice of Roth conversion amounts. Avg/Min/Max use whole-dollar
// comma-separated formatting (no cents) because conversion amounts are
// inherently approximate at the dollar level. Total uses M-abbreviation
// for ≥ $1M (two decimals) and K-abbreviation for ≥ $10K (no decimals);
// smaller totals use whole-dollar formatting.
//
// Returns "" for an empty slice — the template gates the disclosure on
// the slice being non-empty, so this is a defensive belt-and-suspenders.
func conversionSummary(items []models.YearlyConversion) string {
	if len(items) == 0 {
		return ""
	}
	var total float64
	minA := items[0].Amount
	maxA := items[0].Amount
	for _, it := range items {
		total += it.Amount
		if it.Amount < minA {
			minA = it.Amount
		}
		if it.Amount > maxA {
			maxA = it.Amount
		}
	}
	avg := total / float64(len(items))

	yearWord := "years"
	if len(items) == 1 {
		yearWord = "year"
	}

	return fmt.Sprintf("Avg %s  ·  Min %s  ·  Max %s  ·  Total %s over %d %s",
		formatWholeDollars(avg),
		formatWholeDollars(minA),
		formatWholeDollars(maxA),
		formatAbbreviatedTotal(total),
		len(items),
		yearWord,
	)
}

// formatWholeDollars renders v as "$X,XXX" with no cents.
func formatWholeDollars(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	whole := int64(v + 0.5) // round half-up
	formatted := fmt.Sprintf("%d", whole)
	var result strings.Builder
	for i, c := range formatted {
		if i > 0 && (len(formatted)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	if negative {
		return "-$" + result.String()
	}
	return "$" + result.String()
}

// formatAbbreviatedTotal renders v as "$X.YYM" for ≥ $1M, "$XK" for
// ≥ $10K, otherwise "$X,XXX" (no cents). Used only by conversionSummary
// — keep it scoped tightly; do not promote without considering the
// existing formatMoney/formatNumber callers.
func formatAbbreviatedTotal(v float64) string {
	abs := v
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1_000_000:
		s := fmt.Sprintf("$%.2fM", abs/1_000_000)
		if v < 0 {
			return "-" + s
		}
		return s
	case abs >= 10_000:
		s := fmt.Sprintf("$%dK", int64(abs/1_000+0.5))
		if v < 0 {
			return "-" + s
		}
		return s
	default:
		// formatWholeDollars handles its own sign — pass v unchanged.
		return formatWholeDollars(v)
	}
}

func formatNumber(v float64) string {
	if v == 0 {
		v = 0 // IEEE -0 belt, same as formatMoney/formatPercent (CB9)
	}
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
	if v == 0 {
		v = 0 // IEEE -0 would print "-0.0" (sign bit survives %.1f); same belt as formatMoney (CB9)
	}
	if v > 0 {
		return fmt.Sprintf("+%.1f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// formatMultiplier renders a spending-phase multiplier as "1.1" or
// "1.05" -- up to 2 decimal places, with trailing zeros (and a trailing
// decimal point) trimmed. Scoped to the dashboard's Target-provenance
// annotation (kpis.html); not a general-purpose number formatter.
func formatMultiplier(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
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
	case *int:
		if val != nil {
			return float64(*val)
		}
		return 0
	case *int64:
		if val != nil {
			return float64(*val)
		}
		return 0
	case *float64:
		if val != nil {
			return *val
		}
		return 0
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

// htmlSelected returns the literal "selected" attribute text when cond is
// true, else "" — for building a small pre-rendered HTML snippet (e.g. the
// Comparison <select> passed into shared/range-picker.html, U7) with printf
// rather than a template action per option.
func htmlSelected(cond bool) string {
	if cond {
		return "selected"
	}
	return ""
}

// sliceOf builds a []interface{} from its arguments, for building ad-hoc
// lists (e.g. of dicts) in templates that need one (shared/range-picker.html
// preset lists, U7).
func sliceOf(values ...interface{}) []interface{} {
	return values
}

func jsonMarshal(v interface{}) template.JS {
	data, err := json.Marshal(v)
	if err != nil {
		return template.JS("null")
	}
	return template.JS(data)
}

func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

// safeHTMLAttr marks a string as a trusted set of HTML ATTRIBUTES for
// splicing mid-tag (e.g. a block of hx-* attributes on an <input>). Unlike
// safeHTML (template.HTML), which html/template only trusts in element-CONTENT
// context and replaces with the literal "ZgotmplZ" when spliced into a tag's
// attribute area, template.HTMLAttr is trusted in ATTRIBUTE context and
// emitted verbatim. Use for author-controlled attribute strings only, never
// user input.
func safeHTMLAttr(s string) template.HTMLAttr {
	return template.HTMLAttr(s)
}

func safeJS(s string) template.JS {
	return template.JS(s)
}

// colorClass maps a signed amount to a semantic text-color token (U6):
// positive amounts use the `positive` token, negative use `negative`, zero
// uses `neutral`. The token's CSS variable flips light/dark on its own, so
// no `dark:` twin is needed here.
func colorClass(v float64) string {
	if v > 0 {
		return "text-positive"
	} else if v < 0 {
		return "text-negative"
	}
	return "text-neutral"
}

// successRateTextClass keeps aggregate outcomes neutral until a risk target is chosen.
func successRateTextClass(_ float64) string { return "text-gray-800 dark:text-gray-200" }

// verdictClasses maps each models.Health verdict value to the shared Tailwind
// classes for the three verdict band surfaces: the band container
// (background/border), the small-caps label text color, and the tint for a
// health-colored numeric tile value.
var verdictClasses = map[models.Health]struct{ band, label, value string }{
	models.HealthGreen: {
		band:  "bg-emerald-50 dark:bg-emerald-900/20 border-emerald-300 dark:border-emerald-700",
		label: "text-emerald-700 dark:text-emerald-300",
		value: "text-emerald-700 dark:text-emerald-400",
	},
	models.HealthAmber: {
		band:  "bg-amber-50 dark:bg-amber-900/20 border-amber-300 dark:border-amber-700",
		label: "text-amber-700 dark:text-amber-300",
		value: "text-amber-700 dark:text-amber-400",
	},
	models.HealthRed: {
		band:  "bg-rose-50 dark:bg-rose-900/20 border-rose-300 dark:border-rose-700",
		label: "text-rose-700 dark:text-rose-300",
		value: "text-rose-700 dark:text-rose-400",
	},
	models.HealthNeutral: {
		band:  "bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700",
		label: "text-gray-600 dark:text-gray-400",
		value: "text-gray-700 dark:text-gray-200",
	},
}

// verdictClassesFor looks up the verdict classes for a health value. Unknown
// health (zero value or a typo'd constant) renders as red so a missing/wrong
// health value is noticed, matching the whatif verdict bar's old fail-loud
// template ladder.
func verdictClassesFor(h models.Health) struct{ band, label, value string } {
	if c, ok := verdictClasses[h]; ok {
		return c
	}
	return verdictClasses[models.HealthRed]
}

// verdictBandClass maps a models.Health verdict value to the shared Tailwind
// background/border classes for a verdict band container.
func verdictBandClass(h models.Health) string { return verdictClassesFor(h).band }

// verdictLabelClass maps the same health values to the verdict band's
// small-caps label text-color classes.
func verdictLabelClass(h models.Health) string { return verdictClassesFor(h).label }

// verdictValueClass maps the same health values to the tint for a
// health-colored numeric tile value inside a verdict band.
func verdictValueClass(h models.Health) string { return verdictClassesFor(h).value }

// successRateBarClass shares the neutral policy used by successRateTextClass.
func successRateBarClass(_ float64) string { return "bg-accent-strong" }

func percentOf(part, whole interface{}) float64 {
	w := toFloat(whole)
	if w == 0 {
		return 0
	}
	return (toFloat(part) / w) * 100
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

// withRange returns href with "?start=<start>&end=<end>" appended when the
// page data being rendered (pageData, the "." the base layout was executed
// with) carries a non-empty StartDate/EndDate pair, else the bare href.
// This is how base.html's Money-group nav links (Dashboard, Explorer,
// Insights, Major Expenses -- the four range-bearing pages, §2c) propagate
// the current window: Dashboard -> Explorer keeps the same start/end. The
// Plan and Setup nav links call this with a page that never sets those
// keys, so they stay bare (see extractDateRange). Values are query-encoded
// via url.Values, so no manual escaping is required at the call site.
func withRange(href string, pageData interface{}) string {
	start, end := extractDateRange(pageData)
	if start == "" || end == "" {
		return href
	}
	v := url.Values{}
	v.Set("start", start)
	v.Set("end", end)
	return href + "?" + v.Encode()
}

// extractDateRange reads a "StartDate"/"EndDate" pair out of a page-data
// value that may be shaped as map[string]interface{} (most handlers) or a
// struct (accounts, transfers use their own pageData struct with no such
// fields). Either shape resolves without a template-execution error: a
// missing map key or absent struct field simply yields empty strings, which
// withRange treats as "no range set".
func extractDateRange(pageData interface{}) (string, string) {
	if m, ok := pageData.(map[string]interface{}); ok {
		return stringOrEmpty(m["StartDate"]), stringOrEmpty(m["EndDate"])
	}

	rv := reflect.ValueOf(pageData)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return "", ""
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return "", ""
	}
	start := rv.FieldByName("StartDate")
	end := rv.FieldByName("EndDate")
	if !start.IsValid() || start.Kind() != reflect.String {
		return "", ""
	}
	if !end.IsValid() || end.Kind() != reflect.String {
		return "", ""
	}
	return start.String(), end.String()
}

// stringOrEmpty type-asserts v to a string, returning "" for nil or any
// other type (a missing map key comes back as a nil interface{}).
func stringOrEmpty(v interface{}) string {
	s, _ := v.(string)
	return s
}
