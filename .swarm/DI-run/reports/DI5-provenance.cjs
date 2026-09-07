const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),assert=require('node:assert/strict');
const root=process.env.DI5_SOURCE_ROOT,live=process.env.DI5_LIVE_ROOT,out=process.env.DI5_OUTPUT;
assert.ok(root&&root.startsWith('/tmp/'));assert.ok(live&&out&&out.startsWith(root+'/'));fs.mkdirSync(out,{recursive:true});
const hash=p=>crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex');
const baseline='/tmp/budget2-DI4-base.KPl7oY';
const map=JSON.parse(fs.readFileSync('/tmp/DI5-a11y-baseline.Yd8pno/artifacts/source-hashes.json','utf8'));
const manifest=fs.readFileSync(path.join(live,'.swarm/DI-run/manifests/DI4.2.files'),'utf8').trim().split('\n');
const application=manifest.filter(p=>/^(internal|web|cmd)\//.test(p)),changed=new Set(application);
const files=[];
for(const f of map.files){
 const p=f.path,current=hash(path.join(root,p));
 if(!changed.has(p))assert.equal(current,f.sha256,'unowned source differs from accepted baseline: '+p);
 files.push({path:p,baselineSHA256:f.sha256,finalSHA256:current,DI4:changed.has(p)});
}
for(const p of application){assert.equal(hash(path.join(root,p)),hash(path.join(live,p)),'frozen DI4 live/source drift: '+p);}
const owned=["internal/services/insights/classification_di5_test.go","internal/services/insights/period_matrix_di5_test.go","internal/services/insights/period_coverage_di5_test.go","internal/services/insights/forecast_bounds_di5_test.go","internal/services/insights/recurring_cadence_di5_test.go","internal/handlers/dashboard/recurring_di5_test.go","internal/services/mcpsvc/ledger/recurring_di5_test.go","internal/handlers/dashboard/period_consumers_di5_test.go","internal/templates/period_boundaries_di5_test.go","internal/services/mcpsvc/spend/grouping_di5_test.go","internal/handlers/insights/estimate_sums_di5_test.go","internal/handlers/dashboard/reporting_di5_test.go","internal/handlers/insights/http_links_di5_test.go","internal/handlers/insights/pricecreep_di5_test.go","cmd/server/saved_data_di5_test.go","internal/handlers/insights/recurring_render_di5_test.go",".swarm/DI-run/reports/DI5-dashboard-browser.cjs",".swarm/DI-run/reports/DI5-integration-browser.cjs",".swarm/DI-run/reports/DI5-a11y-browser.cjs",".swarm/DI-run/reports/DI5-refresh-browser.cjs","cmd/server/same_date_di5_test.go",".swarm/DI-run/reports/DI5-findings-browser.cjs",".swarm/DI-run/reports/DI5-verify.cjs"];
const own=owned.map(p=>({path:p,sha256:hash(path.join(live,p))}));
for(const p of owned.filter(p=>p.endsWith('.go')))assert.equal(hash(path.join(root,p)),hash(path.join(live,p)),'DI5 test snapshot drift: '+p);
const related=['web/static/js/page-refresh.js','web/static/css/tailwind.css','web/static/css/styles.css','web/templates/components/kpis.html','web/templates/pages/dashboard.html','web/templates/layouts/base.html'];
fs.writeFileSync(path.join(out,'provenance.json'),JSON.stringify({root,live,acceptedBaseline:baseline,originalRunBaseline:'69e484a (NOT rendered by this worker)',DI4Manifest:'.swarm/DI-run/manifests/DI4.2.files',DI4Application:application,files,owned:own,refreshAttribution:related.map(p=>({path:p,baseline:hash(path.join(baseline,p)),final:hash(path.join(root,p))}))},null,2));
const audits=JSON.parse(fs.readFileSync(path.join(root,'di5-evidence/a11y/summary.json'),'utf8'));
fs.writeFileSync(path.join(out,'a11y-counts.json'),JSON.stringify({pages:audits.length,violations:audits.reduce((n,r)=>n+r.violations.length,0),incompleteRules:audits.reduce((n,r)=>n+r.incomplete.length,0),incompleteNodeOccurrences:audits.reduce((n,r)=>n+r.incomplete.reduce((a,v)=>a+v.targets.length,0),0),explicitUnlabelled:audits.map(r=>({id:r.id,controls:r.dom.unlabelled})),animations:audits.map(r=>({id:r.id,animations:r.dom.animations}))},null,2));
console.log('Source provenance verified: '+files.length+' baseline files, '+application.length+' frozen DI4 paths, '+own.length+' DI5 files.');
