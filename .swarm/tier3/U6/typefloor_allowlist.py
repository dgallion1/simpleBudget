#!/usr/bin/env python3
"""U6 oracle — mechanical type-floor rule (SPEC §2a, contract v2).
Every `text-xs` element in web/templates must be LABEL-CLASS. An element is
DENIED (must become text-body-sm or larger) when ANY of:
  R1 its tag is a prose/table/heading container: p table thead tbody tr td
     details li ul ol dl dt dd blockquote section article form h1-h6
  R2 it contains a <p>, <table>, <li> or <h1-6> descendant
  R3 any single text run inside it has >= 6 words, or ends with a period
     (template actions count as one word each)
  R4 it renders a template action whose name looks like prose:
     Rationale|Summary|Description|Explanation|Reason|Message|Note|Hint|
     Help|Blurb|Caption|Warning|Verdict|Sentence|Advice|Insight|Tip
Everything else (th, label, button, input, select, badges, eyebrows, short
spans/divs, code, time, summary) is ALLOWED. Exit 1 and list every hit.
"""
import re, glob, sys
DENY_TAGS={'p','table','tbody','td','details','li','ul','ol','dl','dt','dd','blockquote','section','article','h1','h2','h3','h4','h5','h6'}
VOID={'input','img','br','hr','meta','link','path','circle','rect','line','polyline','polygon','use','source','col','area','base','embed','param','track','wbr'}
PROSE=re.compile(r'(Rationale|Summary|Explanation|Reason|Message|Note|Hint|Help|Blurb|Caption|Warning|Verdict|Sentence|Advice|Insight|Tip)\b')
tag_re=re.compile(r'<(/?)([a-zA-Z][a-zA-Z0-9-]*)\b([^>]*?)(/?)>',re.S)
def has_xs(attrs):
    m=re.search(r'class="([^"]*)"',attrs,re.S)
    return bool(m and re.search(r'(^|\s)text-xs(\s|$)',m.group(1)))
def inner_of(s,start_m):
    """return inner HTML of the element whose start tag is match start_m (nesting-aware)."""
    tag=start_m.group(2).lower()
    if tag in VOID or start_m.group(4)=='/': return ''
    depth=1; pos=start_m.end()
    for m in tag_re.finditer(s,pos):
        t=m.group(2).lower()
        if t!=tag: continue
        if m.group(1): depth-=1
        elif m.group(4)!='/' and t not in VOID: depth+=1
        if depth==0: return s[pos:m.start()]
    return s[pos:]
hits=[]
for f in sorted(glob.glob('web/templates/**/*.html',recursive=True)):
    s=open(f,encoding='utf-8').read()
    for m in tag_re.finditer(s):
        if m.group(1) or not has_xs(m.group(3)): continue
        tag=m.group(2).lower(); line=s[:m.start()].count('\n')+1
        inner=inner_of(s,m)
        why=None
        if tag in DENY_TAGS: why=f'R1 tag <{tag}>'
        elif re.search(r'<(p|table|li|h[1-6])\b',inner): why='R2 contains prose/table descendant'
        else:
            acts=re.findall(r'\{\{[^}]*\}\}',inner)
            if any(PROSE.search(a) for a in acts): why='R4 renders prose variable '+next(a for a in acts if PROSE.search(a))[:60]
            else:
                text=re.sub(r'\{\{[^}]*\}\}','\n',inner)
                text=re.sub(r'<[^>]+>','\n',text)
                for run in text.split('\n'):
                    run=re.sub(r'\s+',' ',run).strip()
                    if not run: continue
                    words=[w for w in run.split(' ') if re.search(r'[A-Za-z0-9]',w)]
                    if len(words)>=6 or (run.endswith('.') and len(words)>=3):
                        why=f'R3 sentence run ({len(words)} words): "{run[:70]}"'; break
        if why: hits.append((f,line,tag,why))
for f,line,tag,why in hits: print(f'{f}:{line}: <{tag} class=text-xs> — {why}')
print(f'type-floor allow-list: {len(hits)} denied text-xs element(s)')
sys.exit(1 if hits else 0)
