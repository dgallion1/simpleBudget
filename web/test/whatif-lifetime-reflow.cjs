// Supply web/test/fixtures/lifetime-reflow.json to an isolated synthetic server.
// Verify its DATA/BACKUP/IMPORT directories under /tmp before running.
// Numeric strings below were captured BEFORE the containment patch; they are
// literal rendering observations, not recomputed financial expectations.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const resolve=name=>process.env.LP5F_NODE_MODULE_ROOT?path.join(process.env.LP5F_NODE_MODULE_ROOT,name):name;
const {Builder,By,Key,until}=require(resolve('selenium-webdriver'));
const chrome=require(resolve('selenium-webdriver/chrome'));
assert.ok(process.env.LP5F_URL,'LP5F_URL must identify the isolated fixture server');
assert.ok(process.env.LP5F_DEBUGGER_ADDRESS,'LP5F_DEBUGGER_ADDRESS required');
let builder=new Builder().forBrowser('chrome').setChromeOptions(new chrome.Options().debuggerAddress(process.env.LP5F_DEBUGGER_ADDRESS));
if(process.env.LP5F_CHROMEDRIVER)builder=builder.setChromeService(new chrome.ServiceBuilder(process.env.LP5F_CHROMEDRIVER));
const expected=[
  [
    [
      "Opening household wealth",
      "$102,916.27"
    ],
    [
      "External gross income",
      "$0.00"
    ],
    [
      "Employee allocation",
      "$0.00"
    ],
    [
      "Employer additions",
      "$0.00"
    ],
    [
      "Investment return",
      "$3,542.77"
    ],
    [
      "Consumption paid",
      "$98,880.76"
    ],
    [
      "Consumption assessed",
      "$100,875.55"
    ],
    [
      "Tax liability",
      "$7,578.30"
    ],
    [
      "Actual taxes paid",
      "$7,578.28"
    ],
    [
      "Unpaid tax",
      "$0.02"
    ],
    [
      "Unfunded essential expenses",
      "$1,994.79"
    ],
    [
      "Unmet optional saving",
      "$0.00"
    ],
    [
      "Reserve gap at period end",
      "$12,000.00"
    ],
    [
      "Cash reinvested (all sources)",
      "$0.00"
    ],
    [
      "Closing household wealth",
      "$0.00"
    ]
  ],
  [
    [
      "Concurrent saved cash",
      "$0.00",
      "$9,529.28",
      "$9,530.45",
      "$1.16",
      "Account rounding adjustment: $0.01",
      "$0.00"
    ],
    [
      "Brokerage",
      "$0.00",
      "$0.00",
      "$0.00",
      "$0.00",
      "—",
      "$0.00"
    ],
    [
      "Workplace traditional",
      "$102,916.27",
      "$0.00",
      "$106,457.88",
      "$3,541.61",
      "—",
      "$0.00"
    ]
  ],
  [
    [
      "Workplace traditional",
      "$9,529.28",
      "$9,529.28",
      "$9,529.28",
      "$9,529.28"
    ]
  ]
];
(async()=>{
 const d=await builder.build();
 const panel='[data-lifetime-year-panel="2053"]';
 const strings=()=>d.executeScript('return [...document.querySelector(arguments[0]).querySelectorAll("table")].map(t=>[...t.querySelectorAll("tbody tr")].map(r=>[...r.querySelectorAll("th,td")].map(c=>c.textContent.trim())))',panel);
 const axe=fs.readFileSync(require.resolve(resolve('axe-core/axe.min.js')),'utf8');
 async function check(theme,phase){
  assert.equal(await d.findElement(By.id('lifetime-year-select')).getAttribute('value'),'2053');
  assert.ok(await d.findElement(By.css(panel)).isDisplayed(),'2053 must be selected and available');
  assert.equal(await d.executeScript('return document.getElementById("lifetime-dialog").open'),false);
  assert.deepEqual(await strings(),expected,'containment must preserve every baseline rendered financial string');
  assert.equal(await d.findElement(By.css(panel+' td .sr-only')).getAttribute('textContent'),'Account rounding adjustment: ');
  assert.match(await d.findElement(By.css(panel+' td .sr-only')).findElement(By.xpath('..')).getAttribute('textContent'),/\$0\.01$/,'fixture must contain the nonzero adjustment');
  const layout=await d.executeScript('window.scrollTo(100000,window.scrollY);return {client:document.documentElement.clientWidth,scroll:document.documentElement.scrollWidth,x:scrollX,dpr:devicePixelRatio,zoom:getComputedStyle(document.documentElement).zoom}');
  console.log(JSON.stringify({theme,phase,layout}));
  assert.equal(layout.dpr,1,'caller must restore native browser zoom to100% for390px regression');
  assert.equal(layout.zoom,'1');
  assert.ok(layout.scroll<=layout.client+1,'document must reflow without escaped absolute labels');
  assert.equal(layout.x,0,'actual document horizontal scroll must remain zero');
  for(const name of ['household','accounts','rmd']){
   const region=await d.findElement(By.css(panel+' [role=region][aria-labelledby="lifetime-'+name+'-2053"]'));
   assert.equal(await region.getAttribute('tabindex'),'0');
   assert.ok((await region.getAccessibleName()).trim(),'native region needs an accessible name');
   assert.ok((await region.findElement(By.css('caption')).getAttribute('textContent')).includes('2053'));
   if(name==='household')continue;
   await d.executeScript('arguments[0].scrollIntoView({block:"center"});arguments[0].scrollLeft=0;arguments[0].focus()',region);
   assert.ok(await d.executeScript('return arguments[0].scrollWidth>arguments[0].clientWidth',region),'financial table must exercise actual horizontal scrolling');
   for(let i=0;i<8;i++)await region.sendKeys(Key.ARROW_RIGHT);
   await d.wait(()=>d.executeScript('return arguments[0].scrollLeft>0',region),5000);
   assert.deepEqual(await d.executeScript('return {focus:document.activeElement===arguments[0],page:scrollX}',region),{focus:true,page:0});
  }
  await d.executeScript(axe);
  const violations=await d.executeAsyncScript('const done=arguments[arguments.length-1];axe.run(document,{runOnly:{type:"tag",values:["wcag2a","wcag2aa","wcag21a","wcag21aa","wcag22aa"]}}).then(r=>done(r.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))))');
  assert.deepEqual(violations,[],theme+' '+phase+' axe');
 }
 try{
  await d.manage().setTimeouts({script:60000});
  await d.sendDevToolsCommand('Network.setCacheDisabled',{cacheDisabled:true});
  await d.manage().window().setRect({width:390,height:844});
  for(const theme of ['light','dark']){
  await d.get(process.env.LP5F_URL.replace(/\/$/,'')+'/whatif');
  await d.wait(until.elementLocated(By.id('lifetime-year-select')),60000);
  await d.executeScript('document.querySelector("[data-wf-tab=cashflow]").click();const s=document.getElementById("lifetime-year-select");s.value="2053";s.dispatchEvent(new Event("change",{bubbles:true}))');
   if(await d.executeScript('return document.documentElement.classList.contains("dark")')!==(theme==='dark'))await d.executeScript('document.getElementById("theme-toggle").click()');
   await d.sleep(800);
   await check(theme,'initial');
   await d.executeScript('document.getElementById("lifetime-year-select").focus()');
   await d.executeAsyncScript('const done=arguments[arguments.length-1];document.body.addEventListener("htmx:afterSwap",()=>setTimeout(done,200),{once:true});htmx.ajax("GET","/whatif/poll?since=-1",{target:"#whatif-results",swap:"outerHTML"})');
   assert.equal(await d.executeScript('return document.activeElement.id'),'lifetime-year-select');
   await check(theme,'HTMX replacement');
  }
  console.log('LIFETIME REFLOW PASS');
 }finally{await d.quit();}
})().catch(e=>{console.error(e);process.exitCode=1;});
