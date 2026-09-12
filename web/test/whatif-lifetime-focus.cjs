const assert = require('node:assert/strict');
const base = process.env.LP4_NODE_MODULES || '/home/darrell/.config/nvm/versions/node/v24.12.0/lib/node_modules/@axe-core/cli/node_modules/';
const {Builder, By} = require(base + 'selenium-webdriver');
const chrome = require(base + 'selenium-webdriver/chrome');
const url = process.env.LP4_URL || 'http://127.0.0.1:18081';
(async () => {
 const d = await new Builder().forBrowser('chrome').setChromeOptions(new chrome.Options().debuggerAddress('127.0.0.1:19222')).setChromeService(new chrome.ServiceBuilder('/snap/bin/chromium.chromedriver')).build();
 const errors = [];
 async function click(el) { await d.executeScript('arguments[0].scrollIntoView({block:"center"})', el); await el.click(); }
 async function fresh() {
  await d.get(url + '/whatif?lp4durable=' + Date.now());
  await click(await d.findElement(By.id('lifetime-open')));
  await d.wait(() => d.executeScript('return document.getElementById("lifetime-dialog").open'), 5000);
  for (const detail of await d.findElements(By.css('#lifetime-dialog details'))) {
   if (!(await detail.getAttribute('open'))) await click(await detail.findElement(By.css('summary')));
  }
 }
 async function withdrawalOrder() { return d.executeScript('return Array.from(document.querySelectorAll("[data-withdrawal]"),x=>x.dataset.withdrawal)'); }
 async function focus(name, expected) {
  const got = await d.executeScript('return {inside:document.getElementById("lifetime-dialog").contains(document.activeElement),expected:document.activeElement.matches(arguments[0]),tag:document.activeElement.tagName,text:document.activeElement.textContent.trim().slice(0,80)}', expected);
  console.log(JSON.stringify({check:name,...got}));
  if (!got.inside || !got.expected) errors.push(name + ': focus must reach ' + expected + ', got ' + JSON.stringify(got));
 }
 try {
  await d.sendDevToolsCommand('Network.setCacheDisabled', {cacheDisabled:true});
  for (const [name,row,add] of [
   ['account','[data-lifetime-account]','[data-add-account]'],
   ['job','[data-lifetime-job]','[data-add-job]'],
   ['rule','[data-lifetime-rule]','[data-add-rule]'],
   ['saving','[data-lifetime-saving]','[data-add-saving]'],
   ['job YTD','[data-ytd-job]','[data-add-ytd-job]'],
   ['rule YTD','[data-ytd-rule]','[data-add-ytd-rule]']]) {
   await fresh();
   if (!(await d.findElements(By.css(row))).length) await click(await d.findElement(By.css(add)));
   let rows = await d.findElements(By.css(row));
   assert.ok(rows.length, name + ' add must create an observable row');
   while (rows.length) {
    await click(await rows[0].findElement(By.css('[data-remove-row]')));
    rows = await d.findElements(By.css(row));
    if (rows.length) await focus(name + ' sibling removal', '#lifetime-dialog input:not([type=hidden]),#lifetime-dialog select,#lifetime-dialog button');
   }
   await focus(name + ' sole removal', add);
  }
  for (const [name,row,add] of [['change','[data-change]','[data-add-change]'],['tier','[data-tier]','[data-add-tier]']]) {
   await fresh();
   if (!(await d.findElements(By.css('[data-lifetime-rule]'))).length) await click(await d.findElement(By.css('[data-add-rule]')));
   const rule = (await d.findElements(By.css('[data-lifetime-rule]')))[0];
   if (name === 'tier') await d.executeScript('arguments[0].value="match"; arguments[0].dispatchEvent(new Event("change",{bubbles:true}))', await rule.findElement(By.css('[data-field="employer.mode"]')));
   if (!(await rule.findElements(By.css(row))).length) await click(await rule.findElement(By.css(add)));
   let rows = await rule.findElements(By.css(row));
   while (rows.length) { await click(await rows[0].findElement(By.css('[data-remove-row]'))); rows = await rule.findElements(By.css(row)); }
   await focus(name + ' sole removal', add);
  }
  await fresh();
  for (let i=0;i<3;i++) {
   const choices = await d.findElements(By.css('#withdrawal-add option[value]:not([value=""])'));
   if (!choices.length) break;
   await d.executeScript('const s=document.getElementById("withdrawal-add");s.value=arguments[0];s.dispatchEvent(new Event("change",{bubbles:true}))', await choices[0].getAttribute('value'));
  }
  for (const action of ['data-order-down','data-order-up','data-order-remove']) {
   const before = await withdrawalOrder();
   const buttons = await d.findElements(By.css('[' + action + ']:not(:disabled)'));
   assert.ok(buttons.length, 'Fixture needs operable withdrawal ' + action);
   const movedID = await buttons[0].findElement(By.xpath('..')).getAttribute('data-withdrawal');
   await click(buttons[0]);
   const after = await withdrawalOrder();
   if (action === 'data-order-down') { const i=before.indexOf(movedID); assert.equal(after[i+1],movedID,'move down must preserve identity and advance actual order'); }
   if (action === 'data-order-up') { const i=before.indexOf(movedID); assert.equal(after[i-1],movedID,'move up must preserve identity and retreat actual order'); }
   if (action === 'data-order-remove') { assert.ok(!after.includes(movedID),'remove must delete selected identity'); assert.deepEqual(after,before.filter(id=>id!==movedID),'remove must preserve survivor order'); }
   await focus('withdrawal ' + action, '#lifetime-withdrawal-order button:not(:disabled),#withdrawal-add');
  }
  while ((await d.findElements(By.css('[data-withdrawal]'))).length) {
   const before = await withdrawalOrder(); const button=(await d.findElements(By.css('[data-order-remove]')))[0]; const removed=await button.findElement(By.xpath('..')).getAttribute('data-withdrawal');
   await click(button); const after=await withdrawalOrder(); assert.deepEqual(after,before.filter(id=>id!==removed),'remove-to-empty must preserve survivor order');
   await focus('withdrawal remove through empty', '#lifetime-withdrawal-order button:not(:disabled),#withdrawal-add');
  }
  const add = await d.findElement(By.id('withdrawal-add'));
  const option = (await d.findElements(By.css('#withdrawal-add option[value]:not([value=""])')))[0];
  assert.ok(option, 'Fixture needs a named withdrawal account to add');
  const addedID = await option.getAttribute('value');
  await d.executeScript('arguments[0].focus();arguments[0].value=arguments[1];arguments[0].dispatchEvent(new Event("change",{bubbles:true}))', add, addedID);
  assert.ok((await withdrawalOrder()).includes(addedID),'add must preserve selected account identity');
  await focus('withdrawal add', '#lifetime-withdrawal-order button:not(:disabled),#withdrawal-add');
  assert.deepEqual(errors, [], 'DURABLE FOCUS CONTRACT FAILED');
  console.log('DURABLE FOCUS PASS');
 } finally { await d.quit(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
