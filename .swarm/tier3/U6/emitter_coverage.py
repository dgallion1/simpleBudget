#!/usr/bin/env python3
"""U6 oracle check 9 — every Tailwind colour-utility class emitted from Go or JS
source (outside the template content globs) must have a rule in the built CSS.
Run with cwd = the budget2 tree under test. Exit 0 only when nothing is missing."""
import re, glob, sys, os
UTIL = r'(?:bg|text|border|ring|fill|stroke|from|to|via|divide|placeholder|outline|decoration)'
HUE  = r'(?:red|rose|pink|fuchsia|purple|violet|indigo|blue|sky|cyan|teal|emerald|green|lime|yellow|amber|orange|gray|slate|zinc|neutral|stone|accent|positive|negative|warning)'
TOKEN = re.compile(r'\b((?:dark:)?(?:hover:)?' + UTIL + '-' + HUE + r'(?:-(?:soft|strong|[0-9]{2,3}))?(?:/[0-9]{1,3})?)\b')
srcs = [p for p in glob.glob('internal/**/*.go', recursive=True) if not p.endswith('_test.go')] + glob.glob('web/static/js/**/*.js', recursive=True)
css = open('web/static/css/tailwind.css', encoding='utf-8').read()
def has_rule(cls):
    sel = '.' + re.sub(r'([:/])', r'\\\1', cls)
    return re.search(re.escape(sel) + r'(?=[,{: >\.\[])', css) is not None
missing = {}
seen = set()
for p in srcs:
    for m in TOKEN.finditer(open(p, encoding='utf-8', errors='ignore').read()):
        cls = m.group(1); seen.add(cls)
        if not has_rule(cls): missing.setdefault(cls, set()).add(p)
print(f'scanned {len(srcs)} files, {len(seen)} distinct colour classes emitted from Go/JS')
for cls in sorted(missing):
    print(f'MISSING {cls}  <- {", ".join(sorted(missing[cls]))}')
print('EMITTER COVERAGE: ' + ('clean' if not missing else f'{len(missing)} class(es) missing from built CSS'))
sys.exit(1 if missing else 0)
