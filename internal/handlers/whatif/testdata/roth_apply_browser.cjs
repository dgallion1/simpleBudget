// Real-browser regression. Run only against an isolated synthetic server:
// BUDGET2_TEST_URL=http://127.0.0.1:8082 PLAYWRIGHT_MODULE=/path/to/playwright
// CHROMIUM_EXECUTABLE=/path/to/chrome node internal/handlers/whatif/testdata/roth_apply_browser.cjs
// The fixture must produce a bracket-fill recommendation. This changes that
// synthetic plan, first applying a recommendation, then a fixed annual amount.
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.BUDGET2_TEST_URL || '');
assert(['localhost', '127.0.0.1'].includes(base.hostname) && base.port === '8082',
    'Use the isolated synthetic test server on :8082, never main or demo.');

(async () => {
    const browser = await chromium.launch({
        headless: true,
        executablePath: process.env.CHROMIUM_EXECUTABLE,
        args: ['--no-sandbox'],
    });
    try {
        const page = await browser.newPage({ viewport: { width: 375, height: 900 } });
        await page.goto(new URL('/whatif', base).href, { waitUntil: 'load' });
        const collapse = page.locator('#wf-collapse-strategies-toggle');
        if (await collapse.getAttribute('aria-expanded') === 'false') await collapse.click();
        await page.getByRole('button', { name: 'Find recommendations', exact: true }).click();
        await page.locator('#roth-recommendations-results h3').waitFor({ timeout: 120000 });
        const heading = page.locator('#roth-recommendations-results h3');
        const revision = Number(await heading.getAttribute('data-roth-revision'));
        assert(Number.isSafeInteger(revision), 'Recommendations must expose their generating revision.');
        await heading.focus();
        const choiceCount = await page.locator('#roth-recommendations-results form').count();
        assert(choiceCount > 0, 'Synthetic fixture must generate choices.');
        await page.evaluate(rev => {
            for (const detail of [rev, { value: rev }, { value: rev - 1 }, { value: 'invalid' }]) {
                document.body.dispatchEvent(new CustomEvent('whatif:revision', { detail }));
            }
        }, revision);
        // Also allow the actual first polling response to land after immediate
        // Find. An equal revision must leave both the choices and focus intact.
        await page.waitForTimeout(2500);
        assert.equal(await page.locator('#roth-recommendations-results form').count(), choiceCount);
        assert(await heading.evaluate(node => node === document.activeElement));
        const row = page.locator('#roth-recommendations-results li').filter({ hasText: /Fill .*bracket/ }).first();
        assert(await row.count(), 'Synthetic fixture must rank a bracket-fill recommendation.');
        const rowText = await row.innerText();
        const primary = rowText.match(/primary age (\d+)/)[1];
        const spouse = rowText.match(/spouse age (\d+)/)?.[1];
        await page.evaluate(() => { window.__beforeRothApply = true; });
        await row.getByRole('button', { name: /Apply Roth \+ SS/ }).focus();
        const navigation = page.waitForNavigation({ waitUntil: 'load', timeout: 120000 });
        await page.keyboard.press('Enter');
        await navigation;
        // These observable assertions deliberately fail a same-document fragment
        // navigation; a navigation event alone was insufficient in RA1 attempt 1.
        assert.equal(await page.locator('#roth-conversion-status').innerText(),
            'Roth plan and Social Security claim ages applied. The planner has been refreshed.');
        assert.equal(await page.evaluate(() => document.activeElement.id), 'roth-conversion-heading');
        assert.equal(await page.evaluate(() => window.__beforeRothApply), undefined);
        assert.equal(await page.locator('#ss-claim-age').inputValue(), primary);
        if (spouse) assert.equal(await page.locator('#ss-spouse-claim-age').inputValue(), spouse);
        assert(await page.locator('#roth-saved-schedule').isVisible(), 'Saved schedule must replace the old controls.');
        assert.notEqual(await page.locator('#roth-fixed-fields').getAttribute('disabled'), null);
        assert(await page.locator('#roth-annual-amount').isDisabled());
        assert.equal(new URL(page.url()).hash, '');
        assert.equal(new URL(page.url()).searchParams.has('roth_applied'), false);

        await page.getByRole('button', { name: 'Switch to fixed annual editing' }).click();
        assert.equal(await page.evaluate(() => document.activeElement.id), 'roth-annual-amount');
        await page.evaluate(() => { window.__beforeFixedApply = true; });
        const fixedNavigation = page.waitForNavigation({ waitUntil: 'load', timeout: 120000 });
        await page.locator('#roth-annual-amount').fill('27000');
        await page.locator('#roth-annual-amount').press('Tab');
        await fixedNavigation;
        assert.equal(await page.locator('#roth-conversion-status').innerText(),
            'Fixed annual Roth conversions applied. The planner has been refreshed.');
        assert.equal(await page.evaluate(() => document.activeElement.id), 'roth-conversion-heading');
        assert.equal(await page.evaluate(() => window.__beforeFixedApply), undefined);
        assert.equal(await page.locator('#roth-saved-schedule').count(), 0);
        assert.equal(await page.locator('#roth-annual-amount').inputValue(), '27000');
        assert.equal(new URL(page.url()).hash, '');
        assert.equal(new URL(page.url()).searchParams.has('roth_applied'), false);
        console.log('PASS: recommendation and fixed-edit refresh, saved controls, announcements, focus, and clean URL');
    } finally {
        await browser.close();
    }
})().catch(error => { console.error(error); process.exitCode = 1; });
