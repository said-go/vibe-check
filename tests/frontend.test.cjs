const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const appSource = fs.readFileSync(path.join(__dirname, '../dist/app.js'), 'utf8');
const swSource = fs.readFileSync(path.join(__dirname, '../dist/sw.js'), 'utf8');
const flush = () => new Promise(resolve => setImmediate(resolve));
const response = (body, status = 200, headers = {}) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });

async function browser() {
  const nodes = new Map(), timers = new Map(), intervals = new Map(), events = new Map();
  let time = 100000, timerID = 0, handler = async () => response({ status: 'ready', ai_available: true });
  let calls = [], reloads = 0, cameraCalls = 0;
  const node = selector => {
    if (!nodes.has(selector)) nodes.set(selector, { hidden: selector === '#camera-modal', disabled: false, textContent: '', style: {}, checked: false,
      classList: { add() {}, remove() {}, toggle() {} }, listeners: {}, clicks: 0,
      addEventListener(type, fn) { this.listeners[type] = fn; }, setAttribute() {}, removeAttribute() {}, focus() {}, append() {}, replaceChildren() {},
      click() { this.clicks++; }, querySelectorAll() { return []; }, getContext() { return { drawImage() {}, fillRect() {}, save() {}, restore() {}, translate() {}, scale() {} }; }
    });
    return nodes.get(selector);
  };
  const swEvents = new Map();
  const window = { addEventListener: (type, fn) => events.set(type, fn), location: { reload: () => reloads++ },
    VIBE_HISTORY: { list: async () => [], prepare: (id, result) => ({ id, ...result }) },
    VIBE_SHARE_CARD: { create: async () => new Blob(['card']) } };
  class ClockDate extends Date { static now() { return time; } }
  const context = vm.createContext({ window, document: { querySelector: node, createElement: () => node('new-node'), body: { style: {} }, addEventListener() {} },
    navigator: { onLine: true, mediaDevices: { getUserMedia: async () => { cameraCalls++; throw Error('camera disabled'); } }, serviceWorker: { controller: {}, addEventListener: (t, fn) => swEvents.set(t, fn) } },
    AbortController, Blob, FormData, URL, Date: ClockDate, Intl, console, crypto: require('node:crypto').webcrypto,
    fetch: (...args) => { calls.push(args); return handler(...args); },
    setTimeout: (fn, ms) => { const id = ++timerID; timers.set(id, { fn, ms }); return id; }, clearTimeout: id => timers.delete(id),
    setInterval: fn => { const id = ++timerID; intervals.set(id, fn); return id; }, clearInterval: id => intervals.delete(id)
  });
  vm.runInContext(appSource, context);
  await flush(); calls = [];
  return { node, run: code => vm.runInContext(code, context), timers, intervals, swEvents,
    setFetch: fn => { handler = fn; }, calls: () => calls, reloads: () => reloads, cameraCalls: () => cameraCalls,
    advance: ms => { time += ms; for (const fn of [...intervals.values()]) fn(); },
    prepare: () => vm.runInContext("modal.hidden=false; currentFile=new Blob(['jpeg'],{type:'image/jpeg'}); currentImage='test'; analysisKey='same-photo-key-123'; $('#privacy-consent').checked=true;", context)
  };
}

test('presets-only readiness still uploads after consent and uses stable key', async () => {
  const b = await browser(); b.prepare();
  b.setFetch(async (url, opts) => url.includes('readyz') ? response({status:'ready',ai_available:false,presets_available:true}) : response({code:'ai_unavailable',retryable:true},503));
  await b.run('analyze()');
  assert.equal(b.calls().filter(([url]) => url.endsWith('/analyses')).length,1);
  assert.equal(b.calls().at(-1)[1].headers['Idempotency-Key'],'same-photo-key-123');
  assert.match(b.node('#error-description').textContent,/недоступен/);
  assert.equal(b.node('#retry-analysis').hidden,false);
});
test('missing consent never uploads', async () => {
  const b=await browser(); b.prepare(); b.node('#privacy-consent').checked=false;
  await b.run('analyze()'); assert.equal(b.calls().length,0);
});
test('malformed successful response shows an actionable error', async () => {
  const b=await browser();b.prepare();
  b.setFetch(async url=>url.includes('readyz')?response({status:'ready'}):response({status:'completed'}));
  await b.run('analyze()');assert.match(b.node('#error-description').textContent,/неверный ответ/);
});

test('incomplete result is rejected before rendering or saving any record', async () => {
  const b=await browser();b.prepare();
  b.setFetch(async url=>url.includes('readyz')?response({status:'ready'}):response({status:'completed',analysis_id:'a',result:{score:42}}));
  await b.run('analyze()');
  assert.match(b.node('#error-description').textContent,/неверный ответ/);
  assert.equal(b.run('currentRecord'),null);
});

test('valid server result renders the result step without automatically saving history', async () => {
  const b=await browser();b.prepare();
  const result={score:74,primary_category:{title:'Спортик'},level_title:'На расслабоне',summary:'Кроссовки готовы к прогулке.',metrics:{coherence:80,originality:60,color_harmony:75},recommendations:[]};
  b.setFetch(async url=>url.includes('readyz')?response({status:'ready'}):response({status:'completed',analysis_id:'test-analysis',result}));
  await b.run('analyze()');
  assert.equal(b.node('#result-step').hidden,false);
  assert.equal(b.node('#result-title').textContent,'Спортик');
  assert.equal(b.node('#result-score').textContent,74);
  assert.equal(b.node('#save-result').disabled,false);
  assert.equal(b.run('activeRequest'),null);
});

test('panoramic image that becomes too narrow after resize never reaches upload', async () => {
  const b=await browser();b.prepare();
  await b.run("globalThis.Image=class { naturalWidth=10000; naturalHeight=500; async decode() {} }; useFile(new Blob(['image'],{type:'image/jpeg'}))");
  assert.match(b.node('#error-description').textContent,/вытянутое/);
  assert.equal(b.run('currentFile'),null);
  assert.equal(b.calls().length,0);
});

test('oversize upload gets an actionable error before decoding or sending', async () => {
  const b=await browser();b.prepare();
  await b.run('useFile({size:26*1024*1024})');
  assert.match(b.node('#error-description').textContent,/до 25 МБ/);
  assert.equal(b.calls().length,0);
});

test('empty file is rejected before image decoding', async () => {
  const b=await browser();b.prepare();
  await b.run("useFile(new Blob([]))");
  assert.match(b.node('#error-description').textContent,/пустой/);
  assert.equal(b.run('currentFile'),null);
});

test('unsupported HEIC receives format-specific guidance without uploading', async () => {
  const b=await browser();b.prepare();
  await b.run("globalThis.Image=class { async decode() {throw Error('unsupported');} }; useFile(new Blob(['heic'],{type:'image/heic'}))");
  assert.match(b.node('#error-description').textContent,/HEIC.*JPEG/);
  assert.equal(b.calls().length,0);
});

test('decoded oversized dimensions are rejected before allocating canvas', async () => {
  const b=await browser();b.prepare();
  await b.run("globalThis.Image=class {naturalWidth=20000;naturalHeight=10000;async decode(){}}; useFile(new Blob(['image']))");
  assert.match(b.node('#error-description').textContent,/100 мегапикселей/);
  assert.equal(b.node('#canvas').width,0);
});

test('stalled decode times out and releases its timer without uploading', async () => {
  const b=await browser();b.prepare();
  const pending=b.run("globalThis.Image=class {decode(){return new Promise(()=>{});}}; useFile(new Blob(['image']))");
  const timeout=[...b.timers.values()].find(t=>t.ms===15000);
  assert.ok(timeout);timeout.fn();await pending;
  assert.match(b.node('#error-description').textContent,/долго открывается/);
  assert.equal([...b.timers.values()].some(t=>t.ms===15000),false);
  assert.equal(b.calls().length,0);
});

test('late decode failure after closing cannot overwrite UI', async () => {
  const b=await browser();b.prepare();
  const pending=b.run("let rejectDecode; globalThis.Image=class {decode(){return new Promise((_,reject)=>{rejectDecode=reject;});}}; useFile(new Blob(['image']))");
  b.run("closeModal(); rejectDecode(Error('decode failed'))");await pending;
  assert.equal(b.node('#camera-modal').hidden,true);
  assert.equal(b.node('#error-description').textContent,'');
  assert.equal(b.run('currentFile'),null);
});
test('analysis timeout aborts the request and offers retry without losing photo key', async () => {
  const b=await browser(); b.prepare();
  b.setFetch(async (url,options)=>url.includes('readyz')?response({status:'ready'}):new Promise((resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')))));
  const pending=b.run('analyze()');await flush();
  [...b.timers.values()].find(t=>t.ms===125000).fn();await pending;
  assert.match(b.node('#error-description').textContent,/слишком много времени/);
  assert.equal(b.run('analysisKey'),'same-photo-key-123');assert.equal(b.run('activeRequest'),null);
});
test('closing the modal cancels without displaying an error or losing control of UI',async()=>{
  const b=await browser();b.prepare();
  b.setFetch(async (url,options)=>url.includes('readyz')?response({status:'ready'}):new Promise((resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')))));
  const pending=b.run('analyze()');await flush();b.run('closeModal()');await pending;
  assert.equal(b.node('#camera-modal').hidden,true);assert.equal(b.node('#error-description').textContent,'');assert.equal(b.timers.size,0);
});
test('Retry-After blocks retries until its deadline',async()=>{
  const b=await browser();b.prepare();
  b.setFetch(async url=>url.includes('readyz')?response({status:'ready'}):response({code:'analysis_rate_limited'},429,{'Retry-After':'3'}));
  await b.run('analyze()');assert.equal(b.node('#retry-analysis').disabled,true);
  const calls=b.calls().length;await b.run('analyze()');assert.equal(b.calls().length,calls);
  b.advance(3000);assert.equal(b.node('#retry-analysis').disabled,false);assert.equal(b.intervals.size,0);
});
test('selecting another uploaded photo opens file picker, not camera',async()=>{
  const b=await browser();b.prepare();b.run("lastPhotoSource='file'; chooseAgain()");
  assert.equal(b.node('#modal-file').clicks,1);assert.equal(b.cameraCalls(),0);
});
test('worker activation in another tab does not reload an open photo',async()=>{
  const b=await browser();b.prepare();b.swEvents.get('controllerchange')();
  assert.equal(b.reloads(),0);assert.equal(b.node('#update-banner').hidden,true);
  b.run('closeModal()');assert.equal(b.node('#update-banner').hidden,false);
});

function serviceWorker({ shell, offline = new Response('offline'), network = new Response('new html'), assets = {}, previous, tabs = [], versions = [] } = {}) {
  const handlers=new Map();let calls=0, writes=0;const deleted=[];
  const cache={match:async key=>{const path=typeof key==='string'?key:new URL(key.url).pathname;return path==='/index.html'?shell:path==='/offline.html'?offline:assets[path];},put:async()=>writes++,addAll:async()=>{}};
  const context=vm.createContext({self:{location:{origin:'https://vibe.test'},addEventListener:(t,fn)=>handlers.set(t,fn),clients:{claim:async()=>{},matchAll:async()=>tabs},skipWaiting(){}},
    caches:{open:async()=>cache,match:async()=>previous,keys:async()=>versions,delete:async key=>{deleted.push(key);return true;}},URL,Response,
    fetch:async()=>{calls++;if(network instanceof Error)throw network;return network;}});
  vm.runInContext(swSource,context);
  return { dispatch(path,mode='navigate',method='GET') {let result;handlers.get('fetch')({request:{url:'https://vibe.test'+path,mode,method},respondWith:p=>result=p});return result;},activate(){let result;handlers.get('activate')({waitUntil:p=>result=p});return result;},deleted,calls:()=>calls,writes:()=>writes};
}
test('installed release serves its own HTML even when newer HTML exists on network',async()=>{
  const sw=serviceWorker({shell:new Response('installed html')});assert.equal(await (await sw.dispatch('/')).text(),'installed html');assert.equal(sw.calls(),0);
});
test('network 500 never replaces the offline shell',async()=>{
  const sw=serviceWorker({network:new Response('server error',{status:500})});assert.equal(await (await sw.dispatch('/')).text(),'offline');assert.equal(sw.writes(),0);
});
test('offline network failure serves the fallback and API/photo uploads are never intercepted',async()=>{
  const sw=serviceWorker({network:Error('offline')});assert.equal(await (await sw.dispatch('/')).text(),'offline');
  assert.equal(sw.dispatch('/api/readyz','cors'),undefined);assert.equal(sw.dispatch('/api/v1/analyses','cors','POST'),undefined);
});

test('manifest comes from current release even if an older cache has a matching URL',async()=>{
  const sw=serviceWorker({assets:{'/manifest.webmanifest':new Response('current manifest')},previous:new Response('old manifest')});
  assert.equal(await (await sw.dispatch('/manifest.webmanifest','cors')).text(),'current manifest');
});

test('only hashed resources can fall back to a previous release',async()=>{
  const sw=serviceWorker({previous:new Response('old hashed asset'),network:new Response('current network manifest')});
  assert.equal(await (await sw.dispatch('/assets/app.0123456789abcdef.js','cors')).text(),'old hashed asset');
  assert.equal(await (await sw.dispatch('/manifest.webmanifest','cors')).text(),'current network manifest');
});

test('multiple updates keep all older caches while any tab is still open',async()=>{
  const sw=serviceWorker({tabs:[{id:'old-photo-tab'}],versions:['vibe-check-a','vibe-check-b','vibe-check-c','other-app']});
  await sw.activate();assert.deepEqual(sw.deleted,[]);
});

test('activation without open tabs reclaims old releases but preserves unrelated caches',async()=>{
  const sw=serviceWorker({versions:['vibe-check-a','vibe-check-b','other-app']});
  await sw.activate();assert.deepEqual(sw.deleted,['vibe-check-a','vibe-check-b']);
});
