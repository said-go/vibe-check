// Loaded only by the opt-in, loopback QA server. Never included in production dist.
(() => {
  'use strict';
  const panel = document.createElement('details');
  panel.id = 'qa-memory-panel';
  panel.style.cssText = 'margin:12px;padding:12px;border:2px dashed #555;background:white;color:black;font:14px Arial;overflow:auto';
  const heading = document.createElement('summary');
  heading.textContent = 'QA: приблизительный JS heap браузера';
  const explanation = document.createElement('p');
  explanation.textContent = 'Снимай замеры после закрытия отчёта. Это приблизительный JS heap, не вся память вкладки. Значения могут округляться и включать ещё не собранный мусор. Ряд замеров не доказывает отсутствие утечки.';
  const button = document.createElement('button');
  button.type = 'button';
  button.id = 'qa-memory-sample';
  button.textContent = 'Снять замер памяти';
  const message = document.createElement('p');
  message.id = 'qa-memory-status';
  message.setAttribute('role', 'status');
  const table = document.createElement('table');
  table.style.cssText = 'border-collapse:collapse;text-align:left';
  const caption = document.createElement('caption');
  caption.textContent = 'Ручные замеры (MiB), максимум 100 строк';
  const head = document.createElement('thead');
  const headerRow = document.createElement('tr');
  for (const label of ['Замер', 'С начала, с', 'Используется', 'Выделено']) {
    const cell = document.createElement('th');
    cell.textContent = label;
    cell.style.padding = '6px';
    headerRow.append(cell);
  }
  head.append(headerRow);
  const body = document.createElement('tbody');
  body.id = 'qa-memory-samples';
  table.append(caption, head, body);
  let samples = 0;
  const start = performance.now();
  button.addEventListener('click', () => {
    const memory = performance.memory;
    if (!memory || !Number.isFinite(memory.usedJSHeapSize) || !Number.isFinite(memory.totalJSHeapSize)) {
      message.textContent = 'Этот браузер не предоставляет performance.memory. Замер не выполнен.';
      return;
    }
    const values = [++samples, ((performance.now() - start) / 1000).toFixed(1),
      (memory.usedJSHeapSize / 1048576).toFixed(2), (memory.totalJSHeapSize / 1048576).toFixed(2)];
    const row = document.createElement('tr');
    for (const value of values) {
      const cell = document.createElement('td');
      cell.textContent = String(value);
      cell.style.padding = '6px';
      row.append(cell);
    }
    body.append(row);
    if (body.children.length > 100) body.firstElementChild.remove();
    message.textContent = `Замер ${samples}: используется ${values[2]} MiB, выделено ${values[3]} MiB.`;
  });
  panel.append(heading, explanation, button, message, table);
  document.querySelector('#qa-release').after(panel);
})();
