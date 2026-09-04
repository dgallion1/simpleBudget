// U6 oracle check 10 — POST/DELETE error paths render a styled banner.
// For each error endpoint: fetch the fragment, inject it into a real page (so the
// app's built CSS applies), and assert in BOTH themes that the banner has a
// non-transparent background, a visible border, uses only token classes (no
// hue literals), and passes axe color-contrast.
//   NODE_PATH=<node_modules with playwright + axe-core> node error_probe.js <base-url> <axe.min.js>
const { chromium } = require('playwright');
const fs = require('fs');
const [,, BASE, AXE] = process.argv;
const axeSrc = fs.readFileSync(AXE, 'utf8');
const CASES = [
  // Field names must be the real form keys (anchor_date/anchor_amount) with
  // valid values, or the handler short-circuits on the "required fields"
  // validation branch (200, full accounts-list-partial re-render, not
  // renderError) before ever reaching the account-lookup 404 this case is
  // named for. Fixed 2026-09-04 (U6 attempt 4): the reconstructed probe
  // (ruling U-2026-09-04j) used the wrong field names (`date`/`amount`),
  // which made check 10 fail on this case for a reason unrelated to the
  // renderError token fix under test.
  { name: 'accounts add-anchor bad id',   method: 'POST',   path: '/accounts/no-such-account/anchor', body: 'anchor_date=2024-01-01&anchor_amount=100' },
  { name: 'whatif expense invalid form',  method: 'POST',   path: '/whatif/expense', body: 'name=&amount=not-a-number' },
  { name: 'major-expenses delete missing', method: 'DELETE', path: '/major-expenses/no-such-expense', body: '' },
];
const HUE = /\b(?:dark:)?(?:bg|text|border)-(?:red|rose|amber|green|emerald|blue|indigo|yellow|orange)-[0-9]{2,3}\b/;
(async () => {
  const browser = await chromium.launch();
  let failures = 0;
  const ctx0 = await browser.newContext();
  const frags = [];
  for (const c of CASES) {
    const r = await ctx0.request.fetch(BASE + c.path, { method: c.method, data: c.body, headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'HX-Request': 'true' } });
    const html = await r.text();
    const status = r.status();
    if (status < 400 || status >= 600) { console.log(`STATUS ${c.name}: HTTP ${status} (expected an error status)`); failures++; }
    if (!/class="[^"]*\b(bg-negative-soft|bg-red-50)\b/.test(html)) { console.log(`FRAGMENT ${c.name}: no error banner in response (${html.slice(0,120).replace(/\s+/g,' ')})`); failures++; }
    if (HUE.test(html)) { console.log(`HUE ${c.name}: hue-literal class in error fragment: ${html.match(HUE)[0]}`); failures++; }
    frags.push({ ...c, html, status });
  }
  await ctx0.close();
  for (const theme of ['light', 'dark']) {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, colorScheme: theme });
    const page = await ctx.newPage();
    await page.addInitScript(t => { try { localStorage.setItem('theme', t); localStorage.setItem('darkMode', t === 'dark' ? 'true' : 'false'); } catch (e) {} }, theme);
    await page.goto(BASE + '/dashboard', { waitUntil: 'networkidle' });
    await page.evaluate(t => document.documentElement.classList.toggle('dark', t === 'dark'), theme);
    for (const f of frags) {
      const res = await page.evaluate(html => {
        const host = document.createElement('div'); host.id = 'u6-error-host'; host.innerHTML = html; document.body.prepend(host);
        const banner = host.firstElementChild;
        const cs = getComputedStyle(banner);
        const out = { bg: cs.backgroundColor, border: cs.borderTopColor, borderW: cs.borderTopWidth, text: '' };
        const msg = banner.querySelector('p'); if (msg) out.text = getComputedStyle(msg).color;
        return out;
      }, f.html);
      const transparent = c => /rgba\(\s*0,\s*0,\s*0,\s*0\)|transparent/.test(c);
      if (transparent(res.bg)) { console.log(`UNSTYLED ${f.name} ${theme}: banner background is transparent (${res.bg})`); failures++; }
      if (res.borderW === '0px' || transparent(res.border)) { console.log(`UNSTYLED ${f.name} ${theme}: banner border missing (${res.borderW} ${res.border})`); failures++; }
      await page.addScriptTag({ content: axeSrc });
      const v = await page.evaluate(async () => { const r = await axe.run(document.getElementById('u6-error-host'), { runOnly: ['color-contrast'] }); return r.violations.map(x => x.nodes.length).reduce((a,b)=>a+b,0); });
      if (v) { console.log(`CONTRAST ${f.name} ${theme}: ${v} node(s) fail color-contrast`); failures++; }
      await page.evaluate(() => document.getElementById('u6-error-host').remove());
      console.log(`ok ${f.name} ${theme}: HTTP ${f.status} bg=${res.bg} border=${res.borderW} ${res.border}`);
    }
    await ctx.close();
  }
  await browser.close();
  console.log(failures ? `ERROR PROBE: ${failures} failure(s)` : 'ERROR PROBE: clean');
  process.exit(failures ? 1 : 0);
})().catch(e => { console.log('ERROR PROBE ERROR: ' + e.message); process.exit(2); });
