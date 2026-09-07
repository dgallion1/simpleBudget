// Promoted from /tmp/DI3-a11y.mBCqsi/focused.cjs: F1 label-in-name probe.
// Run against a dedicated synthetic server, never the real/demo instances.
const fs = require('node:fs');
const assert = require('node:assert/strict');
const { chromium } = require(process.env.DI3_PLAYWRIGHT || '/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base = process.env.DI3_BASE || 'http://127.0.0.1:18774';
const url = new URL(base);
assert.equal(url.hostname, '127.0.0.1');
assert.ok(url.port && !['8080', '8081'].includes(url.port));
const visibleLabel = 'Or drop CSV or backup ZIP files anywhere on this page, or click to browse';
const output = process.env.DI3_LABEL_OUTPUT;

(async () => {
    const browser = await chromium.launch({headless:true, executablePath:process.env.DI3_CHROME || '/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome', args:['--no-sandbox']});
    const results = [];
    try {
        for (const dark of [false, true]) {
            const context = await browser.newContext({viewport:{width:390,height:900}, reducedMotion:'reduce'});
            try {
                const page = await context.newPage();
                await page.route('**/*', route => new URL(route.request().url()).origin === url.origin ? route.continue() : route.abort());
                await page.goto(base + '/dashboard?start=2026-01-01&end=2026-08-28');
                await page.evaluate(d => {document.documentElement.classList.toggle('dark', d);window.dispatchEvent(new Event('themechange'));}, dark);
                await page.addScriptTag({path:process.env.DI3_AXE || '/home/darrell/.config/nvm/versions/node/v24.12.0/lib/node_modules/@axe-core/cli/node_modules/axe-core/axe.min.js'});
                const drop = page.locator('#drop-zone');
                const visibleText = (await drop.innerText()).replace(/\s+/g, ' ').trim();
                // getByRole checks the browser-facing computed accessible name,
                // rather than assuming the aria-label attribute is the name.
                const matchingName = await page.getByRole('button', {name:visibleLabel, exact:true}).count();
                const ax = await drop.ariaSnapshot();
                const audit = await page.evaluate(() => axe.run(document, {runOnly:{type:'rule',values:['label-content-name-mismatch']}}));
                const key = dark ? 'Space' : 'Enter';
                await drop.focus();
                const focused = await drop.evaluate(e => e === document.activeElement);
                const chooser = page.waitForEvent('filechooser', {timeout:5000});
                await page.keyboard.press(key);
                await (await chooser).setFiles([]); // select nothing; no upload
                results.push({theme:dark?'dark':'light', visibleText, matchingName, ax, key, focused,
                    violations:audit.violations.map(v => ({id:v.id, targets:v.nodes.map(n => n.target)}))});
            } finally { await context.close(); }
        }
    } finally { await browser.close(); }
    if (output) fs.writeFileSync(output, JSON.stringify(results, null, 2) + '\n');
    console.log(JSON.stringify(results, null, 2));
    for (const result of results) {
        assert.equal(result.visibleText, visibleLabel, 'Visible copy must remain unchanged');
        assert.equal(result.matchingName, 1, 'Rendered accessible name must equal the visible label');
        assert.deepEqual(result.violations, [], 'Explicit axe label-content-name-mismatch rule');
        assert.equal(result.focused, true, 'Drop zone remains keyboard focusable');
    }
})().catch(error => {console.error(error);process.exitCode = 1;});
