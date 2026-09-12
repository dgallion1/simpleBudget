// Synthetic browser regression. No running app or personal data is used.
// Run from repo root with PLAYWRIGHT_MODULE, CHROMIUM_PATH and PLOTLY_PATH.
const fs=require('fs'),assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const source=fs.readFileSync('web/templates/components/whatif/guardrail_optimizer.html','utf8');
const root=source.slice(source.indexOf('<section'),source.indexOf('<script>'));
const script=source.match(/<script>([\s\S]*?)<\/script>/)[1];
const panel=source.slice(source.indexOf('<section id="guardrail-graph-preview"'),source.indexOf('</section>',source.indexOf('<section id="guardrail-graph-preview"'))+10);
const html=root.replace('<div id="guardrail-optimizer-results" class="mt-3" aria-busy="false"></div>',`<div id="guardrail-optimizer-results" class="mt-3" aria-busy="false">${['a','b','zero','absent'].map(id=>`<button data-guardrail-graph="${id}" data-request-id="synthetic">View ${id}</button>`).join('')}${panel}</div>`);
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH,args:['--no-sandbox']});
 const page=await browser.newPage({viewport:{width:650,height:850}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.setContent('<html><body class="bg-white dark:bg-gray-900">'+html+'</body></html>');await page.addStyleTag({path:'web/static/css/tailwind.css'});await page.addScriptTag({path:process.env.PLOTLY_PATH});await page.addScriptTag({path:'web/static/js/charts.js'});
 await page.evaluate(()=>{
 window.htmx={process(){}};window.delay=0;window.fail=false;window.requests=0;
 window.fetch=async(url,options)=>{
 if(url.endsWith('/cancel'))return {ok:true};
 window.requests++;if(!url.endsWith('/graph'))throw Error('Unexpected mutation: '+url);
 const id=options.body.get('candidate'),mode=options.body.get('display_dollars'),value=id==='b'?300:id==='zero'?0:100;
 const pct=(factor)=>({p10:value*factor,p50:value*2*factor,p90:value*3*factor});
 const simulation_years=id==='absent'?[]:[1,2].map(year=>({year,paths:year===1?1000:700,living_real:pct(year),living_nominal:pct(year*1.2),portfolio_real:pct(year*100),portfolio_nominal:pct(year*120)}));
 const fetchDelay=window.fetchDelay||0;if(fetchDelay)await new Promise(r=>setTimeout(r,fetchDelay));
 if(window.fail)return {ok:false,json:async()=>({error:'Expired synthetic result'})};
 return {ok:true,json:async()=>({candidate:{id,qualifies:false,simulation_years},display_dollars:mode,success_text:'30.80',target:99,floor_monthly_real:7500,validation_runs:1000,validation_seed:'9223372036854775700',chart:{data:[{name:'Portfolio Balance',x:[0,1],y:[500,400],line:{}}],layout:{xaxis:{},yaxis:{}}}})};
 };
 const original=Plotly.newPlot;Plotly.newPlot=async function(...args){const delay=window.delay;await original.apply(this,args);if(delay)await new Promise(r=>setTimeout(r,delay));};
 });await page.addScriptTag({content:script});
 async function select(id){await page.locator(`[data-guardrail-graph="${id}"]`).click();await page.waitForFunction(id=>document.querySelector(`[data-guardrail-graph="${id}"]`).getAttribute('aria-pressed')==='true',id);}
 async function loaded(){await page.waitForFunction(()=>document.querySelector('#guardrail-optimizer-status').textContent.startsWith('Graph loaded'));}
 await select('a');assert.equal(await page.locator('#guardrail-graph-kind').inputValue(),'simulation');
 assert((await page.locator('#guardrail-graph-note').textContent()).includes('9223372036854775700'));
 assert.equal(await page.evaluate(()=>document.activeElement.id),'guardrail-graph-heading');
 await select('b');assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[2].y),[600,1200]);
 assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-plot').data[2].y),[60000,120000]);
 const rows=await page.locator('#guardrail-graph-data table:first-child tbody tr').allTextContents();assert(rows[1].includes('700'));assert(rows[1].includes('$1,200.00'));
 assert.equal(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[1].fill),'tonexty');
 await page.locator('#guardrail-graph-dollars').selectOption('nominal');await loaded();
 assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[2].y),[720,1440]);assert.equal(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data.length),3);
 await page.locator('#guardrail-graph-kind').selectOption('base');await loaded();assert(await page.locator('#guardrail-graph-living').isHidden());assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-plot').data[0].y),[500,400]);
 await select('zero');assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[2].y),[0,0]);assert(await page.locator('#guardrail-graph-living').isVisible());
 await select('absent');assert(await page.locator('#guardrail-graph-living').isHidden());assert(await page.locator('#guardrail-graph-plot').isHidden());assert((await page.locator('#guardrail-graph-note').textContent()).includes('unavailable'));
 await page.evaluate(()=>window.delay=350);await page.locator('[data-guardrail-graph="a"]').click();await page.waitForTimeout(40);await page.evaluate(()=>window.delay=0);await select('b');await page.waitForTimeout(450);assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[2].y),[600,1200]);
 await page.evaluate(()=>window.delay=350);await page.locator('[data-guardrail-graph="a"]').click();await page.locator('#guardrail-graph-close').click();await page.waitForTimeout(450);assert(await page.locator('#guardrail-graph-preview').isHidden());
 await page.evaluate(()=>window.delay=0);await select('b');await page.evaluate(()=>window.fail=true);await page.locator('[data-guardrail-graph="a"]').click();await page.waitForFunction(()=>document.querySelector('#guardrail-optimizer-status').textContent.includes('Expired'));assert.equal(await page.locator('[data-guardrail-graph="b"]').getAttribute('aria-pressed'),'true');
 await page.locator('#guardrail-graph-dollars').focus();const requests=await page.evaluate(()=>window.requests);await page.evaluate(()=>document.documentElement.classList.add('dark'));await page.waitForFunction(()=>document.querySelector('#guardrail-graph-living').data[2].line.color==='#93c5fd');assert.equal(await page.evaluate(()=>window.requests),requests);assert.equal(await page.evaluate(()=>document.activeElement.id),'guardrail-graph-dollars');
 await page.setViewportSize({width:375,height:800});await page.waitForTimeout(400);assert(await page.evaluate(()=>[...document.querySelectorAll('[data-policy-plot]')].every(plot=>plot.querySelector('svg').getBoundingClientRect().width<=plot.getBoundingClientRect().width+1)));
 await page.evaluate(()=>window.fail=false);await select('a');
 if(process.env.SCREENSHOT_PATH)await page.screenshot({path:process.env.SCREENSHOT_PATH,fullPage:true});
 await page.evaluate(()=>window.fetchDelay=350);await page.locator('[data-guardrail-graph="a"]').click();await page.waitForTimeout(40);await page.evaluate(()=>window.fetchDelay=0);await select('b');await page.waitForTimeout(450);assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[2].y),[600,1200]);
 await page.evaluate(()=>window.fail=true);await page.locator('#guardrail-graph-dollars').selectOption('nominal');await page.waitForFunction(()=>document.querySelector('#guardrail-optimizer-status').textContent.includes('Expired'));assert.equal(await page.locator('#guardrail-graph-dollars').inputValue(),'real');assert.deepEqual(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[2].y),[600,1200]);await page.evaluate(()=>window.fail=false);
 await page.evaluate(()=>window.delay=200);await page.locator('[data-guardrail-graph="a"]').click();await page.waitForTimeout(40);const pendingRequests=await page.evaluate(()=>window.requests);await page.evaluate(()=>document.documentElement.classList.remove('dark'));await page.waitForFunction(()=>document.querySelector('[data-guardrail-graph="a"]').getAttribute('aria-pressed')==='true');assert.equal(await page.evaluate(()=>window.requests),pendingRequests);assert.equal(await page.evaluate(()=>document.querySelector('#guardrail-graph-living').data[2].line.color),'#1e40af');
 await page.locator('#guardrail-optimizer-floor').fill('7400');assert.equal(await page.locator('[data-guardrail-graph]').count(),0);assert.deepEqual(errors,[]);
 await browser.close();console.log('PASS: simulation values, bands, counts, both modes, base case, zeros, unavailable, stale paint, close, errors, theme, narrow viewport, invalidation');
})().catch(error=>{console.error(error);process.exit(1)});
