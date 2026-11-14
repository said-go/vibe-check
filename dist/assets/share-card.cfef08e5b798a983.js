(function () {
  const WIDTH = 1080;
  const HEIGHT = 1350;
  const INK = '#151515';
  const ACID = '#efff48';
  const PAPER = '#f5f1ea';
  const BLUE = '#3c62ff';

  function wrap(ctx, value, x, y, width, lineHeight, maxLines) {
    const words = String(value || '').split(/\s+/).filter(Boolean);
    let line = '';
    let lines = 0;
    for (const word of words) {
      const next = line ? `${line} ${word}` : word;
      if (ctx.measureText(next).width > width && line) {
        ctx.fillText(line, x, y + lines * lineHeight);
        lines++;
        if (lines >= maxLines) return;
        line = word;
      } else {
        line = next;
      }
    }
    if (line && lines < maxLines) ctx.fillText(line, x, y + lines * lineHeight);
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
    ctx.fillText('МОЙ ОБРАЗ СЕГОДНЯ', 80, 260);

    ctx.fillStyle = BLUE; ctx.fillRect(80, 310, 920, 450);
    if (record.score != null) {
      ctx.fillStyle = ACID; ctx.beginPath(); ctx.arc(830, 525, 128, 0, Math.PI * 2); ctx.fill();
      ctx.fillStyle = INK; ctx.textAlign = 'center'; ctx.font = '800 100px Unbounded, Arial, sans-serif';
      ctx.fillText(String(record.score), 830, 560);
    }
    ctx.textAlign = 'left'; ctx.fillStyle = '#ffffff'; ctx.font = '800 56px Unbounded, Arial, sans-serif';
    wrap(ctx, record.category.toUpperCase(), 130, 465, 535, 70, 2);
    ctx.font = '700 30px Manrope, Arial, sans-serif';
    wrap(ctx, `Образ: ${record.levelTitle}`, 130, 620, 520, 44, 2);

    ctx.fillStyle = INK; ctx.font = '800 29px Manrope, Arial, sans-serif';
    const metrics = [
      ['ЦЕЛЬНОСТЬ', record.metrics.coherence],
      ['ОРИГИНАЛЬНОСТЬ', record.metrics.originality],
      ['ЦВЕТ', record.metrics.colorHarmony]
    ];
    if (record.score == null) {
      ctx.fillText('ВЕРДИКТ ПО ФОТО', 80, 890);
    } else metrics.forEach(([label, value], index) => {
      const x = 80 + index * 310;
      ctx.strokeStyle = INK; ctx.lineWidth = 3; ctx.strokeRect(x, 810, 290, 160);
      ctx.fillText(label, x + 20, 860);
      ctx.font = '800 65px Unbounded, Arial, sans-serif';
      ctx.fillText(`${value}%`, x + 20, 940);
      ctx.font = '800 29px Manrope, Arial, sans-serif';
    });

    ctx.font = '700 31px Manrope, Arial, sans-serif';
    wrap(ctx, record.summary, 80, 1055, 900, 47, 3);
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
