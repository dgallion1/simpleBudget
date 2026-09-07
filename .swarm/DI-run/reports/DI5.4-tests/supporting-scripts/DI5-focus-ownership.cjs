// DI5.3 durable focus-ownership regression, independent of the immutable oracle.
// Actual Dashboard render and bundled HTMX. Only synthetic GET fragments and
// refresh revision responses are intercepted; all network writes are blocked.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.DI5_PLAYWRIGHT||'/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base=process.env.DI5_BASE,out=process.env.DI5_OUTPUT,u=new URL(base);
assert.equal(u.hostname,'127.0.0.1');assert.ok(u.port&&!['8080','8081'].includes(u.port));assert.ok(out);fs.mkdirSync(out,{recursive:true});
const records=[];
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.DI5_CHROME||'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 try{for(const dark of [false,true])for(const width of [390,768,1440]){
  const context=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await context.newPage();
  let revision=0,epoch='start',fragment='',loads=0;
  const check=(ok,name,state)=>records.push({dark,width,name,ok,...state});
  await p.route('**/*',r=>{
   const url=new URL(r.request().url());if(url.origin!==base||r.request().method()!=='GET')return r.abort();
   if(url.pathname==='/api/ui-refresh')return r.fulfill({json:{epoch,revision}});
   if(url.pathname==='/di53-main')return r.fulfill({contentType:'text/html',body:fragment});
   if(url.pathname==='/di53-partial')return r.fulfill({contentType:'text/html',body:'Updated'});
   if(r.request().isNavigationRequest())loads++;return r.continue();
  });
  await p.goto(base+'/dashboard?start=2026-08-01&end=2026-08-28');
  await p.waitForTimeout(350);
  await p.evaluate(d=>{
   document.documentElement.classList.toggle('dark',d);
   const external=document.createElement('input');external.id='di53-outside';external.setAttribute('aria-label','Outside editing control');document.querySelector('main').before(external);
   const fixture=document.createElement('div');fixture.innerHTML='<label for="di53-edit">Unsaved test note</label><input id="di53-edit" value="saved"><div id="di53-partial">Partial</div>';document.querySelector('main').prepend(fixture);
   window.phases=[];for(const name of ['htmx:afterSwap','htmx:afterSettle'])document.addEventListener(name,()=>{
    const e=document.activeElement;window.phases.push({name,same:e===window.expectedFocus,tag:e.tagName,text:e.textContent.slice(0,60)});
   });
  },dark);
  const input=p.locator('#di53-edit'),outside=p.locator('#di53-outside'),notice=p.getByRole('region',{name:'Page refresh'});
  const signal=async()=>{await p.evaluate(()=>dispatchEvent(new Event('focus')));await p.waitForTimeout(140);};
  const request=async()=>{await input.fill('unsaved');await input.focus();revision++;await signal();await notice.waitFor();};
  const capture=async()=>p.evaluate(()=>{window.expectedFocus=document.activeElement;window.phases=[];window.keptRegion=document.querySelector('section[aria-label="Page refresh"]');window.keptStatus=window.keptRegion.querySelector('[role=status]');});
  const prepare=async(swap)=>{
   fragment=await p.locator('main').evaluate((main,s)=>{
    const clone=main.cloneNode(true);clone.querySelectorAll('script,section[aria-label="Page refresh"]').forEach(e=>e.remove());
    clone.querySelector('#di53-edit').setAttribute('value','unsaved');
    return s==='innerHTML'?clone.innerHTML:clone.outerHTML;
   },swap);
  };
  const state=async()=>p.evaluate(()=>{
   const e=document.activeElement,n=document.querySelector('section[aria-label="Page refresh"]'),r=e.getBoundingClientRect();
   return {same:e===window.expectedFocus,tag:e.tagName,id:e.id,rect:r.toJSON(),visible:r.top>=0&&r.bottom<=innerHeight&&r.left>=0&&r.right<=innerWidth,region:n===window.keptRegion,status:n?.querySelector('[role=status]')===window.keptStatus,count:document.querySelectorAll('section[aria-label="Page refresh"]').length,phases:window.phases};
  });
  await request();
  for(const action of ['Refresh page','Keep editing']){
   await notice.getByRole('button',{name:action,exact:true}).focus();await capture();
   const scroll=await p.evaluate(()=>scrollY);
   await p.evaluate(()=>htmx.ajax('GET','/di53-partial',{target:'#di53-partial',swap:'innerHTML'}));await p.waitForTimeout(80);
   let s=await state();check(s.same&&s.region&&s.status&&await p.evaluate(()=>scrollY)===scroll,'partial non-theft '+action,s);
  }
  await notice.getByRole('button',{name:'Keep editing'}).focus();await capture();await prepare('outerHTML');
  await p.evaluate(()=>document.addEventListener('htmx:beforeSwap',e=>e.preventDefault(),{once:true}));
  await p.evaluate(()=>htmx.ajax('GET','/di53-main',{target:'main',swap:'outerHTML'}));await p.waitForTimeout(80);
  let s=await state();check(s.same&&s.region&&s.phases.length===0,'cancelled swap is inert',s);
  await p.evaluate(()=>{window.responseReady=false;document.addEventListener('htmx:beforeSwap',()=>window.responseReady=true,{once:true});htmx.ajax('GET','/di53-main',{target:'main',swap:'outerHTML swap:350ms'});});
  await p.waitForFunction(()=>window.responseReady);await outside.focus();await capture();await p.waitForTimeout(500);
  s=await state();check(s.same&&s.region,'delayed swap respects moved outside focus',s);
  for(const swap of ['innerHTML','outerHTML'])for(const action of ['Refresh page','Keep editing','outside']){
   await (action==='outside'?outside:notice.getByRole('button',{name:action,exact:true})).focus();await capture();await prepare(swap);
   await p.evaluate(s=>htmx.ajax('GET','/di53-main',{target:'main',swap:s}),swap);await p.waitForTimeout(100);
   s=await state();check(s.same&&s.visible&&s.region&&s.status&&s.count===1&&s.phases.some(x=>x.name==='htmx:afterSwap')&&s.phases.some(x=>x.name==='htmx:afterSettle')&&s.phases.every(x=>x.same),swap+' exact focus through settlement '+action,s);
  }
  await notice.getByRole('button',{name:'Keep editing'}).focus();await prepare('outerHTML');
  await p.evaluate(()=>document.addEventListener('htmx:afterSwap',()=>document.querySelector('#di53-outside').focus(),{once:true}));
  await p.evaluate(()=>htmx.ajax('GET','/di53-main',{target:'main',swap:'outerHTML'}));await p.waitForTimeout(100);
  check(await outside.evaluate(e=>e===document.activeElement),'another afterSwap handler retains chosen focus');
  for(const replaced of [false,true])for(const removal of ['dismiss','epoch'])for(const action of ['Refresh page','Keep editing','outside']){
   // Clear old notice with outside focus, then establish a fresh original input.
   await outside.focus();epoch+='x';revision=0;await signal();await request();
   await p.evaluate(()=>window.originalEdit=document.querySelector('#di53-edit'));
   if(replaced){await prepare('innerHTML');await p.evaluate(()=>htmx.ajax('GET','/di53-main',{target:'main',swap:'innerHTML'}));await p.waitForTimeout(80);}
   await (action==='outside'?outside:notice.getByRole('button',{name:action,exact:true})).focus();await capture();
   if(removal==='dismiss')await notice.getByRole('button',{name:'Keep editing'}).evaluate(e=>e.click());
   else{epoch+='x';revision=0;await signal();}
   const r=await p.evaluate(()=>({tag:document.activeElement.tagName,id:document.activeElement.id,original:document.activeElement===window.originalEdit,main:document.activeElement===document.querySelector('main')||document.activeElement===document.querySelector('main h1'),tabindex:document.activeElement.getAttribute('tabindex'),same:document.activeElement===window.expectedFocus,region:!!document.querySelector('section[aria-label="Page refresh"]'),status:window.keptStatus.isConnected,value:document.querySelector('#di53-edit').value}));
   check(!r.region&&!r.status&&r.value==='unsaved'&&(action==='outside'?r.same:replaced?r.main&&r.tabindex==='-1':r.original),removal+' owned focus '+action+' replaced='+replaced,r);
   if(replaced&&action!=='outside'){
    await outside.focus();
    check(await p.locator('main').evaluate(e=>e.getAttribute('tabindex')===null),'fallback tabindex cleaned after blur '+removal+'/'+action);
   }
   await p.evaluate(()=>{document.dispatchEvent(new CustomEvent('htmx:afterSwap'));document.dispatchEvent(new CustomEvent('htmx:afterSettle'));});await signal();
   check(await notice.count()===0,'no stale resurrection '+removal+'/'+action+'/'+replaced);
  }
  // A removal before an actual delayed swap must not be undone by that swap.
  for(const removal of ['dismiss','epoch']){
   await request();await notice.getByRole('button',{name:'Keep editing'}).focus();await prepare('outerHTML');
   await p.evaluate(()=>{window.responseReady=false;document.addEventListener('htmx:beforeSwap',()=>window.responseReady=true,{once:true});htmx.ajax('GET','/di53-main',{target:'main',swap:'outerHTML swap:350ms'});});
   await p.waitForFunction(()=>window.responseReady);
   if(removal==='dismiss')await notice.getByRole('button',{name:'Keep editing'}).evaluate(e=>e.click());else{epoch+='x';revision=0;await signal();}
   await p.waitForTimeout(500);check(await notice.count()===0,'delayed swap cannot resurrect '+removal);
  }
  check(loads===1,'no reload during protected transitions',{loads});
  fs.writeFileSync(path.join(out,'focus-ownership.json'),JSON.stringify(records,null,2));console.log(JSON.stringify({dark,width,checks:records.filter(r=>r.dark===dark&&r.width===width).length,failures:records.filter(r=>r.dark===dark&&r.width===width&&!r.ok).length}));await context.close();
 }}finally{await browser.close();}
 assert.deepEqual(records.filter(r=>!r.ok),[],'DI5.3 focus ownership failures');
})().catch(e=>{console.error(e);process.exitCode=1;});
