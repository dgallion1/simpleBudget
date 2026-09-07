const fs=require('fs'),assert=require('assert/strict'),{chromium}=require('/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base='http://127.0.0.1:18873',results=[];
(async()=>{const b=await chromium.launch({headless:true,executablePath:'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});try{for(const dark of [false,true])for(const width of [390,768,1440]){
const c=await b.newContext({viewport:{width,height:900},reducedMotion:'reduce'}),p=await c.newPage();let revision=0,epoch='one',main='';
await p.route('**/*',r=>{const u=new URL(r.request().url());if(u.origin!==base||r.request().method()!=='GET')return r.abort();if(u.pathname==='/api/ui-refresh')return r.fulfill({json:{epoch,revision}});if(u.pathname==='/checker-main')return r.fulfill({contentType:'text/html',body:main});return r.continue()});
await p.goto(base+'/dashboard?start=2026-08-01&end=2026-08-28');await p.evaluate(d=>{document.documentElement.classList.toggle('dark',d);dispatchEvent(new Event('themechange'))},dark);await p.waitForTimeout(300);
const input=p.locator('#dashboard-date-start'),notice=p.getByRole('region',{name:'Page refresh'});
for(const removal of ['dismiss','epoch']){
await input.fill('2026-07-01');await input.focus();revision++;await p.evaluate(()=>dispatchEvent(new Event('focus')));await notice.waitFor();
main=await p.locator('main').evaluate(e=>{const n=e.cloneNode(true);n.querySelector('section[aria-label="Page refresh"]').remove();n.querySelector('#dashboard-date-start').setAttribute('value','2026-07-01');n.querySelectorAll('script').forEach(s=>s.remove());return n.outerHTML});
const action=notice.getByRole('button',{name:removal==='dismiss'?'Keep editing':'Refresh page',exact:true});await action.focus();await p.evaluate(()=>{window.oldAction=document.activeElement;window.oldNotice=document.querySelector('section[aria-label="Page refresh"]');window.oldStatus=window.oldNotice.querySelector('[role=status]')});
await p.evaluate(()=>htmx.ajax('GET','/checker-main',{target:'main',swap:'outerHTML'}));await p.waitForTimeout(150);
assert(await action.evaluate(e=>e===document.activeElement&&e===window.oldAction));assert(await notice.evaluate(e=>e===window.oldNotice&&window.oldStatus.isConnected));
if(removal==='dismiss')await p.keyboard.press('Enter');else{epoch+='x';revision=0;await p.evaluate(()=>dispatchEvent(new Event('focus')));await notice.waitFor({state:'detached'})}
await p.waitForTimeout(100);
const state=await p.locator('main').evaluate(e=>({main:e===document.activeElement,tabindex:e.getAttribute('tabindex'),outline:getComputedStyle(e).outline,rect:e.getBoundingClientRect().toJSON(),status:window.oldStatus.isConnected,notice:window.oldNotice.isConnected,value:document.querySelector('#dashboard-date-start').value}));
assert(state.main);assert.equal(state.tabindex,'-1');assert(!state.status&&!state.notice);assert.equal(state.value,'2026-07-01');
await p.screenshot({path:`checker-evidence/fallback-${dark}-${width}-${removal}.png`});
await p.keyboard.press('Tab');const after=await p.evaluate(()=>({tag:document.activeElement.tagName,id:document.activeElement.id,tabindex:document.querySelector('main').getAttribute('tabindex'),inside:!!document.activeElement.closest('main')}));assert.equal(after.tabindex,null);assert.notEqual(after.tag,'BODY');assert(after.inside);
results.push({dark,width,removal,state,after});
}
await c.close();}}finally{await b.close();fs.writeFileSync('checker-evidence/independent-fallback.json',JSON.stringify(results,null,2))}assert.equal(results.length,12);console.log('12/12 actual Dashboard remount/exact-button, dismissal/reset, retained MAIN focus, keyboard Tab cleanup states pass');})().catch(e=>{console.error(e);process.exitCode=1});
