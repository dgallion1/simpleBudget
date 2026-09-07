// Lead-authored acceptance oracle. Real rendered pages and HTMX, synthetic data.
// Every network write is blocked. Failures accumulate so calibration exposes the
// complete matrix, rather than hiding later cases behind the first failure.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const root=process.env.DI5_SOURCE_ROOT,base=process.env.DI5_BASE,out=process.env.DI5_OUTPUT,u=new URL(base);
assert.ok(root.startsWith('/tmp/'));assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));
fs.mkdirSync(out,{recursive:true});
const nav=fs.readFileSync(path.join(root,'web/templates/layouts/base.html'),'utf8');
const desktop=nav.slice(nav.indexOf('<nav'),nav.indexOf('<!-- Theme Toggle -->'));
const routes=[...new Set([...desktop.matchAll(/href="(?:\{\{withRange ")?(\/[a-z][a-z-]*)/g)].map(m=>m[1]))].filter(r=>!r.startsWith('/static'));
assert.equal(routes.length,9,'enumerate every shared-script navigation consumer');
const source=fs.readFileSync(path.join(root,'web/static/js/page-refresh.js'),'utf8');
const results=[],failures=[];
(async()=>{
 const live=await fetch(base+'/static/js/page-refresh.js');assert.equal(await live.text(),source,'server must serve exactly the declared frozen JS');
 const browser=await chromium.launch({headless:true,executablePath:'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 try {for(const dark of [false,true])for(const width of [390,768,1440])for(const route of routes){
  const ctx=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await ctx.newPage();let revision=0,epoch='one',fragment='',loads=0,calls=0;
  const key={route,dark,width};
  const check=(ok,name,extra={})=>{const row={...key,name,ok,...extra};results.push(row);if(!ok)failures.push(row);};
  await p.route('**/*',r=>{const q=new URL(r.request().url());if(q.origin!==base||r.request().method()!=='GET')return r.abort();
   if(q.pathname==='/api/ui-refresh'){calls++;return r.fulfill({json:{epoch,revision}});}
   if(q.pathname==='/oracle-main')return r.fulfill({contentType:'text/html',body:fragment});
   if(q.pathname==='/oracle-partial')return r.fulfill({contentType:'text/html',body:'Updated partial'});
   if(r.request().isNavigationRequest())loads++;
   return r.continue();});
  const response=await p.goto(base+route+'?start=2026-08-01&end=2026-08-28');assert.equal(response.status(),200);
  await p.evaluate(d=>document.documentElement.classList.toggle('dark',d),dark);
  await p.waitForFunction(()=>typeof htmx!=='undefined');await p.waitForTimeout(250);assert.ok(calls>0);
  // A synthetic editing control on each real page isolates refresh ownership
  // from unrelated page-specific autosave handlers. Actual Dashboard input is
  // additionally exercised by the strict original F1 and lifecycle probes.
  await p.evaluate(()=>{const wrapper=document.createElement('div');wrapper.id='oracle-edit-wrap';wrapper.innerHTML='<label for="oracle-edit">Oracle unsaved note</label><input id="oracle-edit" value="saved"><div id="oracle-partial">Original partial</div>';document.querySelector('main').prepend(wrapper);});
  const input=p.locator('#oracle-edit');await input.fill('unsaved');await input.focus();
  const signal=async()=>{await p.evaluate(()=>dispatchEvent(new Event('focus')));await p.waitForTimeout(120);};
  revision++;await signal();const notice=p.getByRole('region',{name:'Page refresh'});await notice.waitFor();
  check(await input.evaluate(e=>e===document.activeElement),'arrival retains editing focus');
  check(await input.inputValue()==='unsaved','arrival preserves edits');
  check(await notice.evaluate(e=>getComputedStyle(e).position==='static'),'normal flow, no overlay');
  await p.evaluate(()=>{window.oracleRegion=document.querySelector('section[aria-label="Page refresh"]');window.oracleStatus=window.oracleRegion.querySelector('[role=status]');});
  // All nine consumers: both actions survive an unrelated actual HTMX swap.
  for(const action of ['Refresh page','Keep editing']){
   const button=notice.getByRole('button',{name:action,exact:true});await button.focus();
   await p.evaluate(()=>window.oracleButton=document.activeElement);
   await p.evaluate(()=>htmx.ajax('GET','/oracle-partial',{target:'#oracle-partial',swap:'innerHTML'}));
   check(await button.evaluate(e=>e===document.activeElement),'partial preserves '+action+' focus');
  }
  await input.focus();await p.evaluate(()=>htmx.ajax('GET','/oracle-partial',{target:'#oracle-partial',swap:'innerHTML'}));
  check(await input.evaluate(e=>e===document.activeElement),'partial does not steal editing focus');
  if(route==='/dashboard'){
   fragment=await p.locator('main').evaluate(e=>{const n=e.cloneNode(true);n.querySelector('#oracle-edit').setAttribute('value','unsaved');n.querySelector('section[aria-label="Page refresh"]').remove();n.querySelectorAll('script').forEach(s=>s.remove());return n.outerHTML;});
   await notice.getByRole('button',{name:'Keep editing'}).focus();
   await p.evaluate(()=>{window.oracleButton=document.activeElement;document.addEventListener('htmx:beforeSwap',e=>e.preventDefault(),{once:true});});
   await p.evaluate(()=>htmx.ajax('GET','/oracle-main',{target:'main',swap:'outerHTML'}));
   check(await p.evaluate(()=>document.activeElement===window.oracleButton&&window.oracleRegion.isConnected),'cancelled swap preserves focus and notice');
   await p.evaluate(()=>{const e=document.createElement('input');e.id='oracle-outside';e.setAttribute('aria-label','Outside main edit');document.body.insertBefore(e,document.querySelector('main'));window.oracleBeforeSwap=false;document.addEventListener('htmx:beforeSwap',()=>window.oracleBeforeSwap=true,{once:true});htmx.ajax('GET','/oracle-main',{target:'main',swap:'outerHTML swap:400ms'});});
   await p.waitForFunction(()=>window.oracleBeforeSwap);await p.locator('#oracle-outside').focus();await p.waitForTimeout(550);
   check(await p.locator('#oracle-outside').evaluate(e=>e===document.activeElement),'delayed swap does not steal newly moved focus');
  }
  // Dashboard exercises each main replacement, each notice action, and an
  // unrelated externally focused control, in every viewport/theme.
  if(route==='/dashboard')for(const swap of ['innerHTML','outerHTML'])for(const action of ['Refresh page','Keep editing','outside']){
   await p.evaluate(()=>{if(!document.querySelector('#oracle-outside')){const e=document.createElement('input');e.id='oracle-outside';e.setAttribute('aria-label','Outside main edit');document.body.insertBefore(e,document.querySelector('main'));}});
   const focus=action==='outside'?p.locator('#oracle-outside'):notice.getByRole('button',{name:action,exact:true});
   await focus.focus();await p.evaluate(()=>window.oracleButton=document.activeElement);
   fragment=await p.locator('main').evaluate((e,swap)=>{const clone=e.cloneNode(true);clone.querySelector('#oracle-edit').setAttribute('value','unsaved');clone.querySelector('section[aria-label="Page refresh"]')?.remove();clone.querySelectorAll('script').forEach(e=>e.remove());return swap==='innerHTML'?clone.innerHTML:clone.outerHTML;},swap);
   await p.evaluate(swap=>htmx.ajax('GET','/oracle-main',{target:'main',swap}),swap);await p.waitForTimeout(50);
   const state=await p.evaluate(()=>{const e=document.activeElement,r=e.getBoundingClientRect();return {same:e===window.oracleButton,active:e.tagName,text:e.textContent.slice(0,80),connected:window.oracleButton.isConnected,visible:r.top>=0&&r.bottom<=innerHeight&&r.left>=0&&r.right<=innerWidth,region:window.oracleRegion===document.querySelector('section[aria-label="Page refresh"]'),status:window.oracleStatus===window.oracleRegion.querySelector('[role=status]'),count:document.querySelectorAll('section[aria-label="Page refresh"]').length};});
   check(state.same&&state.visible&&state.region&&state.status&&state.count===1,swap+' preserves '+action+' focus and identity',state);
  }
  // A focused action must not fall to BODY when its notice is removed.
  // If original editing input was replaced, restore a meaningful main target;
  // do not focus a stale detached node. No focus theft when focus is outside.
  for(const remove of ['dismiss','epoch'])for(const action of ['Refresh page','Keep editing','outside']){
   if(await notice.count()===0){await input.fill('unsaved');await input.focus();revision++;await signal();await notice.waitFor();}
   const focus=action==='outside'?input:notice.getByRole('button',{name:action,exact:true});await focus.focus();
   await p.evaluate(()=>window.oracleRemovalFocus=document.activeElement);
   if(remove==='dismiss')await notice.getByRole('button',{name:'Keep editing'}).evaluate(e=>e.click());
   else{epoch+='x';revision=0;await signal();}
   const state=await p.evaluate(()=>({active:document.activeElement.tagName,same:document.activeElement===window.oracleRemovalFocus,inside:!!document.activeElement.closest('main'),count:document.querySelectorAll('section[aria-label="Page refresh"]').length,status:!!window.oracleStatus?.isConnected,value:document.querySelector('#oracle-edit').value}));
   check(state.count===0&&state.value==='unsaved'&&(action==='outside'?state.same:state.active!=='BODY'&&state.inside),remove+' safely removes while '+action+' focused',state);
  }
  check(loads===1,'no reload or edit loss in protected transitions',{loads});
  fs.writeFileSync(path.join(out,'focus-transitions.json'),JSON.stringify({routes,results,failures},null,2));
  console.log(JSON.stringify({...key,checks:results.filter(x=>x.route===route&&x.dark===dark&&x.width===width).length,failures:failures.filter(x=>x.route===route&&x.dark===dark&&x.width===width).length}));await ctx.close();
 }}finally{await browser.close();}
 assert.deepEqual(failures,[],'focus-transition contract violations');
})().catch(e=>{console.error(e);process.exitCode=1;});
