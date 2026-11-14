(function () {
  const WIDTH = 1080;
  const HEIGHT = 1350;
  const INK = '#151515';
  const ACID = '#efff48';
  const PAPER = '#f5f1ea';
  const BLUE = '#3c62ff';

  function wrap(ctx, value, x, y, width, lineHeight, maxLines) {
    const words = String(value || '').split(/\s+/).filter(Boolean);
    const lines = [];
    let line = '';
    for (const word of words) {
      const next = line ? line + ' ' + word : word;
      if (ctx.measureText(next).width <= width) {
        line = next;
        continue;
      }
      if (line) { lines.push(line); line = ''; }
      // Break unspaced titles and URLs as well as ordinary words.
      for (const character of word) {
        if (line && ctx.measureText(line + character).width > width) {
          lines.push(line);
          line = '';
        }
        line += character;
      }
    }
    if (line) lines.push(line);
    const visible = lines.slice(0, maxLines);
    if (lines.length > maxLines) {
      const tail = Array.from(visible[maxLines - 1]);
      while (tail.length && ctx.measureText(tail.join('') + '…').width > width) tail.pop();
      visible[maxLines - 1] = tail.join('') + '…';
    }
    visible.forEach((text, index) => ctx.fillText(text, x, y + index * lineHeight, width));
  }

  async function create(record) {
    if (document.fonts?.ready) await document.fonts.ready;
    const canvas = document.createElement('canvas');
    canvas.width = WIDTH; canvas.height = HEIGHT;
    const ctx = canvas.getContext('2d');
    if (!ctx) throw new Error('canvas_unavailable');
    ctx.fillStyle = PAPER; ctx.fillRect(0, 0, WIDTH, HEIGHT);
    ctx.fillStyle = ACID; ctx.fillRect(0, 0, WIDTH, 160);
    ctx.fillStyle = INK; ctx.font = '800 72px Unbounded, Arial, sans-serif';
    ctx.fillText('ВАЙБЧЕК', 80, 108);
    ctx.font = '800 28px Manrope, Arial, sans-serif';
    ctx.fillText(record.provisional ? 'ПРЕДВАРИТЕЛЬНЫЙ ВЕРДИКТ' : 'МОЙ ОБРАЗ СЕГОДНЯ', 80, 260);

    ctx.fillStyle = BLUE; ctx.fillRect(80, 310, 920, 450);
    if (record.score != null) {
      ctx.fillStyle = ACID; ctx.beginPath(); ctx.arc(830, 525, 128, 0, Math.PI * 2); ctx.fill();
      ctx.fillStyle = INK; ctx.textAlign = 'center'; ctx.font = '800 100px Unbounded, Arial, sans-serif';
      ctx.fillText(String(record.score), 830, 560);
    }
    ctx.textAlign = 'left'; ctx.fillStyle = '#ffffff'; ctx.font = '800 42px Unbounded, Arial, sans-serif';
    wrap(ctx, record.category.toUpperCase().replace(/-/g, '- '), 130, 435, 535, 60, 3);
    ctx.font = '700 30px Manrope, Arial, sans-serif';
    wrap(ctx, record.score == null ? 'По позе и деталям в кадре' : `Оценка одежды: ${record.score}/100`, 130, 620, 520, 44, 2);

    ctx.fillStyle = INK; ctx.font = '800 29px Manrope, Arial, sans-serif';
    const metrics = [
      ['ЦЕЛЬНОСТЬ', record.metrics.coherence],
      ['ОРИГИНАЛЬНОСТЬ', record.metrics.originality],
      ['ЦВЕТ', record.metrics.colorHarmony]
    ];
    if (record.score == null) {
      wrap(ctx, record.assessmentNote || 'Вердикт по фото — без оценки одежды', 80, 850, 900, 42, 3);
    } else metrics.forEach(([label, value], index) => {
      const x = 80 + index * 310;
      ctx.strokeStyle = INK; ctx.lineWidth = 3; ctx.strokeRect(x, 810, 290, 160);
      ctx.fillText(label, x + 20, 860, 250);
      ctx.font = '800 65px Unbounded, Arial, sans-serif';
      ctx.fillText(`${value}%`, x + 20, 940);
      ctx.font = '800 29px Manrope, Arial, sans-serif';
    });

    ctx.font = '700 31px Manrope, Arial, sans-serif';
    wrap(ctx, record.summary, 80, 1055, 900, 47, 3);
    if (record.provisional && record.score != null) {
      ctx.font = '600 23px Manrope, Arial, sans-serif';
      ctx.fillText('По видимой части образа; детали могут изменить результат.', 80, 1210);
    }
    ctx.fillStyle = ACID; ctx.fillRect(0, 1240, WIDTH, 110);
    ctx.fillStyle = INK; ctx.font = '800 26px Manrope, Arial, sans-serif';
    ctx.fillText('РАЗВЛЕКАТЕЛЬНЫЙ РЕЗУЛЬТАТ · БЕЗ ФОТО', 80, 1306);

    return new Promise((resolve, reject) => {
      canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('image_unavailable')), 'image/png');
    });
  }

  function download(blob, filename = 'vibe-check.png') {
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url; anchor.download = filename;
    document.body.append(anchor); anchor.click(); anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 30000);
  }

  window.VIBE_SHARE_CARD = Object.freeze({ create, download });
})();
