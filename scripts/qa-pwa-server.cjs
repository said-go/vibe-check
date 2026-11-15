// Isolated, loopback-only browser QA. Synthetic responses; never calls AI or stores uploads.
// Run after build-pwa.mjs. Change backend/private/pwa-browser-qa/state.json to switch v1/v2.
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');
const project = path.resolve(__dirname, '..');
const source = path.join(project, 'dist');
const fixture = path.join(project, 'backend/private/pwa-browser-qa');
const stateFile = path.join(fixture, 'state.json');
const port = Number(process.env.PWA_QA_PORT || 8092);
if (!Number.isInteger(port) || port < 1024 || port > 65535) throw Error('Invalid QA port');
const hash = data => crypto.createHash('sha256').update(data).digest('hex').slice(0, 16);
const text = file => fs.readFileSync(file, 'utf8').replace(/\r\n/g, '\n');
fs.mkdirSync(fixture, {recursive:true});
for (const version of ['v1', 'v2']) {
  const root = path.join(fixture, version);
  fs.cpSync(source, root, {recursive:true});
  const css = text(path.join(source, 'styles.css')) + `\n/* browser QA ${version} */\n`;
  const cssName = `assets/styles.${hash(css)}.css`;
  fs.writeFileSync(path.join(root, cssName), css);
  const html = text(path.join(source, 'index.html'))
    .replace(/assets\/styles\.[a-f0-9]{16}\.css/g, cssName)
    .replace('<body>', `<body><p id="qa-release">LOCAL PWA QA ${version.toUpperCase()} — synthetic, no AI</p>`);
  fs.writeFileSync(path.join(root, 'index.html'), html);
  const worker = text(path.join(source, 'sw.js'))
    .replace(/const CACHE_NAME = .*?;/, `const CACHE_NAME = 'vibe-check-qa-${hash(html)}';`)
    .replace(/\/assets\/styles\.[a-f0-9]{16}\.css/g, `/${cssName}`);
  fs.writeFileSync(path.join(root, 'sw.js'), worker);
}
if (!fs.existsSync(stateFile)) fs.writeFileSync(stateFile, JSON.stringify({version:'v1',available:true}));
const counts = {analyses:0,bytesDiscarded:0};
const types = {'.html':'text/html; charset=utf-8','.js':'application/javascript; charset=utf-8','.css':'text/css; charset=utf-8','.png':'image/png','.webmanifest':'application/manifest+json'};
const server = http.createServer(async (req,res) => {
  const json = (status,body) => {res.writeHead(status, {'Content-Type':'application/json','Cache-Control':'no-store'}); res.end(JSON.stringify(body));};
  let state;
  try {state = JSON.parse(text(stateFile).replace(/^\uFEFF/,''));} catch {return json(503,{error:'QA state invalid'});}
  if (!['v1','v2'].includes(state.version)) return json(503,{error:'QA version invalid'});
  const url = new URL(req.url,'http://127.0.0.1');
  if (url.pathname === '/__qa/status') return json(200,{...state,...counts});
  if (state.available === false) return json(503,{error:'QA server unavailable'});
  if (url.pathname === '/api/readyz') return json(200,{status:'ready',ai_available:true,diagnostics_available:false});
  if (url.pathname === '/api/healthz') return json(200,{status:'ok'});
  if (url.pathname === '/api/v1/analyses' && req.method === 'POST') {
    let bytes = 0;
    for await (const chunk of req) {bytes += chunk.length; if(bytes>10*1024*1024)return json(413,{code:'image_too_large'});}
    counts.analyses++; counts.bytesDiscarded+=bytes;
    return json(200,{status:'completed',analysis_id:'pwa-synthetic-browser-check',result:{status:'completed',primary_category:{code:'sporty',title:'Спортик',score:74},score:74,confidence:90,metrics:{coherence:80,originality:60,color_harmony:75},summary:'Синтетический отчёт для проверки обновления. Фото в историю не сохраняется.',recommendations:['Тест без обращения к AI.'],explanations:['Проверяем сохранность отчёта.'],provisional:false}});
  }
  if (!['GET','HEAD'].includes(req.method)) return json(405,{error:'method not allowed'});
  let relative;
  try {relative = url.pathname === '/' ? 'index.html' : decodeURIComponent(url.pathname.slice(1));} catch {return json(400,{error:'invalid path'});}
  const root = path.join(fixture,state.version);
  const file = path.resolve(root,relative);
  if (!file.startsWith(root+path.sep)) return json(404,{error:'not found'});
  try {
    const body = fs.readFileSync(file);
    res.writeHead(200,{'Content-Type':types[path.extname(file)]||'application/octet-stream','Cache-Control':'no-store'});
    res.end(req.method === 'HEAD' ? undefined : body);
  } catch {json(404,{error:'not found'});}
});
server.listen(port,'127.0.0.1',()=>console.log(`Local PWA QA http://127.0.0.1:${port}/ ; state ${stateFile} ; no AI`));
