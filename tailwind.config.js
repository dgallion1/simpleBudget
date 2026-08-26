/**
 * Tailwind configuration for the vendored stylesheet.
 *
 * The site used to pull https://cdn.tailwindcss.com at page load, which
 * compiled utilities in the browser. That broke the local-only promise in the
 * README: with no network there was no stylesheet, and every page rendered
 * unstyled. `make css` now compiles web/static/css/tailwind.css ahead of time
 * and it is committed, embedded and served from /static like every other
 * asset.
 *
 * darkMode: 'class' matches the inline `tailwind.config` the CDN script used
 * to be handed. The theme toggle sets `class="dark"` on <html>, so switching
 * to Tailwind's default media strategy would silently break it.
 *
 * The content globs have to cover every place a class name is written, not
 * just the templates. The runtime CDN saw the finished DOM and needed no such
 * list; an ahead-of-time build only emits what it can find in these files:
 *
 *   - web/templates  — markup, plus the class names built in inline <script>
 *   - web/static/js  — classList/className manipulation in the page scripts
 *   - internal/**\/*.go — Go helpers that return class strings to templates
 *     (verdictBandClass, successRateTextClass, colorClass in
 *     internal/templates/render.go, and handlers that emit HTML fragments)
 *
 * A class assembled from pieces at runtime (`"text-" + color + "-500"`) would
 * be invisible to this scan in any of those files. There are none today —
 * every helper returns a complete literal — and new ones must keep to that.
 */
module.exports = {
  darkMode: 'class',
  content: [
    './web/templates/**/*.html',
    './web/static/js/**/*.js',
    './internal/**/*.go',
  ],
  theme: {
    extend: {},
  },
  plugins: [],
}
