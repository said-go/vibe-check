const $ = (s) => document.querySelector(s);
const modal = $('#camera-modal');
const video = $('#video');
const canvas = $('#canvas');
const ctx = canvas.getContext('2d', { willReadFrequently: true });
let stream = null;
let currentImage = '';

const verdicts = [
  { min: 0, title: 'НЕФОР', text: 'Ты не следуешь правилам — правила пытаются догнать тебя. Образ на своей волне, и это уже заявление.' },
  { min: 55, title: 'ГОТ', text: 'Драма, глубина и безупречное чувство тёмной эстетики. Даже если на тебе белое — вайб всё равно ночной.' },
  { min: 70, title: 'ЗДРАВЫЙ ПАЦАН', text: 'Надёжный вайб без лишнего шума. Выглядишь так, будто знаешь хорошее место и всегда держишь слово.' },
  { min: 84, title: 'СТИЛЯГА', text: 'Образ собран, энергия заряжена, случайные прохожие уже ищут твои вещи в интернете. Главный герой найден.' }
];

function openModal(withCamera = true) {
  modal.hidden = false;
  document.body.style.overflow = 'hidden';
  resetFlow();
  if (withCamera) startCamera();
}
function closeModal() { modal.hidden = true; document.body.style.overflow = ''; stopCamera(); }
function stopCamera() { if (stream) stream.getTracks().forEach(t => t.stop()); stream = null; video.srcObject = null; }
function resetFlow() { $('#capture-step').hidden = false; $('#analyzing-step').hidden = true; $('#result-step').hidden = true; $('#share-note').textContent = ''; }

async function startCamera() {
  $('#camera-error').textContent = '';
  try {
    stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'user', width: { ideal: 1280 } }, audio: false });
    video.srcObject = stream; $('#camera-empty').hidden = true; $('#capture').disabled = false;
  } catch (e) {
    $('#camera-empty').hidden = false; $('#capture').disabled = true;
    $('#camera-error').textContent = 'Камера недоступна — можно выбрать фото с устройства.';
  }
}

function useFile(file) {
  if (!file || !file.type.startsWith('image/')) return;
  const reader = new FileReader();
  reader.onload = () => analyze(reader.result); reader.readAsDataURL(file);
}
function capture() {
  canvas.width = video.videoWidth || 720; canvas.height = video.videoHeight || 720;
  ctx.save(); ctx.translate(canvas.width, 0); ctx.scale(-1, 1); ctx.drawImage(video, 0, 0, canvas.width, canvas.height); ctx.restore();
  analyze(canvas.toDataURL('image/jpeg', .9));
}
function imageScore(img) {
  const size = 80; canvas.width = size; canvas.height = size; ctx.drawImage(img, 0, 0, size, size);
  const data = ctx.getImageData(0, 0, size, size).data; let brightness = 0, color = 0, contrast = 0;
  const samples = [];
  for (let i=0;i<data.length;i+=4){ const b=(data[i]+data[i+1]+data[i+2])/3; brightness+=b; color+=Math.max(data[i],data[i+1],data[i+2])-Math.min(data[i],data[i+1],data[i+2]); samples.push(b); }
  brightness/=samples.length; color/=samples.length;
  for(const b of samples) contrast += Math.abs(b-brightness); contrast/=samples.length;
  return Math.max(42, Math.min(99, Math.round(48 + color*.28 + contrast*.36 + brightness*.055)));
}
function analyze(src) {
  stopCamera(); currentImage = src; $('#capture-step').hidden = true; $('#analyzing-step').hidden = false; $('#analysis-image').src = src;
  const statuses = ['Считываем цветовой код', 'Проверяем силу образа', 'Замеряем уровень ауры', 'Сверяемся с модным радаром'];
  let p = 12, i = 0; $('#progress-bar').style.width = p+'%';
  const timer = setInterval(() => { p += 21 + Math.random()*13; i++; $('#analysis-status').textContent = statuses[Math.min(i, statuses.length-1)]; $('#progress-bar').style.width = Math.min(96,p)+'%'; }, 430);
  const img = new Image(); img.onload = () => setTimeout(() => { clearInterval(timer); $('#progress-bar').style.width='100%'; showResult(imageScore(img)); }, 1900); img.src = src;
}
function showResult(score) {
  const verdict = [...verdicts].reverse().find(v => score >= v.min);
  const style = score, energy = Math.max(44, Math.min(99, score + Math.round(Math.random()*18-7))), aura = Math.max(48, Math.min(99, score + Math.round(Math.random()*15-4)));
  $('#result-image').src=currentImage; $('#result-score').textContent=score; $('#result-title').textContent=verdict.title; $('#result-description').textContent=verdict.text;
  $('#style-stat').textContent=style+'%'; $('#energy-stat').textContent=energy+'%'; $('#aura-stat').textContent=aura+'%';
  $('#analyzing-step').hidden=true; $('#result-step').hidden=false;
}

$('#open-camera').addEventListener('click', () => openModal(true));
$('#file-input').addEventListener('change', e => { openModal(false); useFile(e.target.files[0]); e.target.value=''; });
$('#modal-file').addEventListener('change', e => { useFile(e.target.files[0]); e.target.value=''; });
$('#capture').addEventListener('click', capture);
$('#again').addEventListener('click', () => { resetFlow(); startCamera(); });
$('#share').addEventListener('click', async () => {
  const text=`Мой результат в ВАЙБЧЕК: ${$('#result-title').textContent} — ${$('#result-score').textContent}/100. А ты кто сегодня?`;
  try { if (navigator.share) await navigator.share({title:'Мой вайбчек',text}); else { await navigator.clipboard.writeText(text); $('#share-note').textContent='Результат скопирован — отправляй друзьям!'; } } catch(e) { if(e.name!=='AbortError') $('#share-note').textContent='Не получилось поделиться. Попробуй ещё раз.'; }
});
modal.addEventListener('click', e => { if(e.target.matches('[data-close]')) closeModal(); });
document.addEventListener('keydown', e => { if(e.key==='Escape' && !modal.hidden) closeModal(); });
