#!/usr/bin/env bash
# T7 coverage proof: every literal Tailwind-utility-looking class token found
# in web/templates/**/*.html (including inline <script> blocks) and
# web/static/js/** must resolve to an actual selector in the vendored,
# committed static build at web/static/css/tailwind.css.
#
# This is bash at the entry point (no node/npm/tailwindcss needed to run it);
# the extraction/matching itself is done by an embedded Perl script (a
# standard system tool, like sed/awk) because reliably stripping Go template
# `{{...}}` actions and CSS-escaping arbitrary tokens for a lookahead-bounded
# regex match needs real regex support that plain grep/sed can't do safely
# in one pass.
#
# Usage: swarm/t7-coverage.sh
# Exit 0 = every non-whitelisted token found is covered by tailwind.css.
# Exit non-zero = lists every uncovered token (and, separately, any of the
# known-dynamic safelist entries from tailwind.config.js that turned out NOT
# to be present in the build, which would mean the safelist regenerated
# without effect).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CSS_FILE="web/static/css/tailwind.css"
if [[ ! -f "$CSS_FILE" ]]; then
    echo "FAIL: $CSS_FILE not found — run the pinned build in tailwind.config.js first." >&2
    exit 1
fi

if ! command -v perl >/dev/null 2>&1; then
    echo "FAIL: perl not found on PATH (required by this script)." >&2
    exit 1
fi

set +e
perl -Mstrict -Mwarnings - "$ROOT" <<'PERL_EOF'
use strict;
use warnings;
use File::Find;

my $root = shift @ARGV or die "usage: $0 <repo-root>\n";
chdir $root or die "cannot chdir to $root: $!\n";

my $css_file = "web/static/css/tailwind.css";
open(my $cfh, '<', $css_file) or die "cannot read $css_file: $!\n";
my $css = do { local $/; <$cfh> };
close $cfh;

# ---------------------------------------------------------------------------
# Whitelist: exact class tokens that are NOT Tailwind utilities and are not
# expected to appear as selectors in tailwind.css. Each group names its
# source and why it's exempt.
# ---------------------------------------------------------------------------
my %whitelist = map { $_ => 1 } (
    # --- web/static/css/styles.css (hand-written custom rules) ---
    'chart-container',
    'loading',
    'hover-lift',
    'table-striped',
    'table-hover',
    'major-expenses-pin-check',
    'major-expenses-pin-check-header',
    'major-expenses-pin-check-cell',
    'major-expense-detail-row',       # tr.major-expense-detail-row, styles.css:101
    'wf-tab-active',
    'qa-tab-active',
    'num',

    # --- web/templates/layouts/base.html inline <style> block ---
    'htmx-indicator',
    'htmx-request',
    'htmx-swapping',

    # --- <html> root theme-toggle marker classes (base.html, unlock.html,
    # charts.js/classList.contains) — pure JS state markers, never styled by
    # a CSS rule of their own (Tailwind's `.dark` ancestor selector strategy
    # only ever appears as a *prefix* combinator, e.g. ".dark .foo", never as
    # a bare ".dark{...}" rule) ---
    'light',
    'dark',

    # --- JS querySelector/classList/dataset hook classes with no CSS rule
    # anywhere (verified: `grep -rn '\.<token>\b' web/static/css` is empty
    # for every one of these) ---
    'alias-display',                     # web/templates/pages/explorer.html
    'date-range-btn',                    # web/templates/pages/major-expenses.html, web/static/js/dashboard.js
    'income-display-toggle',             # web/templates/pages/dashboard.html
    'insight-preset-btn',                # web/templates/pages/insights.html
    'major-expense-item-row',            # web/templates/pages/major-expenses.html
    'major-expense-matched-row',         # web/templates/pages/major-expenses.html
    'major-expense-row-toggle',          # web/templates/pages/major-expenses.html
    'major-expenses-bulk-pin-clear',     # web/templates/pages/major-expenses.html
    'major-expenses-bulk-pin-label-lead',  # web/templates/pages/major-expenses.html
    'major-expenses-bulk-pin-label-trail', # web/templates/pages/major-expenses.html
    'major-expenses-exception-row',      # web/templates/pages/major-expenses.html
    'major-expenses-pin-count-chip',     # web/templates/pages/major-expenses.html
    'major-expenses-pin-create-option',  # web/templates/pages/major-expenses.html
    'major-expenses-pin-form',           # web/templates/pages/major-expenses.html
    'major-expenses-pin-select',         # web/templates/pages/major-expenses.html
    'major-expenses-sort-indicator',     # web/templates/pages/major-expenses.html
    'major-expenses-sortable',           # web/templates/pages/major-expenses.html
    'method-form',                       # web/templates/pages/filemanager.html
    'method-tab',                        # web/templates/pages/filemanager.html
    'phase-dollar-label',                # web/templates/components/whatif/spending-phases.html
    'preset-btn',                        # web/templates/pages/dashboard.html, web/static/js/dashboard.js
    'projection-display-toggle',         # web/templates/components/whatif/projection-chart.html
    'sort-arrow',                        # web/templates/pages/major-expenses.html
    'sort-icon',                         # web/templates/pages/major-expenses.html
    'tax-optimizer-card',                # web/templates/components/whatif/tax-optimizer.html
    'wf-tab',                            # web/templates/pages/whatif.html, web/static/js/whatif-tabs.js (only wf-tab-active is styled)
    'caveat',                            # web/templates/components/whatif/rmd.html, tax-optimizer.html

    # --- `class="verdict-{{$v.Health}} ... {{verdictBandClass $v.Health}}"` ---
    # (dashboard-verdict-bar.html, insights-verdict-bar.html,
    # major-expenses-verdict-bar.html, whatif/verdict-bar.html). After
    # stripping the Go `{{...}}` action, the literal text left behind is the
    # fragment "verdict-" (trailing hyphen); the four rendered values
    # (verdict-green/-amber/-red/-neutral) have no CSS rule anywhere and are
    # asserted on only as string markers in
    # internal/handlers/*/verdict_render_test.go and
    # internal/templates/render_major_expenses_verdict_test.go.
    'verdict-',
);

# ---------------------------------------------------------------------------
# Dynamic-class sweep cross-check: every concrete class enumerated in
# tailwind.config.js's `safelist` because some runtime code path constructs
# it (JS template-literal splice or a Go template helper function returning
# a class string) rather than writing it as a literal token. This list must
# stay in sync with tailwind.config.js. We assert each is actually present
# in the build, as a check that the safelist regen took effect.
# ---------------------------------------------------------------------------
my @dynamic_safelist = (
    # web/templates/components/whatif/spending-phases.html: wrColorClass / rmdColorClass
    'text-red-600', 'dark:text-red-400',
    'text-amber-600', 'dark:text-amber-400',
    'text-green-600', 'dark:text-green-400',
    'text-blue-600', 'dark:text-blue-400',
    # internal/templates/render.go: colorClass()
    'text-gray-600', 'dark:text-gray-400',
    # internal/templates/render.go: successRateTextClass()
    'text-lime-600', 'dark:text-lime-400',
    'text-yellow-600', 'dark:text-yellow-400',
    'text-orange-600', 'dark:text-orange-400',
    # internal/templates/render.go: successRateBarClass()
    'bg-green-500', 'bg-lime-500', 'bg-yellow-500', 'bg-orange-500', 'bg-red-500',
    # internal/templates/render.go: verdictClasses map / verdictBandClass / verdictLabelClass / verdictValueClass()
    'bg-emerald-50', 'dark:bg-emerald-900/20', 'border-emerald-300', 'dark:border-emerald-700',
    'text-emerald-700', 'dark:text-emerald-300', 'text-emerald-600', 'dark:text-emerald-400',
    'bg-amber-50', 'dark:bg-amber-900/20', 'border-amber-300', 'dark:border-amber-700',
    'text-amber-700', 'dark:text-amber-300',
    'bg-rose-50', 'dark:bg-rose-900/20', 'border-rose-300', 'dark:border-rose-700',
    'text-rose-700', 'dark:text-rose-300', 'text-rose-600', 'dark:text-rose-400',
    'bg-gray-50', 'dark:bg-gray-800', 'border-gray-200', 'dark:border-gray-700',
    'text-gray-500', 'text-gray-700', 'dark:text-gray-200',
);

# ---------------------------------------------------------------------------
# 1. Collect candidate files.
# ---------------------------------------------------------------------------
my @html_files;
find(sub { push @html_files, $File::Find::name if /\.html$/ }, 'web/templates');
my @js_files;
find(sub { push @js_files, $File::Find::name if /\.js$/ }, 'web/static/js');

# ---------------------------------------------------------------------------
# 2. Extract class tokens.
# ---------------------------------------------------------------------------
my %tokens;

sub harvest_from_text {
    my ($text) = @_;
    # class="..." / className="..." (covers HTML tag attributes and any
    # double-quoted JS string/template-literal fragment containing class="...")
    while ($text =~ /\bclass(?:Name)?\s*=\s*"([^"]*)"/g) {
        $tokens{$_} = 1 for split(/\s+/, $1);
    }
    # className = '...'  (single-quoted JS assignment)
    while ($text =~ /\bclassName\s*=\s*'([^']*)'/g) {
        $tokens{$_} = 1 for split(/\s+/, $1);
    }
    # classList.add(...) / .remove(...) / .contains(...) — every argument is
    # a class name, so grab every quoted string literal inside the parens.
    while ($text =~ /\bclassList\.(?:add|remove|contains)\(([^)]*)\)/g) {
        my $args = $1;
        while ($args =~ /(['"])(.*?)\1/g) {
            $tokens{$_} = 1 for split(/\s+/, $2);
        }
    }
    # classList.toggle('class', forceCondition) — only the FIRST argument is
    # a class name; the second (when present) is a boolean expression that
    # may itself contain an unrelated quoted string (e.g.
    # `clear.classList.toggle('hidden', mode !== 'checked')` in
    # major-expenses.html, where 'checked' is a mode value, not a class), so
    # only the first quoted string literal is harvested.
    while ($text =~ /\bclassList\.toggle\(([^)]*)\)/g) {
        my $args = $1;
        if ($args =~ /(['"])(.*?)\1/) {
            $tokens{$_} = 1 for split(/\s+/, $2);
        }
    }
}

for my $f (@html_files) {
    open(my $fh, '<', $f) or die "cannot read $f: $!\n";
    my $text = do { local $/; <$fh> };
    close $fh;
    # Strip Go template actions ({{...}}, including {{- ... -}} trim forms)
    # so embedded quotes inside e.g. {{if eq .ActiveTab "explorer"}} don't
    # corrupt class="..." attribute parsing. Literal text on either side of
    # an action (both branches of an {{if}}/{{else}}) is left in place.
    $text =~ s/\{\{.*?\}\}/ /gs;
    harvest_from_text($text);
}

for my $f (@js_files) {
    open(my $fh, '<', $f) or die "cannot read $f: $!\n";
    my $text = do { local $/; <$fh> };
    close $fh;
    harvest_from_text($text);
}

# Drop anything that still carries a runtime-interpolation marker: these are
# constructed-fragment leftovers (e.g. "${rmdColorClass}" from a template
# literal, or a stray "${" edge), not literal class names. The concrete
# classes they can produce are handled by the dynamic-class sweep above and
# the tailwind.config.js safelist, not by literal-token coverage.
my @candidates = grep { $_ ne '' && !/[\$\{\}`]/ } keys %tokens;

# ---------------------------------------------------------------------------
# 3. CSS-escape and check each candidate against tailwind.css.
# ---------------------------------------------------------------------------
sub css_escape {
    my ($s) = @_;
    my $out = '';
    for my $ch (split //, $s) {
        if ($ch =~ /[A-Za-z0-9_-]/) {
            $out .= $ch;
        } else {
            $out .= "\\" . $ch;
        }
    }
    return $out;
}

sub covered {
    my ($tok) = @_;
    my $pattern = '\.' . quotemeta(css_escape($tok)) . '(?![A-Za-z0-9_-])';
    return $css =~ /$pattern/;
}

my @missing;
for my $tok (sort @candidates) {
    next if $whitelist{$tok};
    push @missing, $tok unless covered($tok);
}

my @dynamic_missing;
for my $tok (@dynamic_safelist) {
    push @dynamic_missing, $tok unless covered($tok);
}

my $exit = 0;

if (@missing) {
    print "UNCOVERED literal class tokens (not in tailwind.css, not whitelisted):\n";
    print "  - $_\n" for @missing;
    $exit = 1;
}

if (@dynamic_missing) {
    print "MISSING dynamic-safelist entries (tailwind.config.js safelist did not produce these — rebuild needed):\n";
    print "  - $_\n" for @dynamic_missing;
    $exit = 1;
}

if ($exit == 0) {
    print "OK: ", scalar(@candidates) - scalar(grep { $whitelist{$_} } @candidates),
          " literal utility-looking class tokens covered (", scalar(keys %whitelist),
          " whitelisted, ", scalar(@dynamic_safelist), " dynamic-safelist entries verified present).\n";
}

exit $exit;
PERL_EOF
PERL_EXIT=$?
set -e

exit "$PERL_EXIT"
