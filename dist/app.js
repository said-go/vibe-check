const $ = (s) => document.querySelector(s);
const modal = $('#camera-modal');
const video = $('#video');
const canvas = $('#canvas');
const ctx = canvas.getContext('2d', { willReadFrequently: true });
let stream = null;
let currentImage = '';
let currentFile = null;
let currentOriginalHash = '';
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
let backgroundBeforeModal = [];
let overflowBeforeModal = '';
let availabilityGeneration = 0;
let lastPhotoSource = 'file';
let retryTimer = null;
let retryUntil = 0;
let diagnosticsAvailable = false;
const API_BASE = (window.VIBE_API_BASE_URL || '/api').replace(/\/$/, '');
const MAX_UPLOAD_BYTES = 2.5 * 1024 * 1024;
const MAX_DECODED_PIXELS = 100 * 1000 * 1000;
async function decodePhoto(image) {
  let timeout;
  try {
    await Promise.race([
      image.decode(),
      new Promise((_, reject) => { timeout = setTimeout(() => reject(new Error('photo_decode_timeout')), 15000); })
    ]);
  } finally { clearTimeout(timeout); }
}
async function originalHashForFile(file) {
  if (!crypto.subtle) return '';
  const bytes = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
  return [...new Uint8Array(bytes)].map(value => value.toString(16).padStart(2, '0')).join('');
}
const steps = ['capture-step', 'consent-step', 'analyzing-step', 'error-step', 'result-step'];

async function compressedJPEG() {
  const output = document.createElement('canvas');
  for (const scale of [1, .75, .56]) {
    if (Math.min(canvas.width, canvas.height) * scale < 128) continue;
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
  if (!['error-step', 'result-step'].includes(id)) clearDiagnostics();
  steps.forEach(step => { $(`#${step}`).hidden = step !== id; });
  const heading = $(`#${id} h2`);
  modal.setAttribute('aria-labelledby', heading.id);
  heading.focus();
}
function cancelRequest() { requestGeneration++; activeRequest?.abort(); activeRequest = null; clearInterval(retryTimer); retryTimer = null; retryUntil = 0; $('#retry-analysis').disabled = false; $('#retry-analysis').textContent = 'Попробовать ещё раз'; }
function clearPhoto() { photoGeneration++; currentFile = null; currentOriginalHash = ''; currentImage = ''; analysisKey = ''; currentRecord = null; currentCardBlob = null; clearDiagnostics(); $('#diagnostics-consent').checked = false; $('#consent-image').removeAttribute('src'); $('#analysis-image').removeAttribute('src'); $('#result-image').removeAttribute('src'); canvas.width = 0; canvas.height = 0; }

function openModal(withCamera = true, returnFocus = document.activeElement) {
  focusBeforeModal = returnFocus;
  if (modal.hidden) {
    overflowBeforeModal = document.body.style.overflow;
    backgroundBeforeModal = ['header', 'main', 'footer'].map(selector => $(selector)).filter(Boolean).map(element => [element, element.inert]);
    backgroundBeforeModal.forEach(([element]) => { element.inert = true; });
  }
  cancelRequest(); clearPhoto();
  modal.hidden = false;
  showPendingUpdate();
  document.body.style.overflow = 'hidden';
  resetFlow();
  if (withCamera) startCamera();
}
function closeModal() {
  cancelRequest(); clearPhoto(); modal.hidden = true; document.body.style.overflow = overflowBeforeModal; stopCamera();
  backgroundBeforeModal.forEach(([element, wasInert]) => { element.inert = wasInert; });
  backgroundBeforeModal = [];
  if (focusBeforeModal?.isConnected && !focusBeforeModal.disabled) focusBeforeModal.focus();
  else $('#open-camera').focus();
  focusBeforeModal = null;
  showPendingUpdate();
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
  lastPhotoSource = 'camera';
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
  lastPhotoSource = 'file';
  cancelRequest(); clearPhoto(); stopCamera();
  if (file.size === 0) {
    showError('Этот файл пустой. Выбери другое фото.', false); return;
  }
  if (file.size > 25 * 1024 * 1024) {
    showError('Фото слишком большое. Выбери файл до 25 МБ.', false); return;
  }
  const generation = photoGeneration;
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await decodePhoto(image);
    if (generation !== photoGeneration || modal.hidden) return;
    if (!Number.isFinite(image.naturalWidth * image.naturalHeight) || image.naturalWidth * image.naturalHeight > MAX_DECODED_PIXELS) {
      showError('У фото слишком большое разрешение. Уменьши его до 100 мегапикселей или выбери другой кадр.', false); return;
    }
    if (Math.min(image.naturalWidth, image.naturalHeight) < 128) {
      showError('Фото слишком маленькое. Нужен кадр хотя бы 128×128 пикселей.', false); return;
    }
    const scale = Math.min(1, 1600 / Math.max(image.naturalWidth, image.naturalHeight));
    canvas.width = Math.round(image.naturalWidth * scale);
    canvas.height = Math.round(image.naturalHeight * scale);
    if (Math.min(canvas.width, canvas.height) < 128) {
      showError('Фото слишком вытянутое. Обрежь лишние края, чтобы человек занимал больше кадра.', false); return;
    }
    ctx.fillStyle = '#f5f1ea';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
    const blob = await compressedJPEG();
    if (generation !== photoGeneration || modal.hidden) return;
    if (!blob) {
      showError('Не удалось достаточно сжать фото. Попробуй другой кадр.', false); return;
    }
    const originalHash = await originalHashForFile(file).catch(() => '');
    const src = await blobDataURL(blob);
    if (generation !== photoGeneration || modal.hidden) return;
    previewPhoto(new File([blob], 'photo.jpg', { type: 'image/jpeg' }), src, originalHash);
  } catch (error) {
    if (generation === photoGeneration && !modal.hidden) {
      const isHEIC = /image\/hei[cf]/i.test(file.type || '') || /\.hei[cf]$/i.test(file.name || '');
      const message = error?.message === 'photo_decode_timeout'
        ? 'Фото слишком долго открывается. Сохрани его в JPEG или выбери другой кадр.'
        : isHEIC ? 'Этот браузер не смог открыть HEIC. Сохрани фото в JPEG или выбери снимок в другом формате.'
        : 'Браузер не смог открыть это фото. Попробуй JPEG, PNG или WebP.';
      showError(message, false);
    }
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
function previewPhoto(file, src, originalHash = '') {
  stopCamera(); cancelRequest(); currentFile = file; currentOriginalHash = originalHash; currentImage = src; analysisKey = crypto.randomUUID();
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
  single_person_required: 'На фото должен быть один человек. Выбери кадр, где он хорошо виден.',
  image_quality_too_low: 'Качество кадра недостаточно для анализа. Сделай более чёткое фото при хорошем освещении.',
  outfit_visibility_too_low: 'Пока мало деталей для вердикта. Попробуй кадр, где виден хотя бы верх одежды — полный рост не нужен.',
  insufficient_style_evidence: 'Фото загрузилось, но заметных деталей для категории пока мало. Попробуй другой ракурс — чтобы лучше были видны одежда или жест.',
  ai_confidence_too_low: 'AI не смог уверенно разобрать детали на этом фото. Попробуй другой ракурс и ровный свет.'
};
function showError(message, retryable = true, retryAfter = null) {
  clearInterval(retryTimer);
  retryUntil = 0;
  $('#error-title').textContent = 'НЕ ПОЛУЧИЛОСЬ';
  $('#error-description').textContent = message;
  $('#retry-analysis').hidden = !retryable || !currentFile;
  $('#retry-analysis').disabled = false;
  $('#retry-analysis').textContent = 'Попробовать ещё раз';
  if (retryable && retryAfter) {
    const seconds = /^\d+$/.test(retryAfter) ? Number(retryAfter) : Math.ceil((Date.parse(retryAfter) - Date.now()) / 1000);
    if (Number.isFinite(seconds) && seconds > 0) {
      retryUntil = Date.now() + Math.min(seconds, 86400) * 1000;
      const tick = () => {
        const left = Math.max(0, Math.ceil((retryUntil - Date.now()) / 1000));
        $('#retry-analysis').disabled = left > 0;
        $('#retry-analysis').textContent = left ? `Повторить через ${left} с` : 'Попробовать ещё раз';
        if (!left) { clearInterval(retryTimer); retryTimer = null; }
      };
      tick(); retryTimer = setInterval(tick, 1000);
    }
  }
  showStep('error-step');
}
async function analysisReady(signal) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener('abort', abort, { once: true });
  if (signal?.aborted) abort();
  const timeout = setTimeout(abort, 15000);
  try {
    const response = await fetch(`${API_BASE}/readyz?check=${Date.now()}`, {
      headers: { Accept: 'application/json' }, cache: 'no-store', signal: controller.signal
    });
    if (!response.ok || !response.headers.get('Content-Type')?.toLowerCase().includes('application/json')) return false;
    const data = await response.json();
    return data.status === 'ready' ? data : false;
  } catch {
    return false;
  } finally {
    clearTimeout(timeout);
    signal?.removeEventListener('abort', abort);
  }
}
function setDiagnosticsAvailability(ready) {
  diagnosticsAvailable = ready?.diagnostics_available === true;
  $('#diagnostics-option').hidden = !diagnosticsAvailable;
  if (!diagnosticsAvailable) $('#diagnostics-consent').checked = false;
}
function clearDiagnostics() {
  $('#local-diagnostics').hidden = true;
  $('#local-diagnostics').open = false;
  $('#diagnostics-content').replaceChildren();
}
const diagnosticScores = {
  color_harmony: 'Сочетаемость цветов', darkness: 'Доля тёмных тонов', formality: 'Формальность одежды',
  sportiness: 'Спортивные детали', originality: 'Необычность образа', outfit_coherence: 'Согласованность одежды',
  accessory_intensity: 'Выраженность аксессуаров', layering: 'Многослойность', traditional_elements: 'Традиционные детали одежды',
  image_quality: 'Качество снимка', outfit_visibility: 'Видимость одежды', confidence: 'Уверенность распознавания'
};
const diagnosticCues = {
  skullcap_visible: 'Тюбетейка', beard_visible: 'Борода', unnatural_hair_color_visible: 'Неестественный цвет окрашенных волос',
  piercing_visible: 'Пирсинг', hair_below_ears_visible: 'Волосы ниже ушей', hair_over_eyes_visible: 'Волосы закрывают глаза',
  playful_grimace_visible: 'Намеренная гримаса', serious_index_pose_visible: 'Указательный палец у лица и серьёзное выражение',
  gentle_smile_visible: 'Небольшая расслабленная улыбка', relaxed_head_tilt_visible: 'Наклон головы и расслабленные брови',
  beard_absent_visible: 'Видимое отсутствие бороды', muscular_visible: 'Выраженная мускулатура',
  non_muscular_visible: 'Видимое телосложение без выраженной мускулатуры', short_hair_or_bald_visible: 'Короткая стрижка или лысина',
  head_scar_visible: 'Видимый след, похожий на шрам на голове или лице'
};
const diagnosticSignals = {
  goth: 'Готические детали', alternative: 'Альтернативный стиль', streetwear: 'Уличный стиль',
  classic: 'Классический стиль', sport_chic: 'Спорт-шик', retro_90s: 'Мода 1990-х'
};
function renderDiagnostics(data) {
  clearDiagnostics();
  if (!data || typeof data !== 'object') return;
  const container = $('#diagnostics-content');
  const line = (parent, tag, value) => {
    const item = document.createElement(tag);
    item.textContent = String(value || '').slice(0, tag === 'pre' ? 10000 : 1000);
    parent.append(item); return item;
  };
  if (data.available !== true) {
    line(container, 'p', data.source === 'preset'
      ? 'Для этого снимка использован заранее заданный ответ. Распознавание AI не запускалось.'
      : 'Этот результат получен без диагностики. Чтобы проверить признаки, выбери фото заново и включи диагностику перед анализом. Новый анализ может расходовать лимит AI.');
    $('#local-diagnostics').hidden = false; return;
  }
  const selected = Array.isArray(data.selected) ? data.selected : [];
  const isSelected = category => selected.some(c => c && (typeof category.code === 'string' && typeof c.code === 'string'
    ? c.code === category.code : c.title === category.title));
  line(container, 'h3', 'Выбранные категории');
  line(container, 'p', selected.map(c => c?.title).filter(t => typeof t === 'string').join(' + ') || 'Категория не выбрана');
  line(container, 'p', 'Одних баллов недостаточно: учитываются сочетания признаков, приоритеты и качество снимка. Здесь показаны не все этапы выбора.');
  if (data.provisional) line(container, 'p', data.assessment_note || 'Предварительный результат');
  if (data.low_confidence_reason) line(container, 'p', `Причина недостаточной уверенности: ${lowConfidenceMessages[data.low_confidence_reason] || 'Сервер не передал понятное описание причины.'}`);
  line(container, 'h3', 'Соответствие категориям');
  const candidates = line(container, 'ul', '');
  for (const c of (Array.isArray(data.candidates) ? data.candidates : []).slice(0, 20)) {
    if (typeof c?.title === 'string' && Number.isFinite(c.score)) line(candidates, 'li', `${c.title}: ${c.score}/100 — ${isSelected(c) ? 'выбрана' : 'не выбрана'}`);
  }
  line(container, 'p', 'Баллы показывают соответствие категории, а не вероятность и не оценку характера.');
  line(container, 'h3', 'Условия специальных категорий');
  line(container, 'p', '«Подтверждено» — признак распознан на этом снимке. «Не подтверждено» — он не распознан или неразличим; это не доказывает его отсутствие. Проверки описывают только видимые детали, выражение лица и позу, а не характер человека.');
  for (const check of (Array.isArray(data.special_checks) ? data.special_checks : []).slice(0, 20)) {
    if (typeof check?.title !== 'string') continue;
    const status = check.code === 'alternative_combination'
      ? check.eligible === true ? 'бонус применён' : check.eligible === false ? 'бонус не применён' : 'нет результата проверки бонуса'
      : `${check.eligible === true ? 'все перечисленные условия выполнены' : check.eligible === false ? 'не все перечисленные условия выполнены' : 'нет результата проверки'}; ${isSelected(check) ? 'выбрана' : 'не указана среди выбранных'}`;
    line(container, 'h4', `${check.title}: ${status}`);
    const list = line(container, 'ul', '');
    for (const requirement of (Array.isArray(check.requirements) ? check.requirements : []).slice(0, 12)) {
      if (typeof requirement?.label !== 'string') continue;
      const label = diagnosticCues[requirement.feature] || (requirement.feature === 'muscular_visible_false'
        ? 'Распознавание выраженной мускулатуры не сработало (это не доказательство её отсутствия)' : requirement.label);
      line(list, 'li', `${requirement.met === true ? 'Подтверждено' : requirement.met === false ? 'Не подтверждено' : 'Нет данных'} — ${label}`);
    }
  }
  const features = data.features;
  if (features && typeof features === 'object') {
    line(container, 'h3', 'Распознанные признаки после проверки сервером');
    line(container, 'p', 'Числа от 0 до 100 показывают выраженность признака по оценке модели. Уверенность и качество снимка — отдельные оценки, а не гарантия точности.');
    const values = line(container, 'ul', '');
    for (const [key, label] of Object.entries(diagnosticScores)) {
      if (Number.isFinite(features[key])) line(values, 'li', `${label}: ${features[key]}/100`);
    }
    if (Number.isFinite(features.person_count)) line(values, 'li', `Количество людей: ${features.person_count}`);
    for (const [key, label] of Object.entries(diagnosticCues)) {
      if (typeof features[key] === 'boolean') line(values, 'li', `${label}: ${features[key] ? 'Подтверждено' : 'Не подтверждено'}`);
    }
    line(container, 'h4', 'Детали стилей');
    const signals = line(container, 'ul', '');
    for (const [key, label] of Object.entries(diagnosticSignals)) {
      if (Number.isFinite(features.subculture_signals?.[key])) line(signals, 'li', `${label}: ${features.subculture_signals[key]}/100`);
    }
    line(container, 'p', 'Названия цветов не выводятся: сервер исключает свободный текст модели из диагностики.');
  }
  $('#local-diagnostics').hidden = false;
}
async function refreshAvailability() {
  const generation = ++availabilityGeneration;
  const status = $('#api-status');
  status.classList.remove('available', 'unavailable');
  if (!navigator.onLine) {
    setDiagnosticsAvailability(null);
    status.textContent = 'Нет сети — новый AI-анализ недоступен.';
    status.classList.add('unavailable');
    return;
  }
  status.textContent = 'Проверяем доступность AI-анализа…';
  const ready = await analysisReady();
  if (generation !== availabilityGeneration) return;
  setDiagnosticsAvailability(ready);
  status.textContent = ready ? (ready.ai_available === false ? 'Сервер доступен. AI-анализ временно недоступен; часть результатов может быть доступна.' : 'Сервер AI-анализа подключён.') : 'AI-анализ пока не подключён. Интерфейс доступен для просмотра.';
  status.classList.add(ready ? 'available' : 'unavailable');
}
async function analyze() {
  if (!currentFile || !$('#privacy-consent').checked) return;
  if (activeRequest || Date.now() < retryUntil) return;
  if (!navigator.onLine) { showError('Для нового AI-анализа нужен интернет.', true); return; }
  cancelRequest(); const generation = requestGeneration; const controller = new AbortController(); activeRequest = controller;
  clearDiagnostics();
  let timedOut = false;
  const deadline = setTimeout(() => { timedOut = true; controller.abort(); }, 125000);
  showStep('analyzing-step'); $('#analysis-image').src = currentImage;
  $('#analysis-status').textContent = 'Проверяем сервер анализа'; $('#progress-bar').style.width = '15%';
  try {
    const ready = await analysisReady(controller.signal);
    if (generation !== requestGeneration || modal.hidden) return;
    if (timedOut) throw new Error('analysis_timeout');
    setDiagnosticsAvailability(ready);
    if (!ready) {
      showError('AI-анализ пока не подключён. Фото не отправлено — попробуй позже.', true);
      refreshAvailability();
      return;
    }
    $('#analysis-status').textContent = 'AI анализирует видимые детали образа'; $('#progress-bar').style.width = '65%';
    const payload = new FormData(); payload.append('image', currentFile); payload.append('privacy_consent', 'true');
    const headers = { 'Idempotency-Key': analysisKey };
    const wantDiagnostics = diagnosticsAvailable && $('#diagnostics-consent').checked;
    if (wantDiagnostics) headers['X-Vibe-Diagnostics'] = '1';
    if (currentOriginalHash) headers['X-Original-Image-SHA256'] = currentOriginalHash;
    const response = await fetch(`${API_BASE}/v1/analyses`, { method: 'POST', body: payload, headers, signal: controller.signal });
    const data = await response.json().catch(() => ({}));
    if (generation !== requestGeneration || modal.hidden) return;
    if (!response.ok) {
      const error = data.error || data;
      const unavailable = [404, 502, 503, 504].includes(response.status);
      const message = response.status === 429 ? 'Слишком много запросов подряд. Подожди немного и попробуй снова.' : errorMessages[error.code] || (unavailable ? 'Сервер AI-анализа сейчас недоступен. Попробуй позже.' : 'Не удалось завершить анализ. Попробуй ещё раз.');
      showError(message, response.status === 429 || unavailable || !!error.retryable, response.headers.get('Retry-After'));
      return;
    }
    if (!data || !data.result || !['completed', 'low_confidence'].includes(data.status)) throw new Error('invalid_response');
    $('#progress-bar').style.width = '100%';
    if (data.status === 'low_confidence') {
      if (wantDiagnostics) renderDiagnostics(data.diagnostics);
      showError(lowConfidenceMessages[data.result.low_confidence_reason] || 'Не удалось уверенно определить образ. Попробуй другое фото при хорошем освещении.', false);
      return;
    }
    const result = data.result;
    const percentage = value => Number.isFinite(value) && value >= 0 && value <= 100;
    if (typeof data.analysis_id !== 'string' || !data.analysis_id ||
        typeof result.primary_category?.title !== 'string' || !result.primary_category.title ||
        typeof result.summary !== 'string' ||
        (result.provisional !== undefined && typeof result.provisional !== 'boolean') ||
        (result.assessment_note !== undefined && typeof result.assessment_note !== 'string') ||
        (result.assessment_scope !== undefined && !['outfit', 'portrait'].includes(result.assessment_scope)) ||
        (result.explanations !== undefined && (!Array.isArray(result.explanations) || result.explanations.length > 3 || !result.explanations.every(text => typeof text === 'string' && text.length <= 300))) ||
        !percentage(result.score) || !['coherence', 'originality', 'color_harmony'].every(key => percentage(result.metrics?.[key]))) {
      throw new Error('invalid_response');
    }
    showResult(result, data.analysis_id);
    if (wantDiagnostics) renderDiagnostics(data.diagnostics);
  } catch (error) {
    if (generation === requestGeneration && !modal.hidden) clearDiagnostics();
    if (timedOut && generation === requestGeneration && !modal.hidden) {
      showError('Анализ занял слишком много времени. Попробуй повторить запрос.', true);
      return;
    }
    if (error.name !== 'AbortError' && generation === requestGeneration && !modal.hidden) {
      showError(error.message === 'invalid_response' ? 'Сервер AI-анализа пока не настроен или вернул неверный ответ.' : 'Нет связи с сервером анализа. Проверь интернет и попробуй ещё раз.', true);
    }
  } finally { clearTimeout(deadline); if (generation === requestGeneration) activeRequest = null; }
}
function showResult(result, analysisId) {
  currentRecord = window.VIBE_HISTORY.prepare(analysisId, result);
  currentCardBlob = null;
  const record = currentRecord;
  window.VIBE_SHARE_CARD.create(record).then(blob => {
    if (currentRecord === record) currentCardBlob = blob;
  }).catch(() => {});
  $('#save-result').disabled = false;
  $('#save-result').textContent = 'Сохранить в историю';
  $('#download-card').disabled = false;
  $('#share').disabled = false;
  $('#share-note').textContent = '';
  $('#result-image').src = currentImage;
  const portraitOnly = result.assessment_scope === 'portrait';
  $('#result-score').hidden = portraitOnly;
  $('#result-score').textContent = result.score;
  $('#result-title').textContent = result.primary_category.title;
  $('#result-level').textContent = `Оценка одежды: ${result.score}/100`;
  $('#result-level').hidden = portraitOnly;
  $('#result-description').textContent = result.summary;
  $('#result-status').textContent = result.provisional ? 'предварительный вердикт' : 'вердикт готов';
  $('#result-assessment-note').textContent = result.provisional ? (result.assessment_note || 'По этому кадру оценка пока предварительная.') : '';
  $('#result-assessment-note').hidden = !result.provisional;
  const explanations = Array.isArray(result.explanations) ? result.explanations.filter(text => typeof text === 'string' && text.trim()).slice(0, 3) : [];
  $('#result-reasons').replaceChildren(...explanations.map(text => {
    const item = document.createElement('li');
    item.textContent = text;
    return item;
  }));
  $('#result-explanation').hidden = !explanations.length;
  $('#result-stats').hidden = portraitOnly;
  $('#style-stat').textContent = `${result.metrics.coherence}%`;
  $('#energy-stat').textContent = `${result.metrics.originality}%`;
  $('#aura-stat').textContent = `${result.metrics.color_harmony}%`;
  $('#result-recommendations').textContent = Array.isArray(result.recommendations) ? result.recommendations.join(' · ') : '';
  showStep('result-step');
}

function resultText(record) {
  const text = record.score == null
    ? `Мой результат в ВАЙБЧЕК: ${record.category}. ${record.summary}`
    : `Мой результат в ВАЙБЧЕК: ${record.category}. Оценка одежды: ${record.score}/100. А ты кто сегодня?`;
  return record.provisional ? `Предварительный вердикт. ${text}` : text;
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
  score.textContent = record.score == null ? '' : `Оценка одежды: ${record.score}/100`;
  top.append(date);
  if (record.score != null) top.append(score);
  const title = document.createElement('h3');
  title.textContent = record.category;
  const note = document.createElement('p');
  note.className = 'assessment-note';
  note.textContent = record.provisional ? `Предварительный вердикт. ${record.assessmentNote || ''}` : '';
  note.hidden = !record.provisional;
  const actions = document.createElement('div');
  actions.className = 'history-actions';
  for (const [label, action] of [['Скачать карточку', 'download'], ['Удалить', 'delete']]) {
    const button = document.createElement('button');
    button.type = 'button'; button.textContent = label;
    button.dataset.action = action; button.dataset.id = record.id;
    actions.append(button);
  }
  card.append(top, title, note, actions);
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
  const record = currentRecord;
  const button = $('#save-result');
  button.disabled = true;
  try {
    await window.VIBE_HISTORY.save(record.id, {
      score: record.score, level_title: record.levelTitle,
      primary_category: { title: record.category }, summary: record.summary,
      metrics: record.metrics ? { coherence: record.metrics.coherence, originality: record.metrics.originality, color_harmony: record.metrics.colorHarmony } : null,
      recommendations: record.recommendations,
      explanations: record.explanations, provisional: record.provisional,
      assessment_note: record.assessmentNote, assessment_scope: record.assessmentScope
    });
    await renderHistory();
    if (currentRecord !== record || modal.hidden) return;
    button.textContent = 'Сохранено';
    $('#share-note').textContent = 'Результат сохранён на этом устройстве без фото.';
  } catch {
    if (currentRecord !== record || modal.hidden) return;
    button.disabled = false;
    $('#share-note').textContent = 'Не удалось сохранить результат на этом устройстве.';
  }
});

$('#download-card').addEventListener('click', async () => {
  if (!currentRecord) return;
  const record = currentRecord;
  const button = $('#download-card');
  button.disabled = true;
  try {
    const blob = currentCardBlob || await window.VIBE_SHARE_CARD.create(record);
    if (currentRecord !== record || modal.hidden) return;
    window.VIBE_SHARE_CARD.download(blob);
    $('#share-note').textContent = 'Карточка PNG скачана без фотографии.';
  } catch {
    if (currentRecord !== record || modal.hidden) return;
    $('#share-note').textContent = 'Не удалось собрать карточку. Попробуй ещё раз.';
  } finally { if (currentRecord === record && !modal.hidden) button.disabled = false; }
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
function chooseAgain() { cancelRequest(); clearPhoto(); resetFlow(); if (lastPhotoSource === 'camera') startCamera(); else { stopCamera(); $('#modal-file').click(); } }
$('#choose-again').addEventListener('click', chooseAgain);
$('#error-choose-again').addEventListener('click', chooseAgain);
$('#again').addEventListener('click', chooseAgain);
$('#share').addEventListener('click', async () => {
  if (!currentRecord) return;
  const record = currentRecord;
  const button = $('#share');
  button.disabled = true;
  try {
    const text = resultText(record);
    const file = currentCardBlob && new File([currentCardBlob], 'vibe-check.png', { type: 'image/png' });
    if (file && navigator.canShare?.({ files: [file] })) {
      await navigator.share({ title: 'Мой вайбчек', text, files: [file] });
    } else if (navigator.share) {
      await navigator.share({ title: 'Мой вайбчек', text });
    } else {
      await navigator.clipboard.writeText(text);
      if (currentRecord !== record || modal.hidden) return;
      $('#share-note').textContent = 'Текст результата скопирован. Карточку можно скачать отдельно.';
    }
  } catch (error) {
    if (currentRecord !== record || modal.hidden) return;
    if (error.name !== 'AbortError') $('#share-note').textContent = 'Не получилось поделиться. Скачай карточку отдельно.';
  } finally { if (currentRecord === record && !modal.hidden) button.disabled = false; }
});
modal.addEventListener('click', e => { if(e.target.matches('[data-close]')) closeModal(); });
document.addEventListener('keydown', e => { if(e.key==='Escape' && !modal.hidden) closeModal(); });
modal.addEventListener('keydown', event => {
  if (event.key !== 'Tab') return;
  const focusable = [...modal.querySelectorAll('button:not([disabled]):not([hidden]), input:not([disabled]):not([hidden]), summary')]
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

let pendingServiceWorker = null;
let updateRequested = false;
let updateReadyToReload = false;
function showPendingUpdate() {
  $('#update-banner').hidden = (!pendingServiceWorker && !updateReadyToReload) || !modal.hidden;
}
$('#update-app').addEventListener('click', () => {
  if (!modal.hidden) return;
  if (updateReadyToReload) { window.location.reload(); return; }
  if (!pendingServiceWorker) return;
  updateRequested = true;
  $('#update-banner').hidden = true;
  pendingServiceWorker.postMessage({ type: 'SKIP_WAITING' });
});
if ('serviceWorker' in navigator) {
  const hadServiceWorkerController = Boolean(navigator.serviceWorker.controller);
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (!hadServiceWorkerController && !updateRequested) return;
    if (updateRequested && modal.hidden) window.location.reload();
    else { updateReadyToReload = true; showPendingUpdate(); }
  });
  window.addEventListener('load', async () => {
    try {
      const registration = await navigator.serviceWorker.register('/sw.js');
      const offerUpdate = worker => {
        if (!worker || !navigator.serviceWorker.controller) return;
        pendingServiceWorker = worker;
        showPendingUpdate();
      };
      offerUpdate(registration.waiting);
      registration.addEventListener('updatefound', () => {
        const worker = registration.installing;
        worker?.addEventListener('statechange', () => {
          if (worker.state === 'installed') offerUpdate(worker);
        });
      });
      window.addEventListener('focus', () => registration.update().catch(() => {}));
    } catch {}
  });
}
