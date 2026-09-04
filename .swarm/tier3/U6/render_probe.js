// U6 oracle — rendered probes: no horizontal overflow (details opened) and zero
// color-contrast violations, every page, both themes. Usage:
//   NODE_PATH=<node_modules with playwright + axe-core> node render_probe.js <base-url> <axe.min.js>
const { chromium } = require('playwright');
const fs = require('fs');
const [,, BASE, AXE] = process.argv;
const PAGES = ['/dashboard','/explorer','/whatif','/insights','/major-expenses','/duplicates','/filemanager','/accounts','/transfers'];
const axeSrc = fs.readFileSync(AXE, 'utf8');
(async () => {
  const browser = await chromium.launch();
  let failures = 0;
  for (const theme of ['light','dark']) {
    for (const width of [1440, 1280]) {
      const ctx = await browser.newContext({ viewport: { width, height: 1000 }, colorScheme: theme });
      const page = await ctx.newPage();
      await page.addInitScript(t => { try { localStorage.setItem('theme', t); localStorage.setItem('darkMode', t === 'dark' ? 'true' : 'false'); } catch (e) {} ; document.addEventListener('DOMContentLoaded', () => document.documentElement.classList.toggle('dark', t === 'dark')); }, theme);
      for (const p of PAGES) {
        await page.goto(BASE + p, { waitUntil: 'networkidle' });
        // Reveal every user-reachable surface before auditing: open <details>,
        // activate every tab (role=tab / [data-tab] / .tab-button), and open the
        // HTMX-loaded modals whose triggers are on the page — collapsed/hidden
        // panels are still user-visible content and axe skips hidden nodes
        // (U6 attempt-4 oracle hardening, U-2026-09-04k: the darkened
        // successRateTextClass tiers render inside what-if tabs / dashboard
        // modals the GET-only probe never opened; both U6 checkers flagged this).
        await page.evaluate(() => {
          document.querySelectorAll('details').forEach(d => d.open = true);
          document.querySelectorAll('[role=tab],[data-tab],.tab-button,[data-tab-target]').forEach(t => { try { t.click(); } catch (e) {} });
        });
        await page.waitForTimeout(300);
        // Trigger any HTMX modal openers, then wait for the injected content.
        const openers = await page.$$('[hx-get*=modal],[hx-get*=detail],[hx-get*=drilldown],[data-modal-open],[onclick*=openKPIDetail],[onclick*=Drilldown]');
        for (const o of openers) { try { await o.click({ timeout: 500 }); await page.waitForTimeout(150); } catch (e) {} }
        await page.evaluate(t => { document.documentElement.classList.toggle('dark', t === 'dark'); document.querySelectorAll('details').forEach(d => d.open = true); }, theme);
        await page.waitForTimeout(300);
        const isDark = await page.evaluate(() => document.documentElement.classList.contains('dark'));
        const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
        const sw = await page.evaluate(() => document.documentElement.scrollWidth);
        if ((theme === 'dark') !== isDark) { console.log(`THEME ${p} ${theme} ${width}: forced theme did not apply (html.dark=${isDark})`); failures++; }
        if (sw > width) { console.log(`OVERFLOW ${p} ${theme} ${width}: scrollWidth=${sw} (bg ${bg})`); failures++; }
        if (width === 1440) {
          await page.addScriptTag({ content: axeSrc });
          const res = await page.evaluate(async () => { const r = await axe.run(document, { runOnly: ['color-contrast'] }); return r.violations.map(v => ({ id: v.id, nodes: v.nodes.map(n => n.target.join(' ')).slice(0, 8), count: v.nodes.length })); });
          for (const v of res) { console.log(`CONTRAST ${p} ${theme}: ${v.count} node(s): ${v.nodes.join(' | ')}`); failures++; }
        }
        console.log(`ok ${p} ${theme} ${width}: scrollWidth=${sw} bg=${bg}`);
      }
      await ctx.close();
    }
  }
  await browser.close();
  console.log(failures ? `RENDER PROBE: ${failures} failure(s)` : 'RENDER PROBE: clean');
  process.exit(failures ? 1 : 0);
})().catch(e => { console.log('RENDER PROBE ERROR: ' + e.message); process.exit(2); });
