const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function page(withMain = false) {
 const listeners = {};
 const container = () => ({children: [], get firstChild() {return this.children[0] || null;},
  appendChild(e) {this.children.push(e); e.parentNode=this; e.isConnected=true;},
  insertBefore(e, next) {const i=this.children.indexOf(next); this.children.splice(i<0?this.children.length:i,0,e); e.parentNode=this; e.isConnected=true;}});
 const body = container(), main = withMain ? container() : null;
 if (main) main.appendChild({tag:'h1'});
 const document = {body, activeElement:null, querySelector(s) {assert.equal(s,'main');return main;}, visibilityState: 'visible', addEventListener(n, f) { (listeners[n] ||= []).push(f); },
  createElement(tag) { return {tag, style: {}, children: [], contains(e) {return e===this||this.children.some(c=>c===e||c.contains?.(e));}, setAttribute() {}, appendChild(e) { this.children.push(e); }, addEventListener(n,f) {this[n] = f;}, remove() { this.parentNode.children = this.parentNode.children.filter(e => e !== this); this.isConnected=false; }}; }};
 let state = {epoch:'one', revision:0}, reloads = 0, calls = 0, confirm = false, confirmations = 0, tick;
 const location = {href:'http://example.test/whatif?scenario=test#settings', reload() {reloads++;}};
 const window = {location, innerHeight:900, innerWidth:390, addEventListener: document.addEventListener, confirm() { confirmations++; return confirm; }};
 const context = {document, window, location, Set, Promise, setInterval(f, ms) {assert.equal(ms,2000); tick=f;}, fetch:async (url, options) => {
  calls++; assert.equal(url, '/api/ui-refresh'); assert.equal(options.cache,'no-store'); assert.equal(options.mode,'same-origin');
  if (state instanceof Error) throw state;
  return {ok: true, json:async()=>state};
 }};
 const source = fs.existsSync(__dirname+'/page-refresh.js') ? fs.readFileSync(__dirname+'/page-refresh.js','utf8') : '';
 vm.runInNewContext(source,context);
 const flush = async()=>{await new Promise(resolve=>setImmediate(resolve));};
 return {document, body, main, location, flush, async poll() {if(tick) await tick(); await flush();},
  event(n, e={}) {(listeners[n]||[]).forEach(f=>f(e));}, signal(revision,epoch='one') {state={epoch,revision};}, fail() {state=new Error('offline');},
  accept() {confirm=true;}, get reloads(){return reloads;}, get calls(){return calls;}, get confirmations(){return confirmations;}};
}
test('clean tab establishes baseline, reloads once, keeps query/hash; new tab does not loop',async()=>{
 const p=page(); await p.flush(); assert.equal(p.calls,1); p.signal(2); await p.poll(); await p.poll();
 assert.equal(p.reloads,1); assert.equal(p.location.href,'http://example.test/whatif?scenario=test#settings');
 const fresh=page(); fresh.signal(2); await fresh.flush(); await fresh.poll(); assert.equal(fresh.reloads,0);
});
test('hidden signals coalesce until visibility returns',async()=>{
 const p=page(); await p.flush(); p.document.visibilityState='hidden'; p.signal(1); await p.poll(); p.signal(3); await p.poll(); assert.equal(p.reloads,0);
 p.document.visibilityState='visible'; p.event('visibilitychange'); await p.flush(); assert.equal(p.reloads,1);
});
test('dirty edits require explicit confirmation, refusal preserves edits without dialog spam',async()=>{
 const p=page(); await p.flush(); p.event('input',{target:{matches:()=>true}}); p.signal(1); await p.poll();
 assert.equal(p.reloads,0); assert.equal(p.confirmations,0); assert.equal(p.body.children.length,1);
 const notice=p.body.children[0]; const button=notice.children.find(e=>e.tag==='button'); assert.ok(button);
 button.focus(); assert.equal(button.style.outline,'2px solid currentColor');
 button.click(); assert.equal(p.confirmations,1); assert.equal(p.reloads,0);
 p.signal(3); await p.poll(); assert.equal(p.confirmations,1);
 p.accept(); button.click(); assert.equal(p.reloads,1);
});
test('clean HTMX requests and native submissions defer, including overlapping requests',async()=>{
 const p=page(); await p.flush(); const a={},b={};
 p.event('htmx:beforeRequest',{detail:{xhr:a}}); p.event('htmx:beforeRequest',{detail:{xhr:b}});
 p.signal(1); await p.poll(); assert.equal(p.reloads,0);
 p.event('htmx:afterRequest',{detail:{xhr:a}}); await p.poll(); assert.equal(p.reloads,0);
 p.event('htmx:afterRequest',{detail:{xhr:b}}); await p.poll(); assert.equal(p.reloads,1);
 const q=page(); await q.flush(); q.event('submit',{defaultPrevented:false}); q.signal(1); await q.poll(); assert.equal(q.reloads,0);
});
test('failure and restart rebaseline safely; focus and interval do not overlap',async()=>{
 const p=page(); await p.flush(); p.fail(); await p.poll(); assert.equal(p.reloads,0);
 p.signal(0,'two'); await p.poll(); assert.equal(p.reloads,0);
 const calls=p.calls; p.event('focus'); p.event('focus'); await p.flush(); assert.equal(p.calls,calls+1);
 p.signal(1,'two'); await p.poll(); assert.equal(p.reloads,1);
});
test('dismissal removes visible and announced notice until a new signal',async()=>{
 const p=page(); await p.flush(); p.event('change',{target:{matches:()=>true}}); p.signal(1); await p.poll();
 const dismiss=p.body.children[0].children.find(e=>e.textContent==='Keep editing'); dismiss.click();
 await p.poll(); assert.equal(p.body.children.length,0); assert.equal(p.reloads,0);
 p.signal(2); await p.poll(); assert.equal(p.body.children.length,1);
});
test('new epoch clears a pending dirty notice and rebaselines below the old revision',async()=>{
 const p=page(); await p.flush();
 p.event('input',{target:{matches:()=>true}});
 p.signal(7); await p.poll();
 assert.equal(p.body.children.length,1, 'old epoch has a pending dirty notice');
 p.signal(0,'restarted'); await p.poll();
 assert.equal(p.body.children.length,0, 'restart must remove the old epoch notice');
 assert.equal(p.reloads,0, 'restart must preserve unsaved edits');
 assert.equal(p.confirmations,0, 'restart must not prompt automatically');
 await p.poll();
 assert.equal(p.body.children.length,0, 'new baseline must remain quiet');
 p.signal(1,'restarted'); await p.poll();
 assert.equal(p.body.children.length,1, 'new epoch revision one must signal despite old revision seven');
 assert.equal(p.reloads,0, 'dirty protection must survive the restart');
 assert.equal(p.confirmations,0);
});
test('notice precedes main content without stealing focus; displaced input is kept visible',async()=>{
 const p=page(true); await p.flush();let scrolled=0,focused=0;
 const input={isConnected:true,getBoundingClientRect:()=>({top:910,bottom:940,left:20,right:200}),scrollIntoView(options){scrolled++;assert.equal(options.block,'nearest');},focus(){focused++;}};
 p.document.activeElement=input;p.event('input',{target:{matches:()=>true}});p.signal(1);await p.poll();
 assert.equal(p.main.children[0].tag,'section');assert.equal(p.main.children[1].tag,'h1');
 assert.equal(p.body.children.length,0);assert.equal(p.document.activeElement,input);assert.equal(focused,0);assert.equal(scrolled,1);
 await p.poll();assert.equal(scrolled,1,'polling must not keep jumping scroll');
});
test('HTMX replacement restores the same pending region/status once; partial swaps do not reannounce',async()=>{
 const p=page(true);await p.flush();p.event('input',{target:{matches:()=>true}});p.signal(1);await p.poll();
 const notice=p.main.children[0];assert.equal(notice.tag,'section','pending region belongs in main');
 const status=notice.children[0];
 p.event('htmx:afterSwap');assert.equal(p.main.children.filter(e=>e.tag==='section').length,1);
 notice.remove();p.event('htmx:afterSwap');
 assert.equal(p.main.children[0],notice);assert.equal(notice.children[0],status);
 p.event('htmx:afterSwap');assert.equal(p.main.children.filter(e=>e.tag==='section').length,1);
 p.signal(0,'restart');await p.poll();p.event('htmx:afterSwap');assert.equal(p.main.children.filter(e=>e.tag==='section').length,0);
});
test('no-main fallback precedes body content and does not scroll an already visible control',async()=>{
 const p=page();p.body.appendChild({tag:'h1'});await p.flush();
 p.document.activeElement={isConnected:true,getBoundingClientRect:()=>({top:100,bottom:130,left:20,right:200}),scrollIntoView(){assert.fail('visible focus must not scroll');}};
 p.event('input',{target:{matches:()=>true}});p.signal(1);await p.poll();
 assert.equal(p.body.children[0].tag,'section');assert.equal(p.body.children[1].tag,'h1');
});
