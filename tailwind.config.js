// Tailwind config for the vendored, committed static build (T7).
//
// Regenerate with `make css` from the repo root (see the Tailwind section of
// the Makefile). That target pins the same 3.4.17 the CDN was serving and uses
// the standalone CLI, which is one binary in the gitignored tmp/ rather than an
// npm install — so there is no node_modules or package.json left in the repo
// root to clean up. `make css-verify` fails if the committed CSS is stale.
//
// The npm equivalent, if you ever need it, is
// `npx --yes tailwindcss@3.4.17 -c tailwind.config.js -i
// web/static/css/tailwind.src.css -o web/static/css/tailwind.css --minify`.
// Note it does NOT produce byte-identical output: the two front-ends bundle
// different cssnano versions, which order declarations within a rule
// differently. The rules and declarations themselves are identical (verified
// by normalising both and by a pixel diff across every page in both themes),
// but a build made that way will show as stale to `make css-verify`.
//
// This replaces the runtime `<script src="https://cdn.tailwindcss.com">` JIT
// (previously in web/templates/layouts/base.html) with a static build so the
// app renders styled while fully offline. `darkMode`/`theme` below match the
// inline `tailwind.config = {...}` that used to sit next to the CDN script.
//
// Only this config, web/static/css/tailwind.src.css, and the generated
// web/static/css/tailwind.css are committed deliverables. (If you use the npm
// fallback above, delete the node_modules/package.json/package-lock.json that
// npx leaves in the repo root; `make css` creates none of them.)
module.exports = {
  darkMode: 'class',
  content: [
    './web/templates/**/*.html',
    './web/static/js/**/*.js',
  ],
  theme: {
    extend: {
      // U6 semantic palette. Values are CSS custom properties declared on
      // :root/.dark in web/static/css/styles.css so `text-positive` etc.
      // flip color automatically under dark mode with no `dark:` twin.
      colors: {
        accent: {
          DEFAULT: 'rgb(var(--accent) / <alpha-value>)',
          soft: 'rgb(var(--accent-soft) / <alpha-value>)',
          strong: 'rgb(var(--accent-strong) / <alpha-value>)',
        },
        positive: {
          DEFAULT: 'rgb(var(--positive) / <alpha-value>)',
          soft: 'rgb(var(--positive-soft) / <alpha-value>)',
          strong: 'rgb(var(--positive-strong) / <alpha-value>)',
        },
        negative: {
          DEFAULT: 'rgb(var(--negative) / <alpha-value>)',
          soft: 'rgb(var(--negative-soft) / <alpha-value>)',
          strong: 'rgb(var(--negative-strong) / <alpha-value>)',
        },
        warning: {
          DEFAULT: 'rgb(var(--warning) / <alpha-value>)',
          soft: 'rgb(var(--warning-soft) / <alpha-value>)',
          strong: 'rgb(var(--warning-strong) / <alpha-value>)',
        },
        neutral: {
          DEFAULT: 'rgb(var(--neutral) / <alpha-value>)',
          soft: 'rgb(var(--neutral-soft) / <alpha-value>)',
          strong: 'rgb(var(--neutral-strong) / <alpha-value>)',
        },
      },
      // U6 type floor: `text-[10px]`/`text-[11px]` go to zero. `label` is
      // for eyebrows/section labels/badges (uppercase + tracking already
      // applied at the call site); `body-sm` is for anything read as a
      // sentence (helper copy, table cells, targets/deltas/dates).
      fontSize: {
        label: ['0.75rem', { lineHeight: '1rem', letterSpacing: '0.05em' }],
        'body-sm': ['0.875rem', { lineHeight: '1.25rem' }],
      },
    },
  },
  // Classes assembled at runtime (JS template-literal interpolation or Go
  // template helper functions returning class strings) are invisible to the
  // static content scanner above because it only sees literal tokens in the
  // scanned files. Each entry below is a concrete class that some runtime
  // code path can produce; the comment names the source that constructs it.
  safelist: [
    // U6 note: the `wrColorClass`/`rmdColorClass` JS functions this section
    // used to document (spending-phases.html ~605-636) no longer exist in
    // the codebase — stale documentation from an earlier refactor, not a
    // U6 regression. The only surviving runtime-assembled JS color literal
    // is rate-assumptions.html's local `colorClass` (~line 861), which now
    // emits 'text-positive'/'text-negative' directly in that .html file —
    // already covered by the static content scan (no safelist entry
    // needed; the token classes appear literally in a scanned file).

    // --- internal/templates/render.go: colorClass(v float64) ---
    // Go template func `{{colorClass .NetAmount}}` (used in
    // web/templates/pages/explorer.html) now returns one of three U6
    // semantic token classes, defined in Go source outside the scanned
    // content globs.
    'text-positive', 'text-negative', 'text-neutral',

    // --- internal/templates/render.go: successRateTextClass(v float64),
    // successRateBarClass(v float64), verdictClasses map ---
    // U6 judgment call (documented in the U6 task notes): these three are
    // NOT in U6's named conversion scope (only colorClass() and the two
    // whatif JS literals were named). successRateTextClass/BarClass are a
    // five-tier gradient (green/lime/yellow/orange/red) that does not map
    // 1:1 onto the four semantic tokens without merging tiers and losing
    // a visually distinct level — a behavior/design change beyond "hue and
    // size tokens only". verdictClasses (Green/Amber/Red/Neutral) DOES map
    // cleanly onto positive/warning/negative/neutral and is a good
    // low-risk candidate for a follow-up task, but converting it wasn't
    // asked for here, so it is left unchanged and still needs these
    // hue-literal safelist entries. These are Go-source literals; they do
    // NOT count toward U6 criterion (b)'s web/templates hue-family grep.
    'text-red-600', 'dark:text-red-400',
    'text-amber-600', 'dark:text-amber-400',
    'text-green-700', 'dark:text-green-400',
    'text-gray-600', 'dark:text-gray-400',
    // Go template func `{{successRateTextClass ...}}` returns one of five
    // literal strings defined in Go source. U6 attempt-4 (ruling
    // U-2026-09-04j) darkened the light-mode 80/70/60 tiers from -600 to
    // -700: -600 failed 4.5:1 on a white `num` tile (lime-600 3.09:1,
    // yellow-600 2.94:1, orange-600 3.56:1); the -700 shades pass
    // (4.92-5.18:1). dark: variants are unchanged (already passing on a
    // dark ground).
    'text-lime-700', 'dark:text-lime-400',
    'text-yellow-700', 'dark:text-yellow-400',
    'text-orange-700', 'dark:text-orange-400',
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
    // Ruling 2026-08-29e / X8: the map's per-health `value` field (and the
    // Neutral `label`) moved to darker shades to clear 4.5:1 against their
    // own band backgrounds (the insights/major-expenses/whatif verdict-bar
    // value tiles read this map via verdictValueClass; dashboard-verdict-bar.html
    // does not use verdictValueClass and was fixed separately in its own
    // markup) — text-emerald-600/text-amber-600/text-rose-600 are no
    // longer emitted by this map (700 replaces them); text-gray-500 (label,
    // Neutral) is now text-gray-600.
    'bg-emerald-50', 'dark:bg-emerald-900/20', 'border-emerald-300', 'dark:border-emerald-700',
    'text-emerald-700', 'dark:text-emerald-300', 'dark:text-emerald-400',
    'bg-amber-50', 'dark:bg-amber-900/20', 'border-amber-300', 'dark:border-amber-700',
    'text-amber-700', 'dark:text-amber-300',
    // ('dark:text-amber-400' already covered above.)
    'bg-rose-50', 'dark:bg-rose-900/20', 'border-rose-300', 'dark:border-rose-700',
    'text-rose-700', 'dark:text-rose-300', 'dark:text-rose-400',
    'bg-gray-50', 'dark:bg-gray-800', 'border-gray-200', 'dark:border-gray-700',
    // ('text-gray-600 dark:text-gray-400' already covered above.)
    'text-gray-700', 'dark:text-gray-200',
  ],
}
