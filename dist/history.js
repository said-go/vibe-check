(function () {
  const DATABASE = 'vibe-check-results';
  const STORE = 'results';
  const LIMIT = 50;

  function open() {
    return new Promise((resolve, reject) => {
      if (!('indexedDB' in window)) { reject(new Error('storage_unavailable')); return; }
      const request = indexedDB.open(DATABASE, 1);
      request.onupgradeneeded = () => {
        const db = request.result;
        if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE, { keyPath: 'id' });
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error || new Error('storage_unavailable'));
    });
  }

  function transaction(db, mode, run) {
    return new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, mode);
      const request = run(tx.objectStore(STORE));
      let result;
      if (request) {
        request.onsuccess = () => { result = request.result; };
      }
      tx.oncomplete = () => resolve(result);
      tx.onerror = () => reject(tx.error || new Error('storage_failed'));
      tx.onabort = () => reject(tx.error || new Error('storage_failed'));
    });
  }

  async function withDatabase(work) {
    const db = await open();
    try { return await work(db); } finally { db.close(); }
  }

  const displayCategory = title => ({'Спорт-шик':'Спортик','Собранный':'Бизнесмен','Здравый стиль':'Стиляга','Уличный стиль':'Абушка'}[title] || title);

  const list = () => withDatabase(async db => {
    const rows = await transaction(db, 'readonly', store => store.getAll());
    return rows.filter(row => row && typeof row.id === 'string' && row.id &&
      Number.isFinite(row.createdAt) && Number.isFinite(new Date(row.createdAt).getTime()) && typeof row.category === 'string' && row.metrics && typeof row.metrics === 'object')
      .map(row => ({ ...compact(row.id, {
        score: row.score, level_title: row.levelTitle, primary_category: { title: row.category },
        summary: row.summary, metrics: { coherence: row.metrics.coherence, originality: row.metrics.originality, color_harmony: row.metrics.colorHarmony },
        recommendations: row.recommendations, explanations: row.explanations,
        provisional: row.provisional, assessment_note: row.assessmentNote, assessment_scope: row.assessmentScope
      }), createdAt: row.createdAt }))
      .sort((a, b) => b.createdAt - a.createdAt).slice(0, LIMIT);
  });

  function compact(id, result) {
    if (!result || typeof result !== 'object') throw new Error('invalid_result');
    const text = (value, max = 180) => String(value || '').slice(0, max);
    const score = value => value == null ? null : Math.max(0, Math.min(100, Number.isFinite(value) ? Math.round(value) : 0));
    const portrait = result.assessment_scope === 'portrait';
    return {
      id: text(id, 80), createdAt: Date.now(),
      score: portrait ? null : score(result.score), levelTitle: '',
      category: displayCategory(text(result.primary_category?.title, 80)),
      summary: text(result.summary, 240),
      explanations: Array.isArray(result.explanations) ? result.explanations.filter(item => typeof item === 'string').slice(0, 3).map(item => text(item, 180)) : [],
      provisional: result.provisional === true,
      assessmentNote: text(result.assessment_note, 320),
      assessmentScope: portrait ? 'portrait' : 'outfit',
      metrics: {
        coherence: portrait ? null : score(result.metrics?.coherence), originality: portrait ? null : score(result.metrics?.originality),
        colorHarmony: portrait ? null : score(result.metrics?.color_harmony)
      },
      recommendations: Array.isArray(result.recommendations) ? result.recommendations.slice(0, 3).map(item => text(item, 160)) : []
    };
  }

  async function save(id, result) {
    const record = compact(id, result);
    if (!record.id) throw new Error('invalid_result');
    await withDatabase(db => transaction(db, 'readwrite', store => {
      store.put(record);
      const request = store.getAll();
      request.onsuccess = () => {
        const rows = request.result;
        rows.sort((a, b) => (b?.createdAt || 0) - (a?.createdAt || 0));
        for (const row of rows.slice(LIMIT)) if (row?.id) store.delete(row.id);
      };
    }));
    return record;
  }

  const remove = id => withDatabase(db => transaction(db, 'readwrite', store => store.delete(id)));
  const clear = () => withDatabase(db => transaction(db, 'readwrite', store => store.clear()));

  window.VIBE_HISTORY = Object.freeze({ prepare: compact, list, save, remove, clear });
})();
