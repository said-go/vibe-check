const $ = (s) => document.querySelector(s);
const modal = $('#camera-modal');
const video = $('#video');
const canvas = $('#canvas');
const ctx = canvas.getContext('2d', { willReadFrequently: true });
let stream = null;
let currentImage = '';
let currentFile = null;
let analysisKey = '';
let activeRequest = null;
let requestGeneration = 0;
let cameraGeneration = 0;
let photoGeneration = 0;
let deferredInstallPrompt = null;
let focusBeforeModal = null;
let availabilityGeneration = 0;
const API_BASE = (window.VIBE_API_BASE_URL || '/api').replace(/\/$/, '');
const steps = ['capture-step', 'consent-step', 'analyzing-step', 'error-step', 'result-step'];

function showStep(id) {
  steps.forEach(step => { $(`#${step}`).hidden = step !== id; });
  const heading = $(`#${id} h2`);
  modal.setAttribute('aria-labelledby', heading.id);
  heading.focus();
}
function cancelRequest() { requestGeneration++; activeRequest?.abort(); activeRequest = null; }
function clearPhoto() { photoGeneration++; currentFile = null; currentImage = ''; analysisKey = ''; $('#consent-image').removeAttribute('src'); $('#analysis-image').removeAttribute('src'); $('#result-image').removeAttribute('src'); }

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
  stream = null; video.srcObject = null;
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
    video.srcObject = stream; $('#camera-empty').hidden = true; $('#capture').disabled = false;
  } catch (e) {
    if (generation !== cameraGeneration || modal.hidden) return;
    $('#camera-empty').hidden = false; $('#capture').disabled = true;
    $('#camera-error').textContent = 'Камера недоступна — можно выбрать фото с устройства.';
  }
}

function useFile(file) {
  if (!file) return;
  if (!['image/jpeg', 'image/png'].includes(file.type) || file.size > 8 * 1024 * 1024) {
    showError('Нужен JPEG или PNG размером до 8 МБ.', false); return;
  }
  const generation = ++photoGeneration;
  const reader = new FileReader();
  reader.onload = () => { if (generation === photoGeneration && !modal.hidden) previewPhoto(file, reader.result); };
  reader.onerror = () => { if (generation === photoGeneration && !modal.hidden) showError('Не удалось прочитать фото. Выбери другой файл.', false); };
  reader.readAsDataURL(file);
}
function capture() {
  if (!video.videoWidth || !video.videoHeight) return;
  $('#capture').disabled = true;
  const generation = ++photoGeneration;
  canvas.width = video.videoWidth; canvas.height = video.videoHeight;
  ctx.save(); ctx.translate(canvas.width, 0); ctx.scale(-1, 1); ctx.drawImage(video, 0, 0, canvas.width, canvas.height); ctx.restore();
  canvas.toBlob(blob => {
    if (generation !== photoGeneration || modal.hidden) return;
    if (!blob) {
      $('#capture').disabled = false;
      $('#camera-error').textContent = 'Не удалось сохранить кадр. Попробуй ещё раз.';
      return;
    }
    previewPhoto(new File([blob], 'camera.jpg', { type: 'image/jpeg' }), canvas.toDataURL('image/jpeg', .85));
  }, 'image/jpeg', .85);
}
function previewPhoto(file, src) {
  stopCamera(); cancelRequest(); currentFile = file; currentImage = src; analysisKey = crypto.randomUUID();
  $('#consent-image').src = src; $('#privacy-consent').checked = false; $('#start-analysis').disabled = true;
  $('#consent-error').textContent = ''; showStep('consent-step');
}
const errorMessages = {
  privacy_consent_required: 'Подтверди согласие на отправку фото для анализа.',
  unsupported_image_type: 'Подойдёт только JPEG или PNG. Выбери другое фото.',
  image_dimensions_too_small: 'Фото слишком маленькое. Нужен кадр хотя бы 320×320 пикселей.',
  image_dimensions_too_large: 'Фото слишком большое. Сделай или выбери другой кадр.',
  invalid_image: 'Файл не удалось прочитать как фотографию.',
  image_too_large: 'Фото слишком большое. Выбери файл до 8 МБ.',
  rate_limited: 'Слишком много запросов подряд. Подожди немного и попробуй снова.',
  idempotency_conflict: 'Это уже другое фото. Выбери его заново и повтори анализ.',
  invalid_idempotency_key: 'Не удалось подготовить запрос. Выбери фото заново.',
  ai_rejected_image: 'AI не смог обработать этот кадр. Попробуй другое фото.',
  ai_unavailable: 'AI-сервис сейчас недоступен. Повтори попытку позже.',
  invalid_ai_response: 'Ответ AI не удалось проверить. Попробуй ещё раз.'
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
  const timeout = setTimeout(abort, 5000);
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
    if (data.status === 'low_confidence') { showError('Не удалось уверенно определить образ. Сделай другое фото при хорошем освещении, чтобы одежда была видна целиком.', false); return; }
    showResult(data.result);
  } catch (error) {
    if (error.name !== 'AbortError' && generation === requestGeneration && !modal.hidden) {
      showError(error.message === 'invalid_response' ? 'Сервер AI-анализа пока не настроен или вернул неверный ответ.' : 'Нет связи с сервером анализа. Проверь интернет и попробуй ещё раз.', true);
    }
  } finally { if (generation === requestGeneration) activeRequest = null; }
}
function showResult(result) {
  $('#result-image').src = currentImage;
  $('#result-score').textContent = result.score;
  $('#result-title').textContent = result.level_title;
  $('#result-category').textContent = `Стиль: ${result.primary_category.title}`;
  $('#result-description').textContent = result.summary;
  $('#style-stat').textContent = `${result.metrics.coherence}%`;
  $('#energy-stat').textContent = `${result.metrics.originality}%`;
  $('#aura-stat').textContent = `${result.metrics.color_harmony}%`;
  $('#result-recommendations').textContent = Array.isArray(result.recommendations) ? result.recommendations.join(' · ') : '';
  showStep('result-step');
}

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
  const text=`Мой результат в ВАЙБЧЕК: ${$('#result-title').textContent}, ${$('#result-category').textContent} — ${$('#result-score').textContent}/100. А ты кто сегодня?`;
  try { if (navigator.share) await navigator.share({title:'Мой вайбчек',text}); else { await navigator.clipboard.writeText(text); $('#share-note').textContent='Результат скопирован — отправляй друзьям!'; } } catch(e) { if(e.name!=='AbortError') $('#share-note').textContent='Не получилось поделиться. Попробуй ещё раз.'; }
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
