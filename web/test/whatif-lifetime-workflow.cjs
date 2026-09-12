// Caller supplies a fresh synthetic September 2026 scenario: Lifetime absent,
// primary person born 1961-09, 30 projection years, and one replaceable income.
// Before launching, verify the server's DATA, BACKUP and IMPORT directories are
// all isolated under /tmp. This test intentionally saves synthetic settings.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const root = process.env.LP6_NODE_MODULE_ROOT;
const modulePath = name => root ? path.join(root, name) : name;
const {Builder, By, Key, until} = require(modulePath('selenium-webdriver'));
const chrome = require(modulePath('selenium-webdriver/chrome'));
assert.ok(process.env.LP6_URL, 'LP6_URL must identify the caller-owned synthetic server');
assert.ok(process.env.LP6_DEBUGGER_ADDRESS, 'LP6_DEBUGGER_ADDRESS is required');
const url = process.env.LP6_URL.replace(/\/$/, '');
const options = new chrome.Options().debuggerAddress(process.env.LP6_DEBUGGER_ADDRESS);
let builder = new Builder().forBrowser('chrome').setChromeOptions(options);
if (process.env.LP6_CHROMEDRIVER) builder = builder.setChromeService(new chrome.ServiceBuilder(process.env.LP6_CHROMEDRIVER));
(async () => {
 const d = await builder.build();
 const click = selector => d.executeScript('document.querySelector(arguments[0]).click()', selector);
 const state = () => d.executeAsyncScript('const done=arguments[arguments.length-1];fetch("/whatif/state").then(r=>r.json()).then(done)');
 const plan = () => d.executeScript('return JSON.parse(document.getElementById("lifetime-initial-data").textContent)');
 const open = async () => { await click('#lifetime-open'); await d.wait(()=>d.executeScript('return document.getElementById("lifetime-dialog").open'),10000); };
 const refresh = async () => { await d.navigate().refresh(); await d.wait(until.elementLocated(By.id('lifetime-open')),20000); };
 const preview = async () => { await click('#lifetime-preview'); await d.wait(async()=>{const text=await d.findElement(By.id('lifetime-preview-output')).getText();return text&&!text.includes('Calculating');},20000); return d.findElement(By.id('lifetime-preview-output')).getText(); };
 const field = (selector,name,value) => d.executeScript(`const e=document.querySelector(arguments[0]).querySelector('[data-field="'+arguments[1]+'"]');e.value=arguments[2];e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));`,selector,name,value);
 try {
  await d.manage().setTimeouts({script:60000});
  await d.sendDevToolsCommand('Network.setCacheDisabled',{cacheDisabled:true});
  await d.manage().window().setRect({width:1440,height:1000});
  await d.get(url+'/whatif'); await d.wait(until.elementLocated(By.id('lifetime-open')),20000);
  assert.equal(await plan(),null,'fixture must begin without Lifetime settings');
  assert.match(await d.findElement(By.id('lifetime-open')).getText(),/Set up/);
  const initialState=await state(); await open();
 await click('[data-add-account]');await click('[data-add-account]');await click('[data-add-account]');await click('[data-add-job]');await click('[data-add-rule]');await click('[data-add-ytd-job]');await click('[data-add-ytd-rule]');
 const createdCounts=await d.executeScript("return ['account','job','rule'].map(k=>document.querySelectorAll('[data-lifetime-'+k+']').length)");
 await d.executeScript(`
 const set=(r,n,v)=>{const e=r.querySelector('[data-field="'+n+'"]');if(!e)throw Error('missing '+n);if(e.type==='checkbox')e.checked=!!v;else e.value=v;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));};
 document.querySelector('[data-retirement-owner]').value='2035-06';const owner=document.querySelector('[data-retirement-owner]').dataset.retirementOwner, as=[...document.querySelectorAll('[data-lifetime-account]')];
 [['Cash reserve','cash','taxable',0,0,100],['Brokerage','brokerage','taxable',100,0,0],['Workplace traditional','401k','traditional',100,0,0]].forEach((x,i)=>{set(as[i],'name',x[0]);set(as[i],'owner_id',owner);set(as[i],'legal_type',x[1]);set(as[i],'tax_treatment',x[2]);set(as[i],'opening_value',i===0?25000:i===1?50000:100000);set(as[i],'stock_percent',x[3]);set(as[i],'bond_percent',x[4]);set(as[i],'cash_percent',x[5]);if(i===2){set(as[i],'plan_id','Acme 401k');set(as[i],'employer_id','Acme');set(as[i],'limit_group','Acme 401k');}});
 const j=document.querySelector('[data-lifetime-job]');set(j,'name','Acme engineering');set(j,'owner_id',owner);set(j,'employer_id','Acme');set(j,'gross_salary',120000);set(j,'eligible_compensation',120000);set(j,'growth_percent',2);set(j,'start_month','2026-01');set(j,'end_month','2035-06');set(j,'prior_sponsor_wages',12345);const inc=j.querySelector('[data-field=income_source_id]');if(inc.options.length>1){inc.selectedIndex=1;inc.dispatchEvent(new Event('change',{bubbles:true}));}
 window.__enteredEnd={value:j.querySelector('[data-field=end_month]').value,linked:j.querySelector('[data-field=end_at_retirement]').checked,income:inc.options[inc.selectedIndex]?.text,prior:j.querySelector('[data-field=prior_sponsor_wages]').value};set(j,'end_month','');set(j,'end_at_retirement',true);
 const r=document.querySelector('[data-lifetime-rule]');set(r,'job_id',j.querySelector('[data-field=id]').value);set(r,'start_month','2026-01');set(r,'end_month','2035-06');window.__ruleEnd=r.querySelector('[data-field=end_month]').value;set(r,'end_month','');set(r,'end_at_retirement',true);set(r,'rate.mode','fixed');set(r,'rate.fixed_monthly',500);set(r,'rate.percent_of_compensation',0);set(r,'traditional_account_id',as[2].querySelector('[data-field=id]').value);set(r,'employer.mode','none');
 document.getElementById('lifetime-reserve-account').value=as[0].querySelector('[data-field=id]').value;document.getElementById('lifetime-surplus-account').value=as[1].querySelector('[data-field=id]').value;document.getElementById('lifetime-reserve-target').value='12000';document.getElementById('withdrawal-add').value=as[0].querySelector('[data-field=id]').value;document.getElementById('withdrawal-add').dispatchEvent(new Event('change',{bubbles:true}));document.getElementById('withdrawal-add').value=as[1].querySelector('[data-field=id]').value;document.getElementById('withdrawal-add').dispatchEvent(new Event('change',{bubbles:true}));document.getElementById('withdrawal-add').value=as[2].querySelector('[data-field=id]').value;document.getElementById('withdrawal-add').dispatchEvent(new Event('change',{bubbles:true}));
 document.getElementById('lifetime-ytd-year').value='2026';document.getElementById('lifetime-ytd-zero').checked=false;const yj=document.querySelector('[data-ytd-job]');set(yj,'job_id',j.querySelector('[data-field=id]').value);set(yj,'gross_wages',80000);set(yj,'eligible_pay',80000);const yr=document.querySelector('[data-ytd-rule]');set(yr,'rule_id',r.querySelector('[data-field=id]').value);set(yr,'employee_regular',4000);set(yr,'employee_catch_up',0);set(yr,'employer',0);set(yr,'matching_paid',0);
 `);

  assert.deepEqual(createdCounts,[3,1,1]);
  const employeePreview=await preview();
  assert.match(employeePreview,/employee \$2000\.00/); assert.match(employeePreview,/employer \$0/);
  assert.deepEqual(await state(),initialState,'preview must not change persisted revision/state');
  await field('[data-ytd-job]','gross_wages','');
  await click('#lifetime-preview');
  assert.equal(await d.executeScript(`return document.querySelector('[data-ytd-job] [data-field=gross_wages]').checkValidity()`),false,'blank YTD must fail native validation');
  assert.ok(await d.findElement(By.css('[data-ytd-job] [data-field=gross_wages]')).getAttribute('validationMessage')); 
  assert.deepEqual(await state(),initialState,'invalid preview must not write');
  await field('[data-ytd-job]','gross_wages','80000');
  await click('#lifetime-form button[type=submit]');
  await d.wait(async()=>{try{return (await state()).revision!==initialState.revision;}catch{return false;}},20000);
  await refresh(); const saved=await plan();
  assert.equal(saved.accounts.length,3); assert.equal(saved.jobs[0].prior_sponsor_wages,12345);
  assert.ok(saved.contribution_rules[0].employer == null,'employee-only employer must remain nil');
  assert.equal(saved.ytd.jobs[0].gross_wages,80000); assert.equal(saved.ytd.rules[0].employee_regular,4000);
  await open();
  await field('[data-lifetime-rule]','employer.mode','match');
  await d.executeScript(`const r=document.querySelector('[data-lifetime-rule]');const s=r.querySelector('[data-field="employer.destination_account_id"]');s.value=document.querySelectorAll('[data-lifetime-account]')[2].querySelector('[data-field=id]').value;r.querySelector('[data-add-tier]').click();for(const [n,v] of [['tier.from',0],['tier.to',6],['tier.match',50]])r.querySelector('[data-field="'+n+'"]').value=v;`);
  await field('[data-lifetime-rule]','employer.mode','none');
  assert.equal(await d.executeScript('return document.querySelectorAll("[data-employer-fields]").length'),0);
  await field('[data-lifetime-rule]','employer.mode','match');
  assert.equal(await d.findElement(By.css('[data-field="tier.to"]')).getAttribute('value'),'6');
  assert.match(await preview(),/employer \$1000\.00/);
  const matchDestination=saved.accounts[2].id;
  const matchTiers=[{FromPercent:0,ToPercent:6,MatchPercent:50}];
  const beforeMatchSave=await state();
  await click('#lifetime-form button[type=submit]');
  await d.wait(async()=>{try{return (await state()).revision!==beforeMatchSave.revision;}catch{return false;}},20000);
  await refresh(); const matchedSaved=await plan();
  const persistedEmployer=matchedSaved.contribution_rules[0].employer;
  assert.equal(persistedEmployer.mode,'match','save must serialize employer matching mode');
  assert.equal(persistedEmployer.destination_account_id,matchDestination,'save must preserve employer destination identity');
  assert.deepEqual(persistedEmployer.tiers,matchTiers,'save must preserve exact canonical match bands');
  await open();
  assert.equal(await d.findElement(By.css('[data-field="employer.mode"]')).getAttribute('value'),'match');
  assert.equal(await d.findElement(By.css('[data-field="employer.destination_account_id"]')).getAttribute('value'),matchDestination);
  assert.deepEqual(await d.executeScript(`return [...document.querySelectorAll('[data-lifetime-rule] [data-tier]')].map(t=>({FromPercent:Number(t.querySelector('[data-field="tier.from"]').value),ToPercent:Number(t.querySelector('[data-field="tier.to"]').value),MatchPercent:Number(t.querySelector('[data-field="tier.match"]').value)}))`),matchTiers,'reloaded editor must hydrate exact match bands');
  const reloadedMatchState=await state();
  const reloadedMatchPreview=await preview();
  assert.match(reloadedMatchPreview,/employee \$2000\.00/);
  assert.match(reloadedMatchPreview,/employer \$1000\.00/);
  assert.deepEqual(await state(),reloadedMatchState,'reloaded matching preview must not write');
  await field('[data-lifetime-account]','name','Renamed cash');
  assert.match(await d.findElement(By.id('lifetime-reserve-account')).getAttribute('textContent'),/Renamed cash/);
  await field('[data-lifetime-job]','name','Renamed job');
  assert.match(await d.findElement(By.css('[data-lifetime-rule] [data-field=job_id]')).getAttribute('textContent'),/Renamed job/);
  assert.match(await d.findElement(By.css('[data-ytd-rule] [data-field=rule_id]')).getAttribute('textContent'),/Renamed job/);
  // Remove draft identities and assert all reference choices lose them.
  await click('[data-lifetime-rule] > [data-remove-row]');
  assert.doesNotMatch(await d.findElement(By.css('[data-ytd-rule] [data-field=rule_id]')).getAttribute('textContent'),/Renamed job/);
  await click('[data-lifetime-job] [data-remove-row]');
  assert.doesNotMatch(await d.findElement(By.css('[data-ytd-job] [data-field=job_id]')).getAttribute('textContent'),/Renamed job/);
  await click('[data-lifetime-account] [data-remove-row]');
  assert.doesNotMatch(await d.findElement(By.id('lifetime-reserve-account')).getAttribute('textContent'),/Renamed cash/);
  await click('#lifetime-form footer [data-lifetime-close]'); await refresh();
  assert.deepEqual(await plan(),matchedSaved,'cancel must preserve the saved plan');
  // Two real browser windows establish a stale editor without fabricating a token.
  const first=await d.getWindowHandle(); await open();
  await d.switchTo().newWindow('tab'); await d.get(url+'/whatif'); await open();
  await field('[data-lifetime-account]','name','Concurrent saved cash');
  const beforeConcurrent=await state(); await click('#lifetime-form button[type=submit]');
  await d.wait(async()=>{try{return (await state()).revision!==beforeConcurrent.revision;}catch{return false;}},20000);
  await refresh(); const concurrent=await plan(); await d.close(); await d.switchTo().window(first);
  await field('[data-lifetime-account]','name','Stale must not save');
  await click('#lifetime-form button[type=submit]');
  await d.wait(async()=>/changed|stale|reload|conflict/i.test(await d.findElement(By.id('lifetime-status')).getText()),10000);
  await refresh(); assert.deepEqual(await plan(),concurrent,'stale save must not overwrite concurrent update');
  await d.wait(until.elementLocated(By.id('lifetime-year-select')),60000);
  await click('[data-wf-tab=cashflow]');
  const year=await d.executeScript(`const s=document.getElementById('lifetime-year-select');s.selectedIndex=s.options.length-1;s.dispatchEvent(new Event('change',{bubbles:true}));s.focus();return s.value;`);
  await d.executeAsyncScript(`const done=arguments[arguments.length-1];document.body.addEventListener('htmx:afterSwap',()=>setTimeout(done,200),{once:true});htmx.ajax('GET','/whatif/poll?since=-1',{target:'#whatif-results',swap:'outerHTML'});`);
  assert.deepEqual(await d.executeScript('return [document.getElementById("lifetime-year-select").value,document.activeElement.id]'),[year,'lifetime-year-select'],'actual HTMX replacement preserves year and focus');
  const axe=fs.readFileSync(require.resolve(modulePath('axe-core/axe.min.js')),'utf8');
  for(const theme of ['light','dark']) {
   const dark=await d.executeScript('return document.documentElement.classList.contains("dark")');
   if(dark!==(theme==='dark')) await click('#theme-toggle');
   await d.wait(()=>d.executeScript('return document.documentElement.classList.contains("dark")===arguments[0]',theme==='dark'),5000);
   // 600 CSS pixels is a 1200px/200% reflow surrogate, not actual browser zoom.
   // Final independent review verifies real browser zoom separately.
   for(const [width,zoom] of [[390,1],[600,1]]) {
    await d.manage().window().setRect({width,height:900});
    await d.executeScript('document.documentElement.style.zoom=arguments[0]',String(zoom));
    await open();
    for(const summary of await d.findElements(By.css('#lifetime-dialog details:not([open]) > summary'))) await d.executeScript('arguments[0].click()',summary);
    await d.sleep(800); await d.executeScript(axe);
    assert.equal(await d.executeScript('return document.getElementById("lifetime-dialog").open'),true,'editor must remain open before axe');
    const violations=await d.executeAsyncScript('const done=arguments[arguments.length-1];axe.run(document.getElementById("lifetime-dialog"),{runOnly:{type:"tag",values:["wcag2a","wcag2aa","wcag21a","wcag21aa","wcag22aa"]}}).then(r=>done(r.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))))');
    console.log(JSON.stringify({theme,width,zoom,modal:await d.executeScript('return {open:document.getElementById("lifetime-dialog").open,focus:document.activeElement.id,dark:document.documentElement.classList.contains("dark") }'),violations}));
    assert.deepEqual(violations,[],theme+' populated editor axe at '+width+' / '+zoom);
    assert.ok(await d.executeScript('return document.documentElement.scrollWidth<=document.documentElement.clientWidth+1'),'page must reflow');
    await click('#lifetime-form footer [data-lifetime-close]');
   }
  }
  await d.executeScript('document.documentElement.style.zoom="1"');
  await d.manage().window().setRect({width:1200,height:900});
  for(const theme of ['light','dark']) {
   if(await d.executeScript('return document.documentElement.classList.contains("dark")') !== (theme==='dark')) await click('#theme-toggle');
   await d.sleep(800);
   await d.executeScript('document.getElementById("roth-conversion-card").scrollIntoView({block:"center"})');
   assert.equal(await d.executeScript('return document.getElementById("lifetime-dialog").open'),false);
   await d.executeScript(axe);
   const violations=await d.executeAsyncScript('const done=arguments[arguments.length-1];axe.run(document,{runOnly:{type:"tag",values:["wcag2a","wcag2aa","wcag21a","wcag21aa","wcag22aa"]}}).then(r=>done(r.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))))');
   assert.deepEqual(violations,[],theme+' closed-dialog populated page axe, Roth card visible');
  }
  await d.manage().window().setRect({width:390,height:844});
  for(const prefix of ['lifetime-accounts-','lifetime-rmd-']) {
   const regions=await d.findElements(By.css('[data-lifetime-year-panel="'+year+'"] [aria-labelledby^="'+prefix+'"]'));
   assert.ok(regions.length,'named '+prefix+' region exists');
   for(const region of regions) {
    assert.equal(await region.getAttribute('tabindex'),'0'); assert.equal(await region.getAttribute('role'),'region');
    await d.executeScript('arguments[0].focus();arguments[0].scrollLeft=0',region);
    for(let i=0;i<8;i++)await region.sendKeys(Key.ARROW_RIGHT);
    await d.wait(()=>d.executeScript('return arguments[0].scrollLeft>0||arguments[0].scrollWidth<=arguments[0].clientWidth',region),5000);
    assert.ok(await d.executeScript('return document.activeElement===arguments[0]',region),'keyboard scroll keeps region focus');
   }
  }
  console.log('LIFETIME WORKFLOW PASS: create, preview/no-write, YTD, save/reload, employer toggle, named references, cancel, stale conflict, HTMX year, themes/reflow/axe, keyboard regions');
 } finally { await d.executeScript('document.documentElement.style.zoom="1"').catch(()=>{}); await d.quit(); }
})().catch(error=>{console.error(error);process.exitCode=1;});
