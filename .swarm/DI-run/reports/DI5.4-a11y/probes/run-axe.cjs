const fs=require('fs'),{spawnSync}=require('child_process');
const routes=JSON.parse(fs.readFileSync(__dirname+'/artifacts/navigation.json','utf8'));
const urls=routes.flatMap(r=>['light','dark'].map(t=>'http://127.0.0.1:18874/'+r.slice(1)+'-'+t+'.html'));
const args=['proxy','npx','--no-install','@axe-core/cli',...urls,'--tags','wcag2a,wcag2aa,wcag21a,wcag21aa,wcag22aa','--chrome-path',__dirname+'/chrome-1440.sh','--chromedriver-path','/home/darrell/.config/nvm/versions/node/v24.12.0/lib/node_modules/@axe-core/cli/node_modules/chromedriver/lib/chromedriver/chromedriver','--chrome-options=--no-sandbox','--save','artifacts/axe.json','--exit'];
fs.writeFileSync(__dirname+'/artifacts/axe-command.json',JSON.stringify({command:'rtk',args},null,2));
const p=spawnSync('rtk',args,{cwd:__dirname,encoding:'utf8',timeout:300000,maxBuffer:16000000});
fs.writeFileSync(__dirname+'/artifacts/axe-cli.log',(p.stdout||'')+(p.stderr||''));
console.log(p.stdout||'');console.error(p.stderr||'');console.log('AXE_EXIT='+p.status);process.exitCode=p.status??2;
