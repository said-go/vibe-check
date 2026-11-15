const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const app = fs.readFileSync(path.join(__dirname, '../dist/app.js'), 'utf8');
const updateCode = app.slice(app.indexOf('let pendingServiceWorker = null;'));
const workerCode = fs.readFileSync(path.join(__dirname, '../dist/sw.js'), 'utf8');

function page({ controlled = true, reportOpen = false, waiting = null } = {}) {
  const events = new Map(), workerEvents = new Map(), registrationEvents = new Map();
  const nodes = new Map();
  let reloads = 0;
  const node = id => {
    if (!nodes.has(id)) nodes.set(id, { hidden: true, addEventListener(type, fn) { this[type] = fn; } });
    return nodes.get(id);
  };
  const modal = { hidden: !reportOpen };
  const registration = { waiting, update: async () => {}, addEventListener: (type, fn) => registrationEvents.set(type, fn) };
  const serviceWorker = { controller: controlled ? {} : null, register: async () => registration, addEventListener: (type, fn) => workerEvents.set(type, fn) };
  const context = vm.createContext({ modal, $: node, navigator: { serviceWorker }, window: { location: { reload: () => reloads++ }, addEventListener: (type, fn) => events.set(type, fn) } });
  vm.runInContext(updateCode, context);
  return {
    load: () => events.get('load')(),
    activate() { serviceWorker.controller = {}; workerEvents.get('controllerchange')(); },
    report(open) { modal.hidden = !open; vm.runInContext('showPendingUpdate()', context); },
    click: () => node('#update-app').click(),
    banner: () => !node('#update-banner').hidden,
    reloads: () => reloads,
    offer(worker) {
      registration.installing = worker;
      registrationEvents.get('updatefound')();
      worker.installed();
    }
  };
}

function waitingWorker() {
  const listeners = new Map(), messages = [];
  return { state: 'installing', messages, postMessage: message => messages.push(message.type), addEventListener: (type, fn) => listeners.set(type, fn), installed() { this.state = 'installed'; listeners.get('statechange')(); } };
}

test('first-install tab ignores its initial claim but offers later cross-tab upgrades', async () => {
  const tab = page({ controlled: false });
  await tab.load();
  tab.activate();
  assert.equal(tab.reloads(), 0);
  assert.equal(tab.banner(), false);
  // This tab began uncontrolled; another tab now activates a later release.
  tab.activate();
  assert.equal(tab.reloads(), 0);
  assert.equal(tab.banner(), true);
  tab.click();
  assert.equal(tab.reloads(), 1);
});

test('initial-visit report survives two later activations and reloads only after explicit action', async () => {
  const tab = page({ controlled: false, reportOpen: true });
  await tab.load();
  tab.activate();
  tab.activate();
  tab.activate();
  assert.equal(tab.reloads(), 0);
  assert.equal(tab.banner(), false);
  tab.click();
  assert.equal(tab.reloads(), 0);
  tab.report(false);
  assert.equal(tab.banner(), true);
  tab.click();
  assert.equal(tab.reloads(), 1);
});

test('activation requested by one tab does not reload a report open in another tab', async () => {
  const waiting = waitingWorker();
  const requester = page({ waiting }), report = page({ reportOpen: true, waiting });
  await Promise.all([requester.load(), report.load()]);
  requester.click();
  assert.deepEqual(waiting.messages, ['SKIP_WAITING']);
  requester.activate(); report.activate();
  assert.equal(requester.reloads(), 1);
  assert.equal(report.reloads(), 0);
  report.report(false);
  assert.equal(report.banner(), true);
  report.click();
  assert.equal(report.reloads(), 1);
});

test('a report opened during activation defers even a requested reload', async () => {
  const tab = page({ waiting: waitingWorker() });
  await tab.load();
  tab.click();
  tab.report(true);
  tab.activate();
  assert.equal(tab.reloads(), 0);
  tab.report(false);
  assert.equal(tab.banner(), true);
  tab.click();
  assert.equal(tab.reloads(), 1);
});

test('update installed while a report is open is offered only after closing it', async () => {
  const tab = page({ reportOpen: true });
  await tab.load();
  const worker = waitingWorker();
  tab.offer(worker);
  assert.equal(tab.banner(), false);
  tab.report(false);
  assert.equal(tab.banner(), true);
  tab.click();
  assert.deepEqual(worker.messages, ['SKIP_WAITING']);
});

test('three worker releases preserve old tab assets while new navigations use the active shell', async () => {
  const cachesByName = new Map();
  let tabs = [{ id: 'report-tab-on-first-release' }];
  const key = request => typeof request === 'string' ? request : new URL(request.url).pathname;
  const caches = {
    async open(name) {
      if (!cachesByName.has(name)) cachesByName.set(name, new Map());
      const data = cachesByName.get(name);
      return { match: async request => data.get(key(request))?.clone(), async addAll(paths) { for (const p of paths) data.set(p, new Response(`${name}:${p}`)); } };
    },
    keys: async () => [...cachesByName.keys()],
    delete: async name => cachesByName.delete(name),
    async match(request) { for (const data of cachesByName.values()) { const value = data.get(key(request)); if (value) return value.clone(); } }
  };
  function release(letter) {
    const handlers = new Map(), name = `vibe-check-${letter}`, asset = `/assets/app.${letter.repeat(16)}.js`;
    const code = workerCode.replace(/const CACHE_NAME = .*?;/, `const CACHE_NAME = '${name}';`).replace(/const APP_SHELL = .*?;/, `const APP_SHELL = ${JSON.stringify(['/index.html', asset, '/manifest.webmanifest', '/offline.html'])};`);
    vm.runInNewContext(code, { caches, URL, Response, fetch: async () => { throw new Error('offline'); }, self: { location: { origin: 'https://vibe.test' }, addEventListener: (type, fn) => handlers.set(type, fn), clients: { matchAll: async () => tabs, claim: async () => {} } } });
    return {
      asset,
      lifecycle(type) { let work; handlers.get(type)({ waitUntil: value => work = value }); return work; },
      get(p, mode = 'cors') { let response; handlers.get('fetch')({ request: { method: 'GET', url: `https://vibe.test${p}`, mode }, respondWith: value => response = value }); return response; }
    };
  }
  const a = release('a'), b = release('b'), c = release('c');
  for (const worker of [a, b, c]) { await worker.lifecycle('install'); await worker.lifecycle('activate'); }
  assert.equal(await (await c.get('/', 'navigate')).text(), 'vibe-check-c:/index.html');
  assert.equal(await (await c.get(a.asset)).text(), `vibe-check-a:${a.asset}`);
  assert.equal(await (await c.get(b.asset)).text(), `vibe-check-b:${b.asset}`);
  assert.equal(await (await c.get('/manifest.webmanifest')).text(), 'vibe-check-c:/manifest.webmanifest');
  assert.equal(c.get('/api/v1/analyses'), undefined);
  tabs = [];
  await c.lifecycle('activate');
  assert.deepEqual(await caches.keys(), ['vibe-check-c']);
});
