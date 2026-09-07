// DI5.4 promoted minimal browser probe and real page-refresh fallback regression.
// No application server or financial data: synthetic DOM, intercepted revision GETs.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const root=process.env.DI5_SOURCE_ROOT,out=process.env.DI5_OUTPUT;
assert.ok(root.startsWith('/tmp/'));assert.ok(out);fs.mkdirSync(out,{recursive:true});
const results=[];const check=(ok,name,extra={})=>results.push({name,ok,...extra});
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 try{
  const probe=await browser.newPage();await probe.setContent('<main>Fallback</main><button>Next</button>');
  const observation=await probe.evaluate(()=>{
   const m=document.querySelector('main');const samples=[];
   for(const remove of [true,false]){
    document.querySelector('button').focus();m.setAttribute('tabindex','-1');m.focus();
    samples.push({remove,phase:'before',active:document.activeElement.tagName,tabindex:m.getAttribute('tabindex')});
    if(remove)m.removeAttribute('tabindex');
    samples.push({remove,phase:'after',active:document.activeElement.tagName,tabindex:m.getAttribute('tabindex')});
   }return samples;
  });
  check(observation[0].active==='MAIN'&&observation[1].active==='BODY'&&observation[2].active==='MAIN'&&observation[3].active==='MAIN','minimal probe isolates immediate attribute removal',{observation});await probe.close();
  for(const initial of [null,'-1','0','3'])for(const removal of ['dismiss','epoch']){
   const p=await browser.newPage();let epoch='one',revision=0,calls=0;
   await p.route('**/*',r=>{
    const u=new URL(r.request().url());
    if(u.origin!=='http://di54.test'||r.request().method()!=='GET')return r.abort();
    if(u.pathname==='/api/ui-refresh'){calls++;return r.fulfill({json:{epoch,revision}});}
    return r.fulfill({contentType:'text/html',body:'<main><h1>Focus fixture</h1></main><button id="next">Next</button>'});
   });
   await p.goto('http://di54.test/');
   await p.addScriptTag({content:fs.readFileSync(path.join(root,'web/static/js/page-refresh.js'),'utf8')});
   await p.waitForTimeout(30);assert.ok(calls>0);
   await p.evaluate(v=>{
    const main=document.querySelector('main');if(v!==null)main.setAttribute('tabindex',v);
    window.focusAttributes=[];main.addEventListener('focus',()=>window.focusAttributes.push(main.getAttribute('tabindex')));
   },initial);
   const signal=async()=>{await p.evaluate(()=>dispatchEvent(new Event('focus')));await p.waitForTimeout(40);};
   const fallback=async()=>{
    await p.evaluate(()=>{
     const e=document.createElement('input');e.id='edit';e.setAttribute('aria-label','Unsaved note');document.querySelector('main').append(e);e.value='unsaved';e.focus();e.dispatchEvent(new Event('input',{bubbles:true}));
    });revision++;await signal();
    await p.evaluate(()=>{
     const e=document.querySelector('#edit');const replacement=e.cloneNode(true);replacement.value=e.value;e.replaceWith(replacement);
     document.querySelector('section button').focus();
    });
    if(removal==='dismiss')await p.evaluate(()=>document.querySelectorAll('section button')[1].click());
    else{epoch+='x';revision=0;await signal();}
   };
   const state=()=>p.evaluate(()=>({main:document.activeElement===document.querySelector('main'),tabindex:document.querySelector('main').getAttribute('tabindex'),notice:!!document.querySelector('section'),status:!!document.querySelector('[role=status]'),value:document.querySelector('#edit').value}));
   for(let cycle=0;cycle<2;cycle++){
    await fallback();let s=await state();check(s.main&&s.tabindex===(initial??'-1')&&!s.notice&&!s.status&&s.value==='unsaved','focused fallback '+JSON.stringify({initial,removal,cycle}),s);
    await p.keyboard.press('Tab');s=await state();check(!s.main&&s.tabindex===initial,'Tab leaves and cleans only temporary attribute '+JSON.stringify({initial,removal,cycle}),s);
    await p.evaluate(()=>document.querySelector('#edit').remove());
   }
   check(await p.evaluate(v=>window.focusAttributes.every(a=>a===(v??'-1')),initial),'existing tabindex untouched during focus '+JSON.stringify({initial,removal}));
   for(const external of ['0','-1',null]){
    await fallback();
    await p.evaluate(v=>{
     const main=document.querySelector('main');if(v===null)main.removeAttribute('tabindex');else main.setAttribute('tabindex',v);
     // Same-task blur also covers mutations whose observer has not run yet.
     document.querySelector('#next').focus();
    },external);
    const s=await state();check(s.tabindex===external,'preserve external attribute ownership '+JSON.stringify({initial,removal,external}),s);
    await p.evaluate(v=>{document.querySelector('#edit').remove();const m=document.querySelector('main');if(v===null)m.removeAttribute('tabindex');else m.setAttribute('tabindex',v);},initial);
   }
   await p.close();
  }
 }finally{await browser.close();}
 fs.writeFileSync(path.join(out,'fallback-lifecycle.json'),JSON.stringify(results,null,2)+'\n');
 console.log(JSON.stringify({assertions:results.length,failures:results.filter(r=>!r.ok)}));
 assert.deepEqual(results.filter(r=>!r.ok),[],'fallback lifecycle failures');
})().catch(e=>{console.error(e);process.exitCode=1;});
