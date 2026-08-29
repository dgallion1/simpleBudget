#!/usr/bin/env bash
# T7 coverage proof: every literal Tailwind-utility-looking class token found
# in web/templates/**/*.html (including inline <script> blocks),
# web/static/js/**, and Go source under internal/**/*.go and cmd/**/*.go
# (production code only -- *_test.go is excluded, since test-only strings
# never reach production markup) must resolve to an actual selector in the
# vendored, committed static build at web/static/css/tailwind.css.
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
    'conversion-sweep-card',             # web/templates/components/whatif/conversion-sweep.html: unreferenced marker class (cf. tax-optimizer-card)
    'traj-nominal',                      # web/templates/components/whatif/spending-phases.html: styled by the
    'traj-real',                         #   component's own inline <style> block (nominal/real column toggle),
    'traj-view-real',                    #   not by the Tailwind build (PR #63)

    # --- internal/handlers/approval/handlers.go: const pageCSS (a standalone
    # hand-rolled <style> block for the MCP-approval page, entirely separate
    # from the Tailwind build -- same rationale as the styles.css entries
    # above, just embedded in a Go string instead of static/css/styles.css) ---
    'tool',
    'detail',
    'primary',
    'danger',
    'note',

    # --- internal/handlers/approval/handlers.go: `<meta charset="utf-8">`
    # baked into the raw-string HTML templates (showTmpl/doneTmpl). Not a
    # class at all -- the Go-source content-shape fallback (2b/b2 above)
    # matches any `ident="value"` pair whose value is utility-token-shaped,
    # regardless of what the identifier means, and "charset=" happens to
    # produce one ("utf-8": letters, hyphen, digit). Confirmed single-token,
    # two-occurrence, one-file source; not a Tailwind class, no CSS rule for
    # it anywhere, and the attribute is an HTML meta tag, not class= ---
    'utf-8',

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
my @go_files;
for my $dir ('internal', 'cmd') {
    next unless -d $dir;
    find(sub {
        return unless /\.go$/;
        return if /_test\.go$/;
        push @go_files, $File::Find::name;
    }, $dir);
}

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

# ---------------------------------------------------------------------------
# 2b. Go source (internal/**/*.go, cmd/**/*.go, *_test.go excluded): a class
# token can escape the HTML/JS proof above entirely when it is built in Go
# and only ever reaches a page as a template-helper return value or an
# inline-HTML string -- the `{{...}}` stripping in harvest_from_text's HTML
# path throws away the whole action (and any literal class fragment it
# always evaluates to), and a raw `.go` file is never scanned above at all.
#
# This reuses harvest_from_text's own attribute-style matchers verbatim
# (a Go string can itself hold a literal HTML fragment, e.g. an error-banner
# `fmt.Sprintf` with `class="..."` baked into a backtick string -- exactly
# the case found in internal/handlers/{accounts,whatif,majorexpenses}), and
# reuses the same whitelist/covered() coverage check below for everything it
# finds. It does NOT reuse harvest_from_text's regexes for Go's own
# `ident := "..."` short-declaration / struct-field `ident: "..."` forms,
# since those have no HTML/JS equivalent; those get one small Go-specific
# matcher (harvest_from_go_text) instead of loosening the shared one.
#
# To keep this from drowning in false positives -- Go is full of unrelated
# quoted strings (log messages, SQL, category names, flag names, paths) that
# a blind "every quoted string is a candidate" sweep would flag as
# UNCOVERED and force endless whitelist entries -- extraction is scoped
# instead of loosened at the token-matching end:
#   (a) comments are stripped before any pattern runs (so a comment that
#       happens to mention `class="foo"` in prose can't be mistaken for a
#       real literal), keeping look-up strictly to actual Go string-literal
#       text, never comments or bare identifiers;
#   (b) a token is harvested from a bare (non `class=`-attribute) string
#       literal when EITHER of two independent gates passes:
#         (b1) identifier shape -- it sits inside a "class"-shaped Go
#              identifier's scope: exactly `class`/`className` (any case)
#              for a single assignment/field, or a top-level func/var whose
#              OWN name ends in `Class`, `Classes`, or `ClassName` (e.g.
#              colorClass, verdictClasses) for every string literal in its
#              body. The end-anchored name check is deliberate: "class" as
#              a *prefix* of a longer identifier is a different word in Go
#              (ClassifyTransactions, classifyTransaction, ClassPaired/
#              ClassExternal in this repo are transaction classification,
#              not CSS), so only a name that literally *ends* in
#              Class/Classes/ClassName counts; OR
#         (b2) content shape -- for an `ident := "..."` / `ident: "..."`
#              literal whose identifier is NOT class-shaped, the literal is
#              still harvested if its ENTIRE content, split on whitespace,
#              is a list of Tailwind-utility-SHAPED words (every word
#              matches looks_like_utility_word() below: optional variant
#              prefixes, hyphenated segments, and at least one digit or
#              bracketed arbitrary value -- see that sub for the exact
#              rule). This is what catches `x := "text-fuchsia-333"`, where
#              `x` gives the identifier gate nothing to go on but the
#              literal's own content is unambiguously utility-shaped. A
#              single non-matching word anywhere in the literal (e.g.
#              "please" in "please retry-the-request now") disqualifies
#              the whole literal, so ordinary prose strings stay out
#              without needing a per-string whitelist.
# ---------------------------------------------------------------------------

sub strip_go_comments {
    # Single left-to-right pass that tells string literals apart from
    # comments (a naive "cut from // to end of line" would corrupt a string
    # literal that itself contains "//", e.g. a URL). Comments are blanked
    # out (replaced with spaces, so byte offsets are preserved); double-
    # quoted and backtick string literals are passed through untouched.
    my ($text) = @_;
    my $out = '';
    while ($text =~ /\G(?:("(?:[^"\\]|\\.)*")|(`[^`]*`)|(\/\/[^\n]*)|(\/\*.*?\*\/)|(.))/gs) {
        if    (defined $1) { $out .= $1; }
        elsif (defined $2) { $out .= $2; }
        elsif (defined $3) { $out .= ' ' x length($3); }
        elsif (defined $4) { $out .= ' ' x length($4); }
        else                { $out .= $5; }
    }
    return $out;
}

# Is this Go identifier "class"-shaped per rule (b) above? Exactly
# class/className, or ends in Class/Classes/ClassName as its own trailing
# camelCase word (not a prefix like ClassPaired, not a different word like
# classify/Classify).
sub is_class_ident {
    my ($name) = @_;
    return $name =~ /^(?:class|className)$/i
        || $name =~ /(?:^|[a-z0-9_])(?:Class|Classes|ClassName)$/;
}

sub add_go_tokens {
    my (@words) = @_;
    for my $w (@words) {
        next if $w eq '';
        $tokens{$w} = 1;
    }
}

# Does this single whitespace-delimited word have the SHAPE of a Tailwind
# utility class token -- independent of whether it's actually built into
# tailwind.css (that's covered()'s job, run later)? Used only by the
# content-shape fallback (b2) above, never by the identifier-shape path,
# and never by the HTML/JS harvest_from_text() paths (unchanged).
#
# Shape: zero or more `word:` variant prefixes (dark:, sm:, hover:, ...),
# then a base of one or more hyphen-joined [A-Za-z0-9]+ segments, optionally
# followed by a bracketed arbitrary value (`-[50vh]`) and/or a trailing
# `/NN` opacity fraction (`/30`) -- AND at least one digit or bracket must
# appear somewhere in the base. That last requirement is what separates a
# real utility ("bg-red-500", "px-2", "w-[50%]") from an ordinary
# hyphenated English phrase ("retry-the-request"): Tailwind's numeric/
# arbitrary-value scale is exactly the thing a hand-typed sentence doesn't
# have. This intentionally will not flag bare non-numeric utilities like
# "flex" or "font-medium" from a non-class-shaped identifier -- that's a
# known, accepted gap in the fallback path (see task notes); the identifier-
# shape path (b1) still catches those when the identifier itself says
# "class".
sub looks_like_utility_word {
    my ($w) = @_;
    return 0 if $w eq '';
    my $base = $w;
    $base =~ s/^[a-z][a-zA-Z0-9-]*://g;   # strip variant: prefixes
    return 0 if $base eq '';
    return 0 unless $base =~ /^[A-Za-z]+(?:-[A-Za-z0-9]+)*(?:-\[[^\]\s]+\])?(?:\/[0-9]+)?$/;
    return $base =~ /[0-9\[\]]/;
}

# Does the ENTIRE literal read as a space-separated list of utility-shaped
# words (at least one word, every word passing looks_like_utility_word)?
sub literal_looks_like_utility_list {
    my ($text) = @_;
    my @words = split(/\s+/, $text);
    return 0 unless @words;
    for my $w (@words) {
        return 0 unless looks_like_utility_word($w);
    }
    return 1;
}

sub harvest_from_go_text {
    my ($text) = @_;
    # (a) Same attribute-style forms already used for HTML/JS -- catches a
    # Go string that is itself a literal HTML fragment.
    harvest_from_text($text);

    # (b) `class`/`className` := "..." / = "..." / : "..." (Go short
    # declaration, plain assignment, and struct/map-literal field forms;
    # Go has no single-quoted multi-char string, so only "..." and `...`).
    # Each form is harvested when EITHER the identifier is class-shaped
    # (b1) OR the literal's own content is utility-shaped (b2, see
    # literal_looks_like_utility_list above) -- identifier shape does not
    # gate content that already looks like Tailwind classes on its own.
    while ($text =~ /\b([A-Za-z_]\w*)\s*:?=\s*"([^"]*)"/g) {
        my ($ident, $lit) = ($1, $2);
        add_go_tokens(split(/\s+/, $lit))
            if is_class_ident($ident) || literal_looks_like_utility_list($lit);
    }
    while ($text =~ /\b([A-Za-z_]\w*)\s*:?=\s*`([^`]*)`/g) {
        my ($ident, $lit) = ($1, $2);
        add_go_tokens(split(/\s+/, $lit))
            if is_class_ident($ident) || literal_looks_like_utility_list($lit);
    }
    while ($text =~ /\b([A-Za-z_]\w*)\s*:\s*"([^"]*)"/g) {
        my ($ident, $lit) = ($1, $2);
        add_go_tokens(split(/\s+/, $lit))
            if is_class_ident($ident) || literal_looks_like_utility_list($lit);
    }
    while ($text =~ /\b([A-Za-z_]\w*)\s*:\s*`([^`]*)`/g) {
        my ($ident, $lit) = ($1, $2);
        add_go_tokens(split(/\s+/, $lit))
            if is_class_ident($ident) || literal_looks_like_utility_list($lit);
    }

    # (c) every string literal inside a top-level func/var whose own name is
    # class-shaped (colorClass, successRateTextClass, verdictClasses, ...).
    # gofmt places a top-level declaration's closing brace at column 0, so
    # that's used as the scope end.
    my $in_class_scope = 0;
    for my $line (split /\n/, $text, -1) {
        if ($line =~ /^func\s+(?:\([^)]*\)\s*)?(\w+)/ || $line =~ /^var\s+(\w+)/) {
            $in_class_scope = is_class_ident($1) ? 1 : 0;
        } elsif ($line =~ /^\}/) {
            $in_class_scope = 0;
        }
        next unless $in_class_scope;
        while ($line =~ /"([^"]*)"/g) { add_go_tokens(split(/\s+/, $1)); }
        while ($line =~ /`([^`]*)`/g) { add_go_tokens(split(/\s+/, $1)); }
    }
}

for my $f (@go_files) {
    open(my $fh, '<', $f) or die "cannot read $f: $!\n";
    my $text = do { local $/; <$fh> };
    close $fh;
    harvest_from_go_text(strip_go_comments($text));
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
