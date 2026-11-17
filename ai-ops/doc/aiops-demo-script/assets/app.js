(function () {
  'use strict';

  /* ---------------- 现场打勾（localStorage 记忆） ---------------- */
  var KEY = 'aiops-demo-checks-v1';
  var store = {};
  try { store = JSON.parse(localStorage.getItem(KEY) || '{}') || {}; } catch (e) { store = {}; }

  var steps = Array.prototype.slice.call(document.querySelectorAll('.step[data-key]'));
  var boxes = steps.map(function (s) { return s.querySelector('.check'); }).filter(Boolean);

  var totalEl = document.getElementById('totalCount');
  var doneEl = document.getElementById('doneCount');
  var barEl = document.getElementById('doneBar');

  function save() {
    try { localStorage.setItem(KEY, JSON.stringify(store)); } catch (e) { /* 忽略隐私模式限制 */ }
  }

  function refresh() {
    var done = 0;
    steps.forEach(function (step) {
      var box = step.querySelector('.check');
      if (!box) return;
      if (box.checked) {
        done++;
        step.classList.add('done');
      } else {
        step.classList.remove('done');
      }
    });
    if (doneEl) doneEl.textContent = String(done);
    if (totalEl) totalEl.textContent = String(boxes.length);
    if (barEl) barEl.style.width = (boxes.length ? (done / boxes.length) * 100 : 0) + '%';
  }

  steps.forEach(function (step) {
    var box = step.querySelector('.check');
    if (!box) return;
    var key = step.getAttribute('data-key');
    if (store[key]) box.checked = true;
    box.addEventListener('change', function () {
      store[key] = box.checked;
      save();
      refresh();
    });
  });

  if (totalEl) totalEl.textContent = String(boxes.length);
  refresh();

  /* ---------------- 点击截图看原图 ---------------- */
  Array.prototype.forEach.call(document.querySelectorAll('.shot img'), function (img) {
    img.addEventListener('click', function () {
      window.open(img.src, '_blank', 'noopener');
    });
  });

  /* ---------------- 演示计时器 ---------------- */
  var TARGET = 20 * 60;
  var clock = document.getElementById('clock');
  var toggle = document.getElementById('timerToggle');
  var reset = document.getElementById('timerReset');
  var timerBox = document.getElementById('timer');
  var timerBar = document.getElementById('timerBar');

  var elapsed = 0;
  var running = false;
  var tick = null;

  function pad(n) { return (n < 10 ? '0' : '') + n; }

  function render() {
    if (clock) clock.textContent = pad(Math.floor(elapsed / 60)) + ':' + pad(elapsed % 60);
    if (timerBar) timerBar.style.width = Math.min(elapsed / TARGET, 1) * 100 + '%';
    if (timerBox) timerBox.classList.toggle('over', elapsed > TARGET);
  }

  function stop() {
    running = false;
    if (tick) { clearInterval(tick); tick = null; }
    if (toggle) toggle.textContent = '继续';
  }

  function start() {
    running = true;
    if (toggle) toggle.textContent = '暂停';
    tick = setInterval(function () {
      elapsed++;
      render();
      if (elapsed > TARGET + 600) stop();
    }, 1000);
  }

  if (toggle) {
    toggle.addEventListener('click', function () {
      if (running) { stop(); } else { start(); }
    });
  }

  if (reset) {
    reset.addEventListener('click', function () {
      stop();
      elapsed = 0;
      if (toggle) toggle.textContent = '开始';
      render();
    });
  }

  render();

  /* ---------------- Mermaid ---------------- */
  if (typeof mermaid !== 'undefined') {
    var cs = getComputedStyle(document.documentElement);
    function v(name) { return cs.getPropertyValue(name).trim(); }
    try {
      mermaid.initialize({
        startOnLoad: true,
        securityLevel: 'loose',
        theme: 'base',
        fontFamily: '"PingFang SC","Microsoft YaHei",sans-serif',
        themeVariables: {
          background: v('--bg2'),
          primaryColor: v('--bg2'),
          primaryTextColor: v('--ink'),
          primaryBorderColor: v('--accent'),
          secondaryColor: v('--bg'),
          tertiaryColor: v('--bg2'),
          lineColor: v('--muted'),
          textColor: v('--ink'),
          fontSize: '16px'
        }
      });
    } catch (e) { /* 图形渲染失败不影响正文 */ }
  }
})();
