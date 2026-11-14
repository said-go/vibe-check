const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const result = { score: 75, level_title: 'Стиляга', primary_category: { title: 'Спорт-шик' }, summary: 'Всё хорошо', metrics: { coherence: 80, originality: 70, color_harmony: 75 }, recommendations: ['Один акцент'], photo: 'SECRET_IMAGE' };
function history({ unavailable = false, fail = false, rows = [] } = {}) {
  const data = new Map(rows.map(row => [row.id, row]));
  let closed = 0;
  const db = { close() { closed++; }, transaction() {
    const tx = { objectStore() { return {
      getAll: () => request(() => [...data.values()]),
      put: row => request(() => data.set(row.id, row)),
      delete: id => request(() => data.delete(id)), clear: () => request(() => data.clear())
    }; } };
    function request(work) { const req = {}; queueMicrotask(() => { if (!fail) { req.result = work(); req.onsuccess?.(); } }); return req; }
    setImmediate(() => { if (fail) { tx.error = new Error('quota'); tx.onabort?.(); } else tx.oncomplete?.(); });
    return tx;
  } };
  const indexedDB = { open() { const req = {}; queueMicrotask(() => { req.result = db; req.onsuccess(); }); return req; } };
  const window = unavailable ? {} : { indexedDB };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../dist/history.js'), 'utf8'), { window, indexedDB, Date });
  return { api: window.VIBE_HISTORY, data, closed: () => closed };
}
test('history saves a whitelist without image data and migrates old category titles', async () => {
  const h = history(); await h.api.save('one', result);
  const rows = await h.api.list(); assert.equal(rows[0].category, 'Спортик');
  assert.ok(!JSON.stringify([...h.data.values()]).includes('SECRET_IMAGE'));
  assert.equal(rows[0].metrics.colorHarmony, 75);
  assert.ok(h.closed() >= 2);
});
test('history enforces 50 records and supports deletion and clearing', async () => {
  const h = history(); for (let i = 0; i < 53; i++) await h.api.save(String(i), result);
  assert.equal(h.data.size, 50); const rows = await h.api.list(); assert.equal(rows.length, 50);
  await h.api.remove(rows[0].id); assert.equal((await h.api.list()).length, 49);
  await h.api.clear(); assert.equal((await h.api.list()).length, 0);
});
test('malformed stored rows cannot break listing or return stored photo fields', async () => {
  const h = history({ rows: [{ id: 'broken', createdAt: 1 }, { id: 'invalid-date', createdAt: 1e20, category: 'Стиляга', metrics: {} }, { id: 'valid', createdAt: 2, category: 'Гот', metrics: {}, photo: 'SECRET_IMAGE' }] });
  const rows = await h.api.list(); assert.equal(rows.length, 1); assert.equal(rows[0].id, 'valid');
  assert.ok(!JSON.stringify(rows).includes('SECRET_IMAGE'));
});
test('concurrent saves retain at most fifty records', async () => {
  const h = history(); await Promise.all(Array.from({ length: 70 }, (_, i) => h.api.save(String(i), result)));
  assert.equal(h.data.size, 50); assert.equal((await h.api.list()).length, 50);
});
test('storage unavailable and quota errors reject; aborted transactions close the database', async () => {
  await assert.rejects(history({ unavailable: true }).api.list(), /storage_unavailable/);
  const h = history({ fail: true }); await assert.rejects(h.api.save('one', result), /quota/);
  assert.equal(h.closed(), 1); assert.equal(h.data.size, 0);
  await assert.rejects(h.api.save('one', null), /invalid_result/);
});
function card({ contextMissing = false, blobMissing = false } = {}) {
  const texts = []; const calls = []; const png = { type: 'image/png' };
  const ctx = new Proxy({ measureText: s => ({ width: s.length * 12 }), fillText: text => texts.push(text) }, { get: (o, k) => k in o ? o[k] : () => {} });
  const canvas = { getContext: () => contextMissing ? null : ctx, toBlob(cb, type) { calls.push(type); cb(blobMissing ? null : png); } };
  const window = {}; const document = { fonts: { ready: Promise.resolve() }, createElement(type) { assert.equal(type, 'canvas'); return canvas; } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../dist/share-card.js'), 'utf8'), { window, document });
  return { api: window.VIBE_SHARE_CARD, texts, calls, canvas, png };
}
test('share card creates PNG with text metrics and never draws the supplied photo', async () => {
  const c = card(); const record = history().api.prepare('one', result); record.photo = 'SECRET_IMAGE';
  assert.equal(await c.api.create(record), c.png); assert.equal(c.calls[0], 'image/png');
  assert.equal(c.canvas.width, 1080); assert.equal(c.canvas.height, 1350);
  assert.ok(c.texts.includes('75')); assert.ok(!c.texts.join().includes('SECRET_IMAGE'));
  assert.ok(c.texts.join(' ').includes('Оценка одежды: 75/100'));
  assert.ok(!c.texts.join(' ').includes('Стиляга'));
});
test('share card reports canvas or PNG encoding failure', async () => {
  const record = history().api.prepare('one', result);
  await assert.rejects(card({ contextMissing: true }).api.create(record), /canvas_unavailable/);
  await assert.rejects(card({ blobMissing: true }).api.create(record), /image_unavailable/);
});

test('history preserves explanations and uncertainty without turning portrait results into zero ratings', async () => {
  const h = history();
  await h.api.save('portrait', { ...result, score: 0, assessment_scope: 'portrait', provisional: true,
    assessment_note: 'Одежду не оцениваем: видна только улыбка.', explanations: ['В кадре лёгкая улыбка.'] });
  const [record] = await h.api.list();
  assert.equal(record.score, null); assert.equal(record.metrics.coherence, null);
  assert.equal(record.levelTitle, ''); assert.equal(record.assessmentScope, 'portrait');
  assert.equal(record.provisional, true); assert.equal(record.explanations[0], 'В кадре лёгкая улыбка.');
  const c = card(); await c.api.create(record);
  assert.ok(c.texts.includes('ПРЕДВАРИТЕЛЬНЫЙ ВЕРДИКТ'));
  assert.ok(c.texts.join(' ').includes('Одежду не оцениваем'));
  assert.ok(!c.texts.includes('0')); assert.ok(!c.texts.includes('0%'));
});

test('old history records default to full outfit and malformed evidence is bounded', async () => {
  const h = history();
  const old = h.api.prepare('old', result);
  assert.equal(old.provisional, false); assert.equal(old.assessmentScope, 'outfit');
  assert.equal(old.levelTitle, '');
  const record = h.api.prepare('new', { ...result, provisional: 'true',
    explanations: [null, {}, 'a'.repeat(250), 'second', 'third', 'fourth'], assessment_note: 'n'.repeat(500) });
  assert.equal(record.provisional, false); assert.equal(record.explanations.length, 3);
  assert.equal(record.explanations[0].length, 180); assert.equal(record.assessmentNote.length, 320);
});

test('partial outfit share card retains uncertainty outside the joke', async () => {
  const record = history().api.prepare('partial', { ...result, provisional: true, assessment_note: 'Видна только часть одежды.' });
  const c = card(); await c.api.create(record);
  assert.ok(c.texts.includes('ПРЕДВАРИТЕЛЬНЫЙ ВЕРДИКТ'));
  assert.ok(c.texts.join(' ').includes('детали могут изменить результат'));
});

test('renamed categories migrate when reading old saved records', async () => {
  for (const [before, after] of [['Собранный','Бизнесмен'],['Здравый стиль','Стиляга'],['Уличный стиль','Абушка']]) {
    const h = history({rows:[{id:'old',createdAt:1,category:before,metrics:{},score:70}]});
    const [record] = await h.api.list(); assert.equal(record.category,after);
  }
});

test('mixed verdict remains one title through saving and sharing', async () => {
  const h=history();await h.api.save('mixed',{...result,primary_category:{title:'Нефор-качок'}});
  const [record]=await h.api.list();assert.equal(record.category,'Нефор-качок');
  const c=card();await c.api.create(record);
  assert.ok(c.texts.join(' ').includes('НЕФОР- КАЧОК'));
  assert.ok(c.texts.join(' ').includes('Оценка одежды: 75/100'));
});
