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
  const docEvents = new Map(); let activeElement = null;
  const nodes = new Map(), timers = new Map(), intervals = new Map(), events = new Map();
  let time = 100000, timerID = 0, handler = async () => response({ status: 'ready', ai_available: true });
  let calls = [], reloads = 0, cameraCalls = 0, createdNodes = 0;
  const node = selector => {
    if (!nodes.has(selector)) nodes.set(selector, { hidden: selector === '#camera-modal', disabled: false, isConnected: true, inert: false, offsetParent: {}, textContent: '', style: {}, checked: false,
      classList: { add() {}, remove() {}, toggle() {} }, listeners: {}, clicks: 0, children: [], dataset: {},
      addEventListener(type, fn) { this.listeners[type] = fn; }, setAttribute() {}, removeAttribute() {}, focus() { activeElement = this; }, append(...children) { this.children.push(...children); }, replaceChildren(...children) { this.children = children; },
      click() { this.clicks++; }, querySelectorAll() { return []; }, getContext() { return { drawImage() {}, fillRect() {}, save() {}, restore() {}, translate() {}, scale() {} }; }
    });
    return nodes.get(selector);
  };
  const swEvents = new Map();
  const window = { addEventListener: (type, fn) => events.set(type, fn), location: { reload: () => reloads++ },
    VIBE_HISTORY: { list: async () => [], prepare: (id, result) => ({ id, ...result }) },
    VIBE_SHARE_CARD: { create: async () => new Blob(['card']) } };
  class ClockDate extends Date { static now() { return time; } }
  const context = vm.createContext({ window, document: { querySelector: node, createElement: () => node(`new-node-${++createdNodes}`), body: { style: {} }, get activeElement() { return activeElement; }, addEventListener(type, fn) { docEvents.set(type, fn); } },
    navigator: { onLine: true, mediaDevices: { getUserMedia: async () => { cameraCalls++; throw Error('camera disabled'); } }, serviceWorker: { controller: {}, addEventListener: (t, fn) => swEvents.set(t, fn) } },
    AbortController, Blob, FormData, URL, Date: ClockDate, Intl, console, crypto: require('node:crypto').webcrypto,
    fetch: (...args) => { calls.push(args); return handler(...args); },
    setTimeout: (fn, ms) => { const id = ++timerID; timers.set(id, { fn, ms }); return id; }, clearTimeout: id => timers.delete(id),
    setInterval: fn => { const id = ++timerID; intervals.set(id, fn); return id; }, clearInterval: id => intervals.delete(id)
  });
  vm.runInContext(appSource, context);
  await flush(); calls = [];
  return { node, docEvents, active: () => activeElement, run: code => vm.runInContext(code, context), timers, intervals, swEvents,
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

test('legacy level titles never appear in history or shared text', async () => {
  const b = await browser();
  const text = b.run(`(() => {
    const record = {id:'old',createdAt:100000,category:'Клоун',levelTitle:'Здравый пацан',score:72};
    const card = historyCard(record);
    const flatten = node => [node.textContent, ...node.children.map(flatten)].join(' ');
    return flatten(card) + ' ' + resultText(record);
  })()`);
  assert.ok(text.includes('Клоун'));
  assert.ok(text.includes('Оценка одежды: 72/100'));
  assert.ok(!text.includes('Здравый пацан'));
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
  assert.equal(b.node('#result-level').textContent,'Оценка одежды: 74/100');
  assert.equal(b.node('#save-result').disabled,false);
  assert.equal(b.run('activeRequest'),null);
});

test('partial assessment displays safe explanation text and clears it for legacy presets', async () => {
  const b = await browser(); b.prepare();
  const base = {score:70,primary_category:{title:'Спортик'},level_title:'Стиляга',summary:'Тест',metrics:{coherence:70,originality:70,color_harmony:70},recommendations:[]};
  let result = {...base,provisional:true,assessment_scope:'outfit',assessment_note:'Видна только часть одежды.',explanations:['Спортивные детали.','<img src=x onerror=alert(1)>']};
  b.setFetch(async url => url.includes('readyz') ? response({status:'ready'}) : response({status:'completed',analysis_id:'partial',result}));
  await b.run('analyze()');
  assert.equal(b.node('#result-status').textContent,'предварительный вердикт');
  assert.equal(b.node('#result-assessment-note').textContent,'Видна только часть одежды.');
  assert.equal(b.node('#result-explanation').hidden,false);
  assert.equal(b.node('#result-reasons').children[1].textContent,'<img src=x onerror=alert(1)>');
  result=base;
  await b.run('analyze()');
  assert.equal(b.node('#result-assessment-note').hidden,true);
  assert.equal(b.node('#result-explanation').hidden,true);
  assert.equal(b.node('#result-reasons').children.length,0);
  assert.equal(b.node('#result-status').textContent,'вердикт готов');
});

test('portrait verdict hides unassessed outfit scores and restores them on next outfit', async () => {
  const b=await browser(); b.prepare();
  const base={score:0,primary_category:{title:'Типо крутой'},level_title:'',summary:'Тест',metrics:{coherence:0,originality:0,color_harmony:0},recommendations:[]};
  let result={...base,provisional:true,assessment_scope:'portrait',assessment_note:'Одежду не видно.',explanations:['Палец поднят рядом с лицом.']};
  b.setFetch(async url=>url.includes('readyz')?response({status:'ready'}):response({status:'completed',analysis_id:'portrait',result}));
  await b.run('analyze()');
  for(const id of ['#result-stats','#result-score','#result-level']) assert.equal(b.node(id).hidden,true);
  result={...base,assessment_scope:'outfit',score:70,level_title:'Стиляга'};
  await b.run('analyze()');
  for(const id of ['#result-stats','#result-score','#result-level']) assert.equal(b.node(id).hidden,false);
  assert.match(b.run("resultText({category:'Типо крутой',summary:'Тест',score:null,provisional:true})"),/^Предварительный вердикт/);
});

test('invalid explanation metadata is rejected before saving a record',async()=>{
  const b=await browser();b.prepare();
  b.setFetch(async url=>url.includes('readyz')?response({status:'ready'}):response({status:'completed',analysis_id:'bad',result:{score:70,primary_category:{title:'Тест'},level_title:'Тест',summary:'Тест',metrics:{coherence:70,originality:70,color_harmony:70},explanations:[{text:'wrong'}]}}));
  await b.run('analyze()');
  assert.equal(b.run('currentRecord'),null);
  assert.match(b.node('#error-description').textContent,/неверный ответ/);
});

test('no style evidence shows a specific photo suggestion without a fabricated verdict',async()=>{
  const b=await browser();b.prepare();
  b.setFetch(async url=>url.includes('readyz')?response({status:'ready'}):response({status:'low_confidence',result:{low_confidence_reason:'insufficient_style_evidence'}}));
  await b.run('analyze()');
  assert.equal(b.run('currentRecord'),null);
  assert.match(b.node('#error-description').textContent,/Фото загрузилось/);
  assert.equal(b.node('#retry-analysis').hidden,true);
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
  const pending=b.run('analyze()');await flush();b.docEvents.get('keydown')({key:'Escape'});await pending;
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


test('modal isolates background and restores focus and overflow on Escape', async () => {
  const b=await browser();b.node('footer').inert=true;
  b.run("document.body.style.overflow='scroll'");b.node('#open-file').focus();b.run('openModal(false)');
  assert.equal(b.node('main').inert,true);assert.equal(b.node('header').inert,true);
  b.docEvents.get('keydown')({key:'Escape'});
  assert.equal(b.node('#camera-modal').hidden,true);assert.equal(b.node('main').inert,false);
  assert.equal(b.node('footer').inert,true);assert.equal(b.active(),b.node('#open-file'));
  assert.equal(b.run('document.body.style.overflow'),'scroll');
});
for (const action of ['save-result', 'download-card', 'share']) {
  for (const failed of [false, true]) {
    test(`old ${action} ${failed ? 'failure' : 'completion'} cannot alter a newer result`, async () => {
      const b = await browser();
      b.run(`
        const sampleResult = { score: 70, level_title: 'Стиляга', primary_category: { title: 'Стиляга' }, summary: 'Тест', metrics: { coherence: 70, originality: 70, color_harmony: 70 }, recommendations: [] };
        modal.hidden = false;
        showResult(sampleResult, 'old');
        let resolveAction, rejectAction;
        const actionPending = new Promise((resolve, reject) => { resolveAction = resolve; rejectAction = reject; });
        let downloads = 0;
        window.VIBE_HISTORY.save = () => actionPending;
        window.VIBE_SHARE_CARD.download = () => { downloads++; };
        navigator.clipboard = { writeText: () => actionPending };
      `);
      // Avoid the pre-rendered PNG so download waits for encoding and share uses clipboard.
      await flush();
      b.run("currentCardBlob = null; window.VIBE_SHARE_CARD.create = () => actionPending;");
      const pending = b.node('#' + action).listeners.click();
      assert.equal(b.node('#' + action).disabled, true);
      b.run("window.VIBE_SHARE_CARD.create = async () => new Blob(['new']); showResult(sampleResult, 'new');");
      assert.equal(b.node('#' + action).disabled, false);
      // A new operation owns this button now; old finally must not enable it.
      b.node('#' + action).disabled = true;
      b.run(failed ? "rejectAction(new Error('storage or encoding unavailable'))" : "resolveAction(new Blob(['old']))");
      await pending;
      assert.equal(b.node('#' + action).disabled, true);
      assert.equal(b.node('#save-result').textContent, 'Сохранить в историю');
      assert.equal(b.node('#share-note').textContent, '');
      assert.equal(b.run('downloads'), 0);
    });
  }
}

test('removed modal opener falls back to launch button',async()=>{
  const b=await browser();b.node('#open-file').focus();b.run('openModal(false)');
  b.node('#open-file').isConnected=false;b.run('closeModal()');assert.equal(b.active(),b.node('#open-camera'));
});
test('Tab wraps visible controls while excluding hidden step',async()=>{
  const b=await browser();b.run('openModal(false)');
  const first=b.node('first-control'),last=b.node('last-control'),hidden=b.node('hidden-control');hidden.offsetParent=null;
  b.node('#camera-modal').querySelectorAll=()=>[first,hidden,last];let prevented=0;
  last.focus();b.node('#camera-modal').listeners.keydown({key:'Tab',shiftKey:false,preventDefault(){prevented++;}});assert.equal(b.active(),first);
  b.node('#camera-modal').listeners.keydown({key:'Tab',shiftKey:true,preventDefault(){prevented++;}});assert.equal(b.active(),last);assert.equal(prevented,2);
});
