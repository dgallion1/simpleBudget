const fs = require('node:fs');
const assert = require('node:assert/strict');
const {chromium} = require('/home/darrell/.npm/_npx/e41f203b7505f1fb/node_modules/playwright');
const base='http://127.0.0.1:18775';
const output=process.env.DI4_OUTPUT || '/tmp/budget2-DI4-visual.0j6FTL';
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:'/home/darrell/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome',args:['--no-sandbox']});
 const results=[];
 try {
  for(const dark of [false,true]) for(const width of [390,768,1440]) {
   const context=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'});
   const page=await context.newPage();const errors=[];
   page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/*',r=>new URL(r.request().url()).origin===base?r.continue():r.abort());
   await page.goto(base+'/insights?start=2026-08-01&end=2026-08-28');
   await page.evaluate(d=>{document.documentElement.classList.toggle('dark',d);window.dispatchEvent(new Event('themechange'));},dark);
   await page.waitForFunction(()=>document.querySelector('#chart-trends')?.data?.length);
   await page.waitForTimeout(250);
   const dimensions=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth}));
   const headings=await page.locator('main h1,main h2,main h3').allTextContents();
   const preview=await page.locator('#findings-preview > li').count();
   const remaining=await page.locator('#all-findings li').count();
   let disclosed=remaining===0;
   if(remaining){await page.locator('#all-findings summary').focus();await page.keyboard.press('Enter');disclosed=await page.locator('#all-findings li').first().isVisible();}
   await page.addScriptTag({path:'/home/darrell/.config/nvm/versions/node/v24.12.0/lib/node_modules/@axe-core/cli/node_modules/axe-core/axe.min.js'});
   const audits=await page.evaluate(async()=> {
    const a=await axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}});
    const b=await axe.run(document,{runOnly:{type:'rule',values:['label-content-name-mismatch']}});
    return {violations:a.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>({target:n.target,summary:n.failureSummary}))})),label:b.violations};
   });
   const chart=await page.evaluate(()=> {
    const c=document.querySelector('#chart-trends');
    const rows=[...document.querySelectorAll('#category-trends-table tbody tr')];
    return {series:c.data.map(t=>({name:t.name,x:t.x,y:t.y,color:t.marker.color})),rows:rows.map(r=>({name:r.dataset.category,current:Number(r.dataset.current),previous:Number(r.dataset.previous)})),background:getComputedStyle(c.parentElement).backgroundColor};
   });
   const luminance=color=>{
    let rgb=color.startsWith('#')?color.slice(1).match(/../g).map(x=>parseInt(x,16)):color.match(/[\d.]+/g).slice(0,3).map(Number);
    return rgb.map(v=>{v/=255;return v<=0.04045?v/12.92:Math.pow((v+0.055)/1.055,2.4)}).reduce((a,v,i)=>a+v*[0.2126,0.7152,0.0722][i],0);
   };
   const background=dark?'#1f2937':'#ffffff';
   chart.contrast=chart.series.map(s=>{const a=luminance(s.color),b=luminance(background);return(Math.max(a,b)+0.05)/(Math.min(a,b)+0.05)});
   for(let i=0;i<chart.rows.length;i++){assert.equal(chart.series[0].x[i],chart.rows[i].current);assert.equal(chart.series[1].x[i],chart.rows[i].previous);}
   await page.screenshot({path:output+'/DI4-'+(dark?'dark':'light')+'-'+width+'.png',fullPage:true});
   const input=page.locator('#insights-date-start');await input.focus();await input.fill('2026-07-01');
   const request=page.waitForResponse(r=>r.url().includes('/insights?')&&r.request().headers()['hx-request']==='true');
   await input.dispatchEvent('change');await request;
   await page.waitForFunction(()=>document.querySelector('#period-context')?.innerText.includes('2026-07-01'));
   const focus=await page.locator('#insights-date-start').evaluate(e=>document.activeElement===e);
   const wrappers=await page.locator('#insights-wrapper').count();
   const refreshedLinks=await page.locator('#insights-findings a').evaluateAll(es=>es.every(e=>new URL(e.href).searchParams.get('start')==='2026-07-01'));
   let presetWorks=false;
   const req2=page.waitForResponse(r=>r.url().includes('/insights?')&&r.url().includes('preset=all'),{timeout:2500}).catch(()=>null);
   await page.locator('.insight-preset-btn[data-preset="all"]').click();
   presetWorks=!!(await req2);
   const presetFocus=await page.locator('.insight-preset-btn[data-preset="all"]').evaluate(e=>document.activeElement===e);
   const result={dark,width,dimensions,headings,preview,remaining,disclosed,audits,chart,focus,wrappers,refreshedLinks,presetWorks,presetFocus,errors};
   results.push(result);console.log(JSON.stringify(result));
   await context.close();
  }
 }finally{await browser.close();}
 fs.writeFileSync(output+'/DI4-browser.json',JSON.stringify(results,null,2)+'\n');
 for(const r of results){
  assert.equal(r.dimensions.scroll,r.width,'page overflow');
  assert.ok(r.preview<=5 && r.disclosed);assert.equal(r.headings.filter(x=>x==='Insights').length,1);
  assert.deepEqual(r.audits.violations,[]);assert.deepEqual(r.audits.label,[]);
  assert.equal(r.focus,true,'HTMX focus');assert.equal(r.wrappers,1);assert.equal(r.refreshedLinks,true);assert.equal(r.presetWorks,true,'preset after HTMX');assert.equal(r.presetFocus,true,'preset focus restoration');assert.deepEqual(r.errors,[]);
  assert.ok(r.chart.contrast.every(v=>v>=3),'chart trace contrast');
 }
})().catch(e=>{console.error(e);process.exitCode=1;});
