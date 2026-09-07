// Author command/evidence recorder; does not write checker verdicts.
const fs = require('node:fs'), path = require('node:path'), cp = require('node:child_process');
const root = '/tmp/DI5-fourth-worker.1ptHqE';
const out = path.join(root, '.swarm/DI-run/reports/DI5.4');
fs.mkdirSync(out, {recursive: true});
const [name, cwd, command, ...args] = process.argv.slice(2);
const log = fs.openSync(path.join(out, name + '.log'), 'wx');
const start = new Date().toISOString();
const child = cp.spawn(command, args, {cwd, env: process.env, stdio: ['ignore', log, log]});
child.on('exit', (code, signal) => {
  fs.closeSync(log);
  const result = {name, cwd, command: [command, ...args], start, end: new Date().toISOString(), exit: code, signal,
    env: Object.fromEntries(Object.entries(process.env).filter(([key]) => key.startsWith('DI5_')))};
  fs.writeFileSync(path.join(out, name + '-command.json'), JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result));
  console.log(fs.readFileSync(path.join(out, name + '.log'), 'utf8').slice(-1600));
  process.exitCode = code === null ? 1 : code;
});
