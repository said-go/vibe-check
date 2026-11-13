const $ = (s) => document.querySelector(s);
const modal = $('#camera-modal');
const video = $('#video');
const canvas = $('#canvas');
const ctx = canvas.getContext('2d', { willReadFrequently: true });
let stream = null;
let currentImage = '';
let currentFile = null;
let currentPreset = null;
let analysisKey = '';
let currentRecord = null;
let currentCardBlob = null;
let historyRows = [];
let activeRequest = null;
let requestGeneration = 0;
let cameraGeneration = 0;
let photoGeneration = 0;
let deferredInstallPrompt = null;
let focusBeforeModal = null;
let availabilityGeneration = 0;
const API_BASE = (window.VIBE_API_BASE_URL || '/api').replace(/\/$/, '');
const MAX_UPLOAD_BYTES = 2.5 * 1024 * 1024;
// Only SHA-256 fingerprints and the owner's requested text are included here; no photos are bundled.
const PHOTO_PRESETS = Object.freeze({
  f1c8dc5f938b95675b54e169d8139c4166f61e2b1fe807114fcfd44ba64d7624: {
    title: 'Чушпан', summary: 'Самому не стыдно было присылать такое фото!'
  },
  '4b4941958438cac8fb68040995e00f70172f527b6db0c75ecae818f7c6238173': {
    title: 'Пупсик', summary: 'Мягкий человек с доброй душой, но очень наивный'
  }
});
const NORMAL_CONSENT_DESCRIPTION = 'После согласия фото будет отправлено на наш сервер. Он удалит метаданные и передаст очищенную копию в Yandex AI Studio для анализа. Наш сервер не сохраняет фото.';
const NORMAL_CONSENT_LABEL = 'Согласен отправить фото в Yandex AI Studio для анализа';
async function presetForFile(file) {
  if (!crypto.subtle) return null;
  const bytes = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
  const fingerprint = [...new Uint8Array(bytes)].map(value => value.toString(16).padStart(2, '0')).join('');
  return PHOTO_PRESETS[fingerprint] || null;
}
const steps = ['capture-step', 'consent-step', 'analyzing-step', 'error-step', 'result-step'];

async function compressedJPEG() {
  const output = document.createElement('canvas');
  for (const scale of [1, .75, .56]) {
    output.width = Math.max(1, Math.round(canvas.width * scale));
    output.height = Math.max(1, Math.round(canvas.height * scale));
    output.getContext('2d').drawImage(canvas, 0, 0, output.width, output.height);
    for (const quality of [.86, .7, .54, .4]) {
      const blob = await new Promise(resolve => output.toBlob(resolve, 'image/jpeg', quality));
      if (blob?.type === 'image/jpeg' && blob.size <= MAX_UPLOAD_BYTES) return blob;
    }
  }
  return null;
}

function blobDataURL(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

function showStep(id) {
  steps.forEach(step => { $(`#${step}`).hidden = step !== id; });
  const heading = $(`#${id} h2`);
  modal.setAttribute('aria-labelledby', heading.id);
  heading.focus();
}
function cancelRequest() { requestGeneration++; activeRequest?.abort(); activeRequest = null; }
function clearPhoto() { photoGeneration++; currentFile = null; currentPreset = null; currentImage = ''; analysisKey = ''; currentRecord = null; currentCardBlob = null; $('#consent-image').removeAttribute('src'); $('#analysis-image').removeAttribute('src'); $('#result-image').removeAttribute('src'); canvas.width = 0; canvas.height = 0; }

function openModal(withCamera = true, returnFocus = document.activeElement) {
  focusBeforeModal = returnFocus;
  cancelRequest(); clearPhoto();
  modal.hidden = false;
  document.body.style.overflow = 'hidden';
  resetFlow();
  if (withCamera) startCamera();
}
function closeModal() {
  cancelRequest(); clearPhoto(); modal.hidden = true; document.body.style.overflow = ''; stopCamera();
  focusBeforeModal?.focus();
  focusBeforeModal = null;
}
function stopCamera() {
  cameraGeneration++;
  if (stream) stream.getTracks().forEach(t => t.stop());
  stream = null; video.onloadedmetadata = null; video.srcObject = null;
  $('#capture').disabled = true;
  $('#camera-empty').hidden = false;
}
function resetFlow() { showStep('capture-step'); $('#share-note').textContent = ''; $('#privacy-consent').checked = false; $('#start-analysis').disabled = true; $('#consent-error').textContent = ''; }

async function startCamera() {
  stopCamera();
  const generation = cameraGeneration;
  $('#camera-error').textContent = '';
  try {
    const cameraStream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'user', width: { ideal: 1280 } }, audio: false });
    if (generation !== cameraGeneration || modal.hidden) { cameraStream.getTracks().forEach(t => t.stop()); return; }
    stream = cameraStream;
    video.onloadedmetadata = () => {
      if (generation === cameraGeneration && !modal.hidden && video.videoWidth && video.videoHeight) $('#capture').disabled = false;
    };
    video.srcObject = stream; $('#camera-empty').hidden = true;
    if (video.videoWidth && video.videoHeight) $('#capture').disabled = false;
  } catch (e) {
    if (generation !== cameraGeneration || modal.hidden) return;
    $('#camera-empty').hidden = false; $('#capture').disabled = true;
    $('#camera-error').textContent = 'Камера недоступна — можно выбрать фото с устройства.';
  }
}

async function useFile(file) {
  if (!file) return;
  cancelRequest(); clearPhoto(); stopCamera();
  if (file.size > 25 * 1024 * 1024) {
    showError('Фото слишком большое. Выбери файл до 25 МБ.', false); return;
  }
  const generation = photoGeneration;
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    if (generation !== photoGeneration || modal.hidden) return;
    if (Math.min(image.naturalWidth, image.naturalHeight) < 128) {
      showError('Фото слишком маленькое. Нужен кадр хотя бы 128×128 пикселей.', false); return;
    }
    const scale = Math.min(1, 1600 / Math.max(image.naturalWidth, image.naturalHeight));
    canvas.width = Math.round(image.naturalWidth * scale);
    canvas.height = Math.round(image.naturalHeight * scale);
    ctx.fillStyle = '#f5f1ea';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
    const blob = await compressedJPEG();
    if (generation !== photoGeneration || modal.hidden) return;
    if (!blob) {
      showError('Не удалось достаточно сжать фото. Попробуй другой кадр.', false); return;
    }
    const preset = await presetForFile(file).catch(() => null);
    const src = await blobDataURL(blob);
    if (generation !== photoGeneration || modal.hidden) return;
    previewPhoto(new File([blob], 'photo.jpg', { type: 'image/jpeg' }), src, preset);
  } catch {
    if (generation === photoGeneration && !modal.hidden) showError('Браузер не смог открыть это фото. Попробуй JPEG, PNG или WebP.', false);
  } finally {
    URL.revokeObjectURL(url);
  }
}
async function capture() {
  if (!video.videoWidth || !video.videoHeight) return;
  $('#capture').disabled = true;
  const generation = ++photoGeneration;
  const scale = Math.min(1, 1600 / Math.max(video.videoWidth, video.videoHeight));
  canvas.width = Math.round(video.videoWidth * scale); canvas.height = Math.round(video.videoHeight * scale);
  ctx.save(); ctx.translate(canvas.width, 0); ctx.scale(-1, 1); ctx.drawImage(video, 0, 0, canvas.width, canvas.height); ctx.restore();
  try {
    const blob = await compressedJPEG();
    if (generation !== photoGeneration || modal.hidden) return;
    if (!blob) {
      $('#capture').disabled = false;
      $('#camera-error').textContent = 'Не удалось достаточно сжать кадр. Попробуй ещё раз.';
      return;
    }
    const src = await blobDataURL(blob);
    if (generation !== photoGeneration || modal.hidden) return;
    previewPhoto(new File([blob], 'camera.jpg', { type: 'image/jpeg' }), src);
  } catch {
    if (generation === photoGeneration && !modal.hidden) {
      $('#capture').disabled = false;
      $('#camera-error').textContent = 'Не удалось сохранить кадр. Попробуй ещё раз.';
    }
  }
}
function previewPhoto(file, src, preset = null) {
  stopCamera(); cancelRequest(); currentFile = file; currentPreset = preset; currentImage = src; analysisKey = crypto.randomUUID();
  $('#consent-description').textContent = preset ? 'Это фото совпало с заранее заданным примером. Ответ покажем здесь; фото никуда не отправим.' : NORMAL_CONSENT_DESCRIPTION;
  $('#consent-label-text').textContent = preset ? 'Показать заранее заданный ответ' : NORMAL_CONSENT_LABEL;
  $('#start-analysis').textContent = preset ? 'Показать результат' : 'Узнать вайб';
  $('#consent-image').src = src; $('#privacy-consent').checked = false; $('#start-analysis').disabled = true;
  $('#consent-error').textContent = ''; showStep('consent-step');
}
const errorMessages = {
  privacy_consent_required: 'Подтверди согласие на отправку фото для анализа.',
  unsupported_image_type: 'Подойдёт только JPEG или PNG. Выбери другое фото.',
  image_dimensions_too_small: 'Фото слишком маленькое. Нужен кадр хотя бы 128×128 пикселей.',
  image_dimensions_too_large: 'Фото слишком большое. Сделай или выбери другой кадр.',
  invalid_image: 'Файл не удалось прочитать как фотографию.',
  image_too_large: 'Не удалось отправить фото. Выбери файл поменьше или попробуй другой кадр.',
  rate_limited: 'Слишком много запросов подряд. Подожди немного и попробуй снова.',
  idempotency_conflict: 'Это уже другое фото. Выбери его заново и повтори анализ.',
  invalid_idempotency_key: 'Не удалось подготовить запрос. Выбери фото заново.',
  ai_rejected_image: 'AI не смог обработать этот кадр. Попробуй другое фото.',
  ai_unavailable: 'AI-сервис сейчас недоступен. Повтори попытку позже.',
  invalid_ai_response: 'Ответ AI не удалось проверить. Попробуй ещё раз.'
};
const lowConfidenceMessages = {
  single_person_required: 'AI не увидел в кадре ровно одного человека. Сфотографируйся один, чтобы одежда была хорошо видна.',
  image_quality_too_low: 'Качество кадра недостаточно для анализа. Сделай более чёткое фото при хорошем освещении.',
  outfit_visibility_too_low: 'Одежда видна недостаточно хорошо. Отойди от камеры и покажи образ целиком.',
  ai_confidence_too_low: 'AI не смог уверенно распознать детали одежды. Попробуй другой ракурс и ровный свет.'
};
function showError(message, retryable = true) {
  $('#error-title').textContent = 'НЕ ПОЛУЧИЛОСЬ';
  $('#error-description').textContent = message;
  $('#retry-analysis').hidden = !retryable || !currentFile;
  showStep('error-step');
}
async function analysisReady(signal) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener('abort', abort, { once: true });
  const timeout = setTimeout(abort, 15000);
  try {
    const response = await fetch(`${API_BASE}/readyz?check=${Date.now()}`, {
      headers: { Accept: 'application/json' }, cache: 'no-store', signal: controller.signal
    });
    if (!response.ok || !response.headers.get('Content-Type')?.toLowerCase().includes('application/json')) return false;
    return (await response.json()).status === 'ready';
  } catch {
    return false;
  } finally {
    clearTimeout(timeout);
    signal?.removeEventListener('abort', abort);
  }
}
async function refreshAvailability() {
  const generation = ++availabilityGeneration;
  const status = $('#api-status');
  status.classList.remove('available', 'unavailable');
  if (!navigator.onLine) {
    status.textContent = 'Нет сети — новый AI-анализ недоступен.';
    status.classList.add('unavailable');
    return;
  }
  status.textContent = 'Проверяем доступность AI-анализа…';
  const ready = await analysisReady();
  if (generation !== availabilityGeneration) return;
  status.textContent = ready ? 'Сервер AI-анализа подключён.' : 'AI-анализ пока не подключён. Интерфейс доступен для просмотра.';
  status.classList.add(ready ? 'available' : 'unavailable');
}
async function analyze() {
  if (!currentFile || !$('#privacy-consent').checked) return;
  if (currentPreset) {
    const preset = currentPreset;
    showResult({
      status: 'completed', score: null, level_title: 'Особый вердикт',
      primary_category: { title: preset.title }, summary: preset.summary,
      metrics: { coherence: null, originality: null, color_harmony: null }, recommendations: []
    }, analysisKey);
    return;
  }
  if (!navigator.onLine) { showError('Для нового AI-анализа нужен интернет.', true); return; }
  cancelRequest(); const generation = requestGeneration; const controller = new AbortController(); activeRequest = controller;
  showStep('analyzing-step'); $('#analysis-image').src = currentImage;
  $('#analysis-status').textContent = 'Проверяем сервер анализа'; $('#progress-bar').style.width = '15%';
  try {
    const ready = await analysisReady(controller.signal);
    if (generation !== requestGeneration || modal.hidden) return;
    if (!ready) {
      showError('AI-анализ пока не подключён. Фото не отправлено — попробуй позже.', true);
      refreshAvailability();
      return;
    }
    $('#analysis-status').textContent = 'AI анализирует видимые детали образа'; $('#progress-bar').style.width = '65%';
    const payload = new FormData(); payload.append('image', currentFile); payload.append('privacy_consent', 'true');
    const response = await fetch(`${API_BASE}/v1/analyses`, { method: 'POST', body: payload, headers: { 'Idempotency-Key': analysisKey }, signal: controller.signal });
    const data = await response.json().catch(() => ({}));
    if (generation !== requestGeneration || modal.hidden) return;
    if (!response.ok) {
      const error = data.error || data;
      const unavailable = [404, 502, 503, 504].includes(response.status);
      const message = response.status === 429 ? 'Слишком много запросов подряд. Подожди немного и попробуй снова.' : errorMessages[error.code] || (unavailable ? 'Сервер AI-анализа сейчас недоступен. Попробуй позже.' : 'Не удалось завершить анализ. Попробуй ещё раз.');
      showError(message, response.status === 429 || unavailable || !!error.retryable);
      return;
    }
    if (!data.result || !['completed', 'low_confidence'].includes(data.status)) throw new Error('invalid_response');
    $('#progress-bar').style.width = '100%';
    if (data.status === 'low_confidence') {
      showError(lowConfidenceMessages[data.result.low_confidence_reason] || 'Не удалось уверенно определить образ. Попробуй другое фото при хорошем освещении.', false);
      return;
    }
    showResult(data.result, data.analysis_id);
  } catch (error) {
    if (error.name !== 'AbortError' && generation === requestGeneration && !modal.hidden) {
      showError(error.message === 'invalid_response' ? 'Сервер AI-анализа пока не настроен или вернул неверный ответ.' : 'Нет связи с сервером анализа. Проверь интернет и попробуй ещё раз.', true);
    }
  } finally { if (generation === requestGeneration) activeRequest = null; }
}
function showResult(result, analysisId) {
  const preset = result.score == null;
  currentRecord = window.VIBE_HISTORY.prepare(analysisId, result);
  currentCardBlob = null;
  const record = currentRecord;
  window.VIBE_SHARE_CARD.create(record).then(blob => {
    if (currentRecord === record) currentCardBlob = blob;
  }).catch(() => {});
  $('#save-result').disabled = false;
  $('#save-result').textContent = 'Сохранить в историю';
  $('#result-image').src = currentImage;
  $('#result-score').textContent = preset ? '★' : result.score;
  $('#result-title').textContent = preset ? result.primary_category.title : result.level_title;
  $('#result-category').textContent = preset ? 'Особый вердикт' : `Стиль: ${result.primary_category.title}`;
  $('#result-description').textContent = result.summary;
  $('#result-stats').hidden = preset;
  $('#style-stat').textContent = preset ? '—' : `${result.metrics.coherence}%`;
  $('#energy-stat').textContent = preset ? '—' : `${result.metrics.originality}%`;
  $('#aura-stat').textContent = preset ? '—' : `${result.metrics.color_harmony}%`;
  $('#result-recommendations').textContent = preset ? '' : Array.isArray(result.recommendations) ? result.recommendations.join(' · ') : '';
  showStep('result-step');
}

function resultText(record) {
  return record.score == null
    ? `Мой особый вердикт в ВАЙБЧЕК: ${record.category}. ${record.summary}`
    : `Мой результат в ВАЙБЧЕК: ${record.levelTitle}, стиль ${record.category} — ${record.score}/100. А ты кто сегодня?`;
}

function historyCard(record) {
  const card = document.createElement('article');
  card.className = 'history-card';
  const top = document.createElement('div');
  top.className = 'history-card-top';
  const date = document.createElement('time');
  date.dateTime = new Date(record.createdAt).toISOString();
  date.textContent = new Intl.DateTimeFormat('ru-RU', { dateStyle: 'medium' }).format(record.createdAt);
  const score = document.createElement('strong');
  score.textContent = record.score == null ? '★' : `${record.score}/100`;
  top.append(date, score);
  const title = document.createElement('h3');
  title.textContent = record.levelTitle;
  const category = document.createElement('p');
  category.textContent = `Стиль: ${record.category}`;
  const actions = document.createElement('div');
  actions.className = 'history-actions';
  for (const [label, action] of [['Скачать карточку', 'download'], ['Удалить', 'delete']]) {
    const button = document.createElement('button');
    button.type = 'button'; button.textContent = label;
    button.dataset.action = action; button.dataset.id = record.id;
    actions.append(button);
  }
  card.append(top, title, category, actions);
  return card;
}

async function renderHistory() {
  const list = $('#history-list');
  const message = $('#history-message');
  try {
    historyRows = await window.VIBE_HISTORY.list();
    list.replaceChildren(...historyRows.map(historyCard));
    $('#clear-history').hidden = historyRows.length === 0;
    message.textContent = historyRows.length ? `Сохранено результатов: ${historyRows.length}. Фото не сохраняются.` : 'История пока пуста.';
    return true;
  } catch {
    historyRows = [];
    list.replaceChildren();
    $('#clear-history').hidden = true;
    message.textContent = 'Локальная история недоступна в этом браузере.';
    return false;
  }
}

$('#save-result').addEventListener('click', async () => {
  if (!currentRecord) return;
  const button = $('#save-result');
  button.disabled = true;
  try {
    await window.VIBE_HISTORY.save(currentRecord.id, {
      score: currentRecord.score, level_title: currentRecord.levelTitle,
      primary_category: { title: currentRecord.category }, summary: currentRecord.summary,
      metrics: { coherence: currentRecord.metrics.coherence, originality: currentRecord.metrics.originality, color_harmony: currentRecord.metrics.colorHarmony },
      recommendations: currentRecord.recommendations
    });
    await renderHistory();
    button.textContent = 'Сохранено';
    $('#share-note').textContent = 'Результат сохранён на этом устройстве без фото.';
  } catch {
    button.disabled = false;
    $('#share-note').textContent = 'Не удалось сохранить результат на этом устройстве.';
  }
});

$('#download-card').addEventListener('click', async () => {
  if (!currentRecord) return;
  const button = $('#download-card');
  button.disabled = true;
  try {
    window.VIBE_SHARE_CARD.download(currentCardBlob || await window.VIBE_SHARE_CARD.create(currentRecord));
    $('#share-note').textContent = 'Карточка PNG скачана без фотографии.';
  } catch {
    $('#share-note').textContent = 'Не удалось собрать карточку. Попробуй ещё раз.';
  } finally { button.disabled = false; }
});

$('#history-list').addEventListener('click', async event => {
  const button = event.target.closest('button[data-action]');
  if (!button) return;
  const record = historyRows.find(item => item.id === button.dataset.id);
  if (!record) return;
  button.disabled = true;
  try {
    if (button.dataset.action === 'delete') {
      await window.VIBE_HISTORY.remove(record.id);
      await renderHistory();
    } else {
      window.VIBE_SHARE_CARD.download(await window.VIBE_SHARE_CARD.create(record));
      button.disabled = false;
    }
  } catch {
    button.disabled = false;
    $('#history-message').textContent = 'Не удалось выполнить действие с историей.';
  }
});

$('#clear-history').addEventListener('click', async () => {
  if (!window.confirm('Удалить все сохранённые результаты с этого устройства?')) return;
  try {
    await window.VIBE_HISTORY.clear();
    await renderHistory();
  } catch { $('#history-message').textContent = 'Не удалось очистить историю.'; }
});

renderHistory();

$('#open-camera').addEventListener('click', () => openModal(true));
$('#open-file').addEventListener('click', () => $('#file-input').click());
$('#modal-open-file').addEventListener('click', () => $('#modal-file').click());
$('#file-input').addEventListener('change', e => { if (e.target.files[0]) { openModal(false, $('#open-file')); useFile(e.target.files[0]); } e.target.value=''; });
$('#modal-file').addEventListener('change', e => { useFile(e.target.files[0]); e.target.value=''; });
$('#capture').addEventListener('click', capture);
$('#privacy-consent').addEventListener('change', e => { $('#start-analysis').disabled = !e.target.checked; });
$('#start-analysis').addEventListener('click', analyze);
$('#retry-analysis').addEventListener('click', analyze);
function chooseAgain() { cancelRequest(); clearPhoto(); resetFlow(); startCamera(); }
$('#choose-again').addEventListener('click', chooseAgain);
$('#error-choose-again').addEventListener('click', chooseAgain);
$('#again').addEventListener('click', chooseAgain);
$('#share').addEventListener('click', async () => {
  if (!currentRecord) return;
  const button = $('#share');
  button.disabled = true;
  try {
    const text = resultText(currentRecord);
    const file = currentCardBlob && new File([currentCardBlob], 'vibe-check.png', { type: 'image/png' });
    if (file && navigator.canShare?.({ files: [file] })) {
      await navigator.share({ title: 'Мой вайбчек', text, files: [file] });
    } else if (navigator.share) {
      await navigator.share({ title: 'Мой вайбчек', text });
    } else {
      await navigator.clipboard.writeText(text);
      $('#share-note').textContent = 'Текст результата скопирован. Карточку можно скачать отдельно.';
    }
  } catch (error) {
    if (error.name !== 'AbortError') $('#share-note').textContent = 'Не получилось поделиться. Скачай карточку отдельно.';
  } finally { button.disabled = false; }
});
modal.addEventListener('click', e => { if(e.target.matches('[data-close]')) closeModal(); });
document.addEventListener('keydown', e => { if(e.key==='Escape' && !modal.hidden) closeModal(); });
modal.addEventListener('keydown', event => {
  if (event.key !== 'Tab') return;
  const focusable = [...modal.querySelectorAll('button:not([disabled]):not([hidden]), input:not([disabled]):not([hidden])')]
    .filter(element => element.offsetParent !== null);
  if (!focusable.length) return;
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
});

function updateConnectionStatus() {
  const indicator = $('#connection-status');
  indicator.classList.toggle('offline', !navigator.onLine);
  indicator.setAttribute('aria-label', navigator.onLine ? 'Наш сервер не сохраняет фото' : 'Нет подключения к сети');
}
window.addEventListener('online', updateConnectionStatus);
window.addEventListener('offline', updateConnectionStatus);
window.addEventListener('online', refreshAvailability);
window.addEventListener('offline', refreshAvailability);
window.addEventListener('focus', refreshAvailability);
updateConnectionStatus();
refreshAvailability();

window.addEventListener('beforeinstallprompt', event => {
  event.preventDefault();
  deferredInstallPrompt = event;
  $('#install-app').hidden = false;
});
$('#install-app').addEventListener('click', async () => {
  if (!deferredInstallPrompt) return;
  deferredInstallPrompt.prompt();
  await deferredInstallPrompt.userChoice;
  deferredInstallPrompt = null;
  $('#install-app').hidden = true;
});
window.addEventListener('appinstalled', () => {
  deferredInstallPrompt = null;
  $('#install-app').hidden = true;
});

if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => navigator.serviceWorker.register('/sw.js').catch(() => {}));
}
