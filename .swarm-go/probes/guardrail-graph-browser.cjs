// Run against synthetic data only. Requires Playwright and Chromium; optional PLAYWRIGHT_MODULE and CHROMIUM_PATH select installed runtimes.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base=process.env.GUARDRAIL_TEST_URL; if(!base)throw new Error('Set GUARDRAIL_TEST_URL to a synthetic test server');
const assert=require('assert');
(async()=>{
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH,args:['--no-sandbox']});
const page=await browser.newPage();page.on('pageerror',e=>console.log('PAGEERROR',e.message));
await page.goto(base+'/whatif');
await page.locator('#guardrail-optimizer-target').selectOption('99');
await page.locator('#guardrail-optimizer-run').click();
await page.locator('[data-guardrail-graph]').first().waitFor({timeout:120000});
const buttons=page.locator('[data-guardrail-graph]');console.log('rows',await buttons.count());
for(let i=0;i<await buttons.count();i++){const b=buttons.nth(i);const form={request_id:await b.getAttribute("data-request-id"),candidate:await b.getAttribute("data-guardrail-graph"),display_dollars:"real"};const r=await page.request.post(base+"/whatif/guardrails/optimize/graph",{form});assert.equal(r.status(),200);const j=await r.json();assert(j.chart.data[0].y.length>0);const rejected=await page.request.post(base+"/whatif/guardrails/optimize/apply",{form:{request_id:form.request_id,recommendation:form.candidate}});assert.equal(rejected.status(),409);}console.log("all returned graph tokens render; none grants Apply");
await buttons.first().click();await page.locator('#guardrail-graph-preview').waitFor();
await page.waitForFunction(()=>document.querySelector('#guardrail-optimizer-status').textContent.startsWith('Graph loaded'));
const initial=await page.locator('#guardrail-graph-heading').textContent();
await page.locator('#guardrail-graph-dollars').selectOption('nominal');
await page.waitForFunction(()=>document.querySelector('#guardrail-optimizer-status').textContent.includes('in nominal dollars'));
assert((await page.locator('#guardrail-graph-summary').textContent()).includes('nominal dollars'));
// Delay Plotly completion of selection A, then select B; B must own every surface.
await page.evaluate(()=>{const orig=Plotly.newPlot;window.originalPlot=orig;let n=0;Plotly.newPlot=async function(...args){const p=await orig.apply(this,args);if(++n===1)await new Promise(r=>setTimeout(r,1000));return p;};});
await buttons.first().click();await page.waitForTimeout(100);await buttons.last().click();await page.waitForTimeout(1800);
const lastToken=await buttons.last().getAttribute('data-guardrail-graph');assert.equal(await buttons.last().getAttribute('aria-pressed'),'true');
assert((await page.locator('#guardrail-graph-heading').textContent()).includes('Guardrails disabled'));
const consistent=await page.evaluate(()=>{const ys=document.querySelector('#guardrail-graph-plot').data[0].y;const cells=[...document.querySelectorAll('#guardrail-graph-data table:first-child tbody tr')].map(r=>r.cells[1].textContent);return ys.every((y,i)=>new Intl.NumberFormat('en-US',{style:'currency',currency:'USD'}).format(y)===cells[i]);});assert(consistent);
// Close during an unresolved paint must remain closed after completion.
await page.evaluate(()=>{Plotly.newPlot=async function(...args){const p=await window.originalPlot.apply(this,args);await new Promise(r=>setTimeout(r,600));return p;};});
await buttons.first().click();await page.waitForTimeout(100);await page.locator('#guardrail-graph-close').click();await page.waitForTimeout(1000);assert(await page.locator('#guardrail-graph-preview').isHidden());
// Failed request keeps previous comparison and announces error.
await page.evaluate(()=>Plotly.newPlot=window.originalPlot);await buttons.last().click();await page.locator('#guardrail-graph-preview').waitFor();
await page.route('**/whatif/guardrails/optimize/graph',route=>route.fulfill({status:409,contentType:'application/json',body:JSON.stringify({error:'Injected expired graph'})}));await buttons.first().click();await page.waitForTimeout(300);assert((await page.locator('#guardrail-optimizer-status').textContent()).includes('Injected expired graph'));assert.equal(await buttons.last().getAttribute('aria-pressed'),'true');assert((await page.locator('#guardrail-graph-heading').textContent()).includes('Guardrails disabled'));
await page.unroute('**/whatif/guardrails/optimize/graph');await page.locator('#guardrail-optimizer-floor').fill('7400');assert.equal(await buttons.count(),0);assert.equal(await page.locator('#guardrail-graph-preview').count(),0);
console.log('PASS nominal, all surfaces coherent after reversed Plotly completion, close-inflight, error retains comparison, input clears');await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
