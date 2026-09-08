
const fs=require('fs'), vm=require('vm'), assert=require('assert/strict');
const code=fs.readFileSync(process.argv[2]+'/web/static/js/whatif-roth-conversion.js','utf8');
function run(detail, inside=true) {
 const handlers={}; let removed=0;
 const heading={dataset:{rothRevision:'7'},getAttribute:()=> '7'};
 const button={focus(){document.activeElement=button;}};
 const progress={textContent:''};
 const target={dataset:{rothRevision:'7'},getAttribute:()=> '7',
  querySelector(q){if(q==='form')return {};if(q.includes('data-roth-revision'))return heading;return null;},
  replaceChildren(){removed++;},contains(node){return node===heading;}};
 const controls={querySelector:()=>button};
 const document={readyState:'complete',activeElement:inside?heading:{},
  body:{addEventListener(n,f){handlers[n]=f;}},
  getElementById(id){return {'roth-recommendations-results':target,'roth-recommendations-progress':progress,'roth-recommendations-controls':controls}[id]||null;},
  querySelector(){return button;}};
 const window={location:{hash:'',pathname:'/whatif',search:''},__whatifRevision:999};
 vm.runInNewContext(code,{window,document,history:{},URL,console});
 handlers['whatif:revision']({detail});
 return {removed,progress:progress.textContent,focused:document.activeElement===button};
}
for(const detail of [7,{value:7},6,{value:6},'garbage',{value:'garbage'},null,{}, {value:'7junk'}]) {
 const actual=run(detail);
 assert.equal(actual.removed,0,'unchanged/older/malformed revision must retain choices: '+JSON.stringify(detail));
 assert.equal(actual.progress,'','must not falsely announce a change');
 assert.equal(actual.focused,false,'unchanged events must preserve focus');
}
for(const detail of [8,{value:8}]) {
 const actual=run(detail);
 assert.equal(actual.removed,1,'newer revision must invalidate');
 assert.match(actual.progress,/plan changed/i,'newer revision must announce');
 assert.equal(actual.focused,true,'return removed focus to Find');
 assert.equal(run(detail,false).focused,false,'do not steal unrelated focus');
}
console.log('REVISION OUTPUT PASS');
