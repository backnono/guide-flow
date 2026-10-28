const steps = [
  { number: '01', title: '先找出“剩下的页数”', description: '第一天看了全书的 1/3，所以还剩下全书的 2/3。我们先不急着算第二天，先把“剩下的”找出来。', note: '把整本书看成 3 份', parts: ['bar-read','bar-left'], labels: ['第一天：1/3','剩下：2/3'] },
  { number: '02', title: '再算“剩下的 1/4”', description: '全书 120 页，剩下的是 120 × 2/3 = 80 页。第二天看了剩下的 1/4，就是 80 ÷ 4 = 20 页。', note: '80 页平均分成 4 份', parts: ['bar-read','bar-left'], labels: ['已看：60页','剩下：80页'] },
  { number: '03', title: '回头检查，答案是 20 页', description: '第二天看了 80 页中的 1 份，也就是 20 页。把过程倒过来检查：20 × 4 = 80，算对啦！', note: '答案：20 页', parts: ['bar-read','bar-left'], labels: ['第一天：40页','第二天：20页'] }
];
let current = 0;
const $ = (id) => document.getElementById(id);
function renderStep() {
  const step = steps[current];
  $('step-number').textContent = step.number;
  $('step-title').textContent = step.title;
  $('step-description').textContent = step.description;
  $('visual-stage').innerHTML = `<div class="book-bar"><span class="${step.parts[0]}"></span><span class="${step.parts[1]}"></span></div><div class="bar-caption"><span>${step.labels[0]}</span><span>${step.labels[1]}</span></div><div class="stage-note">${step.note}</div>`;
  document.querySelectorAll('#step-dots i').forEach((dot, index) => dot.classList.toggle('active', index === current));
  $('prev-step').disabled = current === 0;
  $('next-step').textContent = current === steps.length - 1 ? '✓' : '→';
}
$('next-step').addEventListener('click', () => { current = Math.min(current + 1, steps.length - 1); renderStep(); });
$('prev-step').addEventListener('click', () => { current = Math.max(current - 1, 0); renderStep(); });
$('load-sample').addEventListener('click', () => { current = 0; renderStep(); document.querySelector('.lesson-card').scrollIntoView({ behavior: 'smooth', block: 'center' }); });
$('edit-question').addEventListener('click', () => {
  const text = prompt('请确认题目文字：', $('question-text').textContent.trim());
  if (text && text.trim()) $('question-text').textContent = text.trim();
});
$('read-aloud').addEventListener('click', () => {
  if (!('speechSynthesis' in window)) return alert('当前浏览器暂不支持语音朗读。');
  speechSynthesis.cancel(); const utterance = new SpeechSynthesisUtterance(`${steps[current].title}。${steps[current].description}`); utterance.lang = 'zh-CN'; speechSynthesis.speak(utterance);
});
$('check-answer').addEventListener('click', () => { const feedback = $('feedback'); feedback.className = 'feedback ' + ($('answer').value.trim() === '12' ? 'correct' : 'wrong'); feedback.textContent = $('answer').value.trim() === '12' ? '答对啦！48 × 1/4 = 12' : '再想想：48 平均分成 4 份，每份是多少？'; });
$('question-image').addEventListener('change', (event) => { const file = event.target.files[0]; if (!file) return; const zone = document.querySelector('.upload-zone'); zone.querySelector('strong').textContent = `已选择：${file.name}`; zone.querySelector('small').textContent = '示例原型暂用内置讲解，下一版接入识别'; zone.style.borderColor = 'var(--green)'; });
renderStep();
