const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function render(result) {
  const nodes = new Map();
  const node = selector => {
    if (!nodes.has(selector)) nodes.set(selector, { textContent: '', hidden: false, replaceChildren(...items) { this.children = items; } });
    return nodes.get(selector);
  };
  let cardRecord;
  const window = { VIBE_SHARE_CARD: { create: async record => { cardRecord = record; return {}; } } };
  const context = vm.createContext({ window, Date, $: node,
    document: { createElement: () => ({ textContent: '' }) }, showStep() {} });
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../dist/history.js'), 'utf8'), context);
  const app = fs.readFileSync(path.join(__dirname, '../dist/app.js'), 'utf8');
  // Exercise the actual display path with the real history normalizer and a
  // card capture; unrelated camera/network setup belongs to frontend.test.cjs.
  vm.runInContext('let currentRecord, currentCardBlob, currentImage = "local-preview";\n' +
    app.slice(app.indexOf('function showResult('), app.indexOf('function resultText(')), context);
  context.result = result;
  vm.runInContext('showResult(result, "consistency-check")', context);
  return { node, record: cardRecord };
}

test('report and PNG share category migration, rounded scores and bounded report text', () => {
  const summary = 'Описание '.repeat(40);
  const { node, record } = render({ primary_category: { title: 'Спорт-шик' }, score: 75.4,
    metrics: { coherence: 80.6, originality: 70.1, color_harmony: 75.4 }, summary,
    recommendations: ['Совет'], explanations: ['Причина'], provisional: true, assessment_note: 'Обувь не видна.' });
  assert.equal(node('#result-title').textContent, 'Спортик');
  assert.equal(node('#result-title').textContent, record.category);
  assert.equal(node('#result-level').textContent, 'Оценка одежды: 75/100');
  assert.equal(node('#style-stat').textContent, '81%');
  assert.equal(node('#aura-stat').textContent, '75%');
  assert.equal(node('#result-description').textContent, record.summary);
  assert.ok(record.summary.endsWith('…'));
  assert.equal(node('#result-assessment-note').textContent, record.assessmentNote);
  assert.deepEqual(node('#result-reasons').children.map(item => item.textContent), Array.from(record.explanations));
});

test('portrait reports and share records consistently hide clothing ratings', () => {
  const { node, record } = render({ primary_category: { title: 'Типо крутой' }, score: 0,
    metrics: { coherence: 0, originality: 0, color_harmony: 0 }, summary: 'Жест в кадре.',
    assessment_scope: 'portrait', provisional: true, assessment_note: 'Одежда не оценивалась.' });
  assert.equal(node('#result-score').hidden, true);
  assert.equal(node('#result-level').hidden, true);
  assert.equal(node('#result-stats').hidden, true);
  assert.equal(record.score, null);
  assert.equal(record.metrics.coherence, null);
  assert.equal(record.assessmentScope, 'portrait');
  assert.equal(node('#result-assessment-note').textContent, record.assessmentNote);
});

test('mixed report preserves both category names and required cues without using category confidence as clothing score', () => {
  const title = 'Типо крутой-герой боевика';
  const explanations = [
    'Поднят указательный палец. Серьёзное выражение лица дополняет позу.',
    'Короткая стрижка или лысина, борода и видимый шрам на голове или лице — сочетание для шуточного кинообраза.'
  ];
  const { node, record } = render({
    primary_category: { title, score: 100 }, secondary_category: { title: 'Герой боевика', score: 100 },
    score: 46, metrics: { coherence: 50, originality: 25, color_harmony: 60 },
    summary: 'Два вайба в одном кадре. Одному названию тут явно тесно.', explanations
  });
  assert.equal(node('#result-title').textContent, title);
  assert.equal(record.category, title);
  assert.equal(node('#result-level').textContent, 'Оценка одежды: 46/100');
  assert.equal(record.score, 46);
  assert.deepEqual(node('#result-reasons').children.map(item => item.textContent), explanations);
  assert.deepEqual(Array.from(record.explanations), explanations);
});
