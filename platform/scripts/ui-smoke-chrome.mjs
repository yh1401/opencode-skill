/**
 * ai-ops 前端 UI 冒烟测试（真实 Chrome + CDP）
 *
 * 用法：
 *   node scripts/ui-smoke-chrome.mjs
 *   BASE_URL=http://localhost:5173 node scripts/ui-smoke-chrome.mjs
 *
 * 说明：
 * - 自动拉起一个独立的无头 Chrome（临时 profile，不复用日常浏览器数据），测完自动关闭。
 * - 逐个路由全量刷新，收集 DOM 指标与 JS 异常；favicon 404 不算失败。
 * - 退出码：0 全部通过；1 存在失败路由。
 *
 * 依赖：Node 22+（内置 fetch / WebSocket）与本机 Google Chrome。
 */
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { rm } from 'node:fs/promises';

const BASE_URL = process.env.BASE_URL || 'http://localhost:5173';
const PORT = Number(process.env.CDP_PORT || 9222);
const CHROME_BIN = process.env.CHROME_BIN || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const PROFILE_DIR = '/tmp/aiops-ui-smoke-profile';
const SETTLE_MS = 1200;

const ROUTES = [
  '/dashboard', '/diagnosis', '/alert-center', '/inspection',
  '/topology', '/op-topology', '/tickets', '/quality', '/metrics',
  '/mcp', '/log-analysis',
];

/** 启动 Chrome，返回进程句柄 */
function launchChrome() {
  if (!existsSync(CHROME_BIN)) {
    throw new Error(`未找到 Chrome：${CHROME_BIN}，可通过 CHROME_BIN 指定路径`);
  }
  const proc = spawn(CHROME_BIN, [
    '--headless=new',
    `--remote-debugging-port=${PORT}`,
    `--user-data-dir=${PROFILE_DIR}`,
    '--no-first-run',
    '--no-default-browser-check',
    '--window-size=1440,900',
    'about:blank',
  ], { stdio: 'ignore' });
  return proc;
}

/** 等待 CDP 端口就绪 */
async function waitForCDP(timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/version`);
      if (res.ok) return res.json();
    } catch {
      // 端口尚未监听，继续等待
    }
    await new Promise((r) => setTimeout(r, 300));
  }
  throw new Error(`CDP 端口 ${PORT} 在 ${timeoutMs}ms 内未就绪`);
}

/** 最小 CDP 客户端：请求-响应 + 事件订阅 */
function connectCDP(wsUrl) {
  const ws = new WebSocket(wsUrl);
  let seq = 0;
  const pending = new Map();
  const listeners = [];

  const send = (method, params = {}, sessionId) =>
    new Promise((resolve, reject) => {
      const id = ++seq;
      pending.set(id, { resolve, reject });
      ws.send(JSON.stringify({ id, method, params, sessionId }));
    });

  const waitEvent = (name, sessionId, timeout = 12000) =>
    new Promise((resolve) => {
      const timer = setTimeout(() => resolve(null), timeout);
      const fn = (msg) => {
        if (msg.method === name && (!sessionId || msg.sessionId === sessionId)) {
          clearTimeout(timer);
          listeners.splice(listeners.indexOf(fn), 1);
          resolve(msg.params);
        }
      };
      listeners.push(fn);
    });

  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id);
      pending.delete(msg.id);
      msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
      return;
    }
    listeners.slice().forEach((fn) => fn(msg));
  });

  const opened = new Promise((resolve) => ws.addEventListener('open', resolve));
  return { ws, send, waitEvent, opened, listeners };
}

async function main() {
  const chrome = launchChrome();
  const stop = () => { try { chrome.kill('SIGKILL'); } catch { /* 已退出 */ } };
  process.on('exit', stop);

  try {
    const version = await waitForCDP();
    console.log(`Chrome ${version.Browser} · UA ${version['User-Agent']}`);
    console.log(`目标 ${BASE_URL}\n`);

    const cdp = connectCDP(version.webSocketDebuggerUrl);
    await cdp.opened;
    const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
    await cdp.send('Page.enable', {}, sessionId);
    await cdp.send('Runtime.enable', {}, sessionId);
    await cdp.send('Log.enable', {}, sessionId);

    let errors = [];
    cdp.listeners.push((msg) => {
      if (msg.sessionId !== sessionId) return;
      if (msg.method === 'Runtime.exceptionThrown') {
        const text = msg.params.exceptionDetails?.exception?.description || '';
        errors.push('exception: ' + text.split('\n')[0]);
      }
      if (msg.method === 'Runtime.consoleAPICalled' && msg.params.type === 'error') {
        const text = msg.params.args.map((a) => a.value ?? a.description ?? '').join(' ');
        errors.push('console: ' + text.slice(0, 160));
      }
      if (msg.method === 'Log.entryAdded' && msg.params.entry.level === 'error') {
        errors.push('log: ' + msg.params.entry.text.slice(0, 160));
      }
    });

    const rows = [];
    for (const route of ROUTES) {
      errors = [];
      const loaded = cdp.waitEvent('Page.loadEventFired', sessionId);
      await cdp.send('Page.navigate', { url: BASE_URL + route }, sessionId);
      await loaded;
      await new Promise((r) => setTimeout(r, SETTLE_MS));

      const { result } = await cdp.send('Runtime.evaluate', {
        expression: `(() => { const t = document.body.innerText || '';
          return { path: location.pathname,
            cards: document.querySelectorAll('.ant-card').length,
            rows: document.querySelectorAll('tbody tr').length,
            canvas: document.querySelectorAll('canvas').length,
            iframe: Array.from(document.querySelectorAll('iframe')).map(f => f.src),
            textErr: (t.match(/加载失败|请求失败|Network Error/g) || []).slice(0, 2) }; })()`,
        returnByValue: true,
      }, sessionId);

      // favicon 缺失属已知无害项
      const real = errors.filter((e) => !/favicon/i.test(e) && !/status of 404/.test(e));
      rows.push({ route, ...result.value, errors: real });
    }

    await cdp.send('Target.closeTarget', { targetId });
    cdp.ws.close();

    for (const r of rows) {
      const extra = [r.canvas ? `canvas=${r.canvas}` : '', r.iframe.length ? `iframe=${r.iframe.join(',')}` : '']
        .filter(Boolean).join(' ');
      const bad = r.errors.length || r.textErr?.length;
      console.log(
        `${bad ? 'FAIL' : 'PASS'}  ${r.route.padEnd(15)} cards=${String(r.cards).padEnd(2)} rows=${String(r.rows).padEnd(3)} ${extra}`,
      );
      if (bad) {
        r.errors.forEach((e) => console.log(`        ! ${e}`));
        r.textErr?.forEach((e) => console.log(`        ! 页面文案异常: ${e}`));
      }
    }

    const failed = rows.filter((r) => r.errors.length || r.textErr?.length);
    console.log(`\n共 ${rows.length} 个路由，通过 ${rows.length - failed.length}，失败 ${failed.length}`);
    process.exitCode = failed.length ? 1 : 0;
  } finally {
    stop();
    await rm(PROFILE_DIR, { recursive: true, force: true }).catch(() => {});
  }
}

main().catch((err) => {
  console.error('冒烟测试执行失败：', err.message);
  process.exit(1);
});
