// Tailwind config for the vendored, committed static build (T7).
//
// Regeneration recipe (pinned, reproducible — run from repo root):
//   npx --yes tailwindcss@3.4.17 -c tailwind.config.js \
//     -i web/static/css/tailwind.src.css \
//     -o web/static/css/tailwind.css --minify
//
// This replaces the runtime `<script src="https://cdn.tailwindcss.com">` JIT
// (previously in web/templates/layouts/base.html) with a static build so the
// app renders styled while fully offline. `darkMode`/`theme` below match the
// inline `tailwind.config = {...}` that used to sit next to the CDN script.
//
// After running the command above, delete any node_modules/package.json/
// package-lock.json that npx creates in the repo root — only this config,
// web/static/css/tailwind.src.css, and the generated web/static/css/tailwind.css
// are committed deliverables.
module.exports = {
  darkMode: 'class',
  content: [
    './web/templates/**/*.html',
    './web/static/js/**/*.js',
  ],
  theme: {
    extend: {},
  },
  // Classes assembled at runtime (JS template-literal interpolation or Go
  // template helper functions returning class strings) are invisible to the
  // static content scanner above because it only sees literal tokens in the
  // scanned files. Each entry below is a concrete class that some runtime
  // code path can produce; the comment names the source that constructs it.
  safelist: [
    // --- web/templates/components/whatif/spending-phases.html ---
    // `wrColorClass` (withdrawal-rate color, line ~605) and `rmdColorClass`
    // (RMD color, line ~611) are built as JS string literals then spliced
    // into a template-literal `class="..."` via `${wrColorClass}` /
    // `${rmdColorClass}` (lines ~635-636).
    'text-red-600', 'dark:text-red-400',
    'text-amber-600', 'dark:text-amber-400',
    'text-green-600', 'dark:text-green-400',
    'text-blue-600', 'dark:text-blue-400',

    // --- web/templates/components/whatif/rate-assumptions.html ---
    // `colorClass` (line ~858) is a JS string literal spliced into a
    // template-literal `class="..."` via `${colorClass}` (line ~864).
    // ('text-green-600 dark:text-green-400' and 'text-red-600
    // dark:text-red-400' already covered above.)

    // --- internal/templates/render.go: colorClass(v float64) ---
    // Go template func `{{colorClass .NetAmount}}` (used in
    // web/templates/pages/explorer.html) returns one of three literal
    // strings defined in Go source, outside the scanned content globs.
    'text-gray-600', 'dark:text-gray-400',
    // ('text-green-600 dark:text-green-400' and 'text-red-600
    // dark:text-red-400' already covered above.)

    // --- internal/templates/render.go: successRateTextClass(v float64) ---
    // Go template func `{{successRateTextClass ...}}` returns one of five
    // literal strings defined in Go source.
    'text-lime-600', 'dark:text-lime-400',
    'text-yellow-600', 'dark:text-yellow-400',
    'text-orange-600', 'dark:text-orange-400',
    // (green/red variants already covered above.)

    // --- internal/templates/render.go: successRateBarClass(v float64) ---
    // Go template func `{{successRateBarClass ...}}` returns one of five
    // literal bg-color strings defined in Go source.
    'bg-green-500', 'bg-lime-500', 'bg-yellow-500', 'bg-orange-500', 'bg-red-500',

    // --- internal/templates/render.go: verdictClasses map + verdictBandClass/
    // verdictLabelClass/verdictValueClass(h models.Health) ---
    // Go template funcs `{{verdictBandClass $v.Health}}`,
    // `{{verdictLabelClass $v.Health}}`, `{{verdictValueClass $v.Health}}`
    // (used in dashboard-verdict-bar.html, insights-verdict-bar.html,
    // major-expenses-verdict-bar.html, whatif/verdict-bar.html,
    // whatif/historical-backtest.html, whatif/monte-carlo.html,
    // whatif/rate-assumptions.html, whatif/tax-optimizer.html) look up one of
    // four {band, label, value} class-string triples from a Go map, keyed by
    // models.Health (Green/Amber/Red/Neutral), defined in Go source.
    'bg-emerald-50', 'dark:bg-emerald-900/20', 'border-emerald-300', 'dark:border-emerald-700',
    'text-emerald-700', 'dark:text-emerald-300', 'text-emerald-600', 'dark:text-emerald-400',
    'bg-amber-50', 'dark:bg-amber-900/20', 'border-amber-300', 'dark:border-amber-700',
    'text-amber-700', 'dark:text-amber-300',
    // ('text-amber-600 dark:text-amber-400' already covered above.)
    'bg-rose-50', 'dark:bg-rose-900/20', 'border-rose-300', 'dark:border-rose-700',
    'text-rose-700', 'dark:text-rose-300', 'text-rose-600', 'dark:text-rose-400',
    'bg-gray-50', 'dark:bg-gray-800', 'border-gray-200', 'dark:border-gray-700',
    'text-gray-500',
    // ('dark:text-gray-400' and 'text-gray-700 dark:text-gray-200' below.)
    'text-gray-700', 'dark:text-gray-200',
  ],
}
