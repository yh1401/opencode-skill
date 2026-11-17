/* =========================================================
 * API 对接文档面板（本地联调用）
 *   - 在线查看 / 下载 docs/前端对接说明.md（走本地测试服务 GET /doc）
 *   - 内置接口在线调试：按文档里的 8 个接口预置路径，填入参数即可请求
 * 依赖主脚本中的全局：API_BASE / authHeaders()
 * ========================================================= */

var DOC_API = API_BASE + "/api/external/operationTopology/doc";
var PROTOTYPE_API = API_BASE + "/api/external/operationTopology/prototype"; // 原型包：/files 清单、/download zip

// 预置接口（与《前端对接说明》§2 接口总览一致）
var DOC_TESTS = [
  { name: "① 拓扑图 GET /graph", method: "GET", path: "/graph", query: "env=PRO" },
  { name: "② 关系明细 GET /list", method: "GET", path: "/list", query: "page=1&pageSize=20" },
  { name: "③ 节点详情 GET /detail", method: "GET", path: "/detail", query: "id=app:base-config" },
  { name: "④ 类型枚举 GET /middlewareTypes", method: "GET", path: "/middlewareTypes", query: "" },
  { name: "⑤ 手动同步 POST /sync", method: "POST", path: "/sync", query: "" },
  { name: "⑥ 同步状态 GET /sync/status", method: "GET", path: "/sync/status", query: "" },
  { name: "⑦ 同步日志 GET /sync/logs", method: "GET", path: "/sync/logs", query: "date=" },
  { name: "⑧ 日志下载 GET /sync/logs?download=1", method: "GET", path: "/sync/logs", query: "download=1" }
];

var docCache = null;      // 文档内容缓存（file/updatedAt/sizeBytes/content）
var docTab = "doc";       // 当前页签：doc | test

/* ---------------- 面板开关 ---------------- */
function openApiDocPanel(tab) {
  docTab = tab || "doc";
  document.getElementById("docMask").classList.add("show");
  renderDocPanel();
}
function closeApiDocPanel() {
  document.getElementById("docMask").classList.remove("show");
}

/* ---------------- 页签与渲染 ---------------- */
function renderDocPanel() {
  const tabs = [["doc", "docTabDoc", "对接文档"], ["test", "docTabTest", "接口测试"]];
  tabs.forEach(([key, id]) => {
    const el = document.getElementById(id);
    if (el) el.classList.toggle("active", docTab === key);
  });
  document.getElementById("docTitle").textContent =
    docTab === "doc" ? "API 对接文档 · 前端对接说明" : "API 接口在线调试";
  if (docTab === "doc") renderDocTab(); else renderDocTestTab();
}

function renderDocTab() {
  const el = document.getElementById("docBody");
  const toolbar = `<div class="doc-toolbar">
      <span class="doc-file" id="docFileMeta">正在读取文档…</span>
      <button class="sync-log-btn" id="docRefreshBtn">刷新</button>
      <button class="sync-log-btn" id="docDlBtn">下载文档</button>
      <button class="sync-log-btn" id="prototypeDlBtn" title="打包下载 api/tprel/prototype 整个目录">下载原型包(zip)</button>
      <button class="sync-log-btn" id="prototypeListBtn" title="列出原型目录文件并逐个下载">原型文件清单</button>
    </div>
    <div class="proto-list" id="protoList"></div>`;
  el.innerHTML = toolbar + `<div class="md-body" id="mdBody"><div class="doc-loading">加载中…</div></div>`;
  document.getElementById("docRefreshBtn").addEventListener("click", () => loadDoc(true));
  document.getElementById("docDlBtn").addEventListener("click", downloadDoc);
  document.getElementById("prototypeDlBtn").addEventListener("click", downloadPrototypeZip);
  document.getElementById("prototypeListBtn").addEventListener("click", loadPrototypeFiles);
  loadDoc(false);
}

async function loadDoc(force) {
  const meta = document.getElementById("docFileMeta");
  const body = document.getElementById("mdBody");
  if (!body) return;
  if (docCache && !force) { paintDoc(); return; }
  try {
    const resp = await fetch(DOC_API, { headers: authHeaders() });
    const j = await resp.json();
    if (!resp.ok || j.code !== 0) {
      body.innerHTML = `<div class="doc-err">读取文档失败：${(j && j.msg) || ("HTTP " + resp.status)}</div>`;
      if (meta) meta.textContent = "读取失败";
      return;
    }
    docCache = j.data || {};
    paintDoc();
  } catch (e) {
    body.innerHTML = `<div class="doc-err">读取文档异常：${e.message}</div>`;
  }
}

function paintDoc() {
  const meta = document.getElementById("docFileMeta");
  const body = document.getElementById("mdBody");
  if (!body || !docCache) return;
  if (meta) {
    meta.textContent = `${docCache.file} ｜ ${(docCache.sizeBytes / 1024).toFixed(1)} KB ｜ 更新于 ${docCache.updatedAt}`;
  }
  body.innerHTML = mdToHtml(docCache.content || "");
}

async function downloadDoc() {
  try {
    const resp = await fetch(DOC_API + "?download=1", { headers: authHeaders() });
    if (!resp.ok) { alert("下载失败：HTTP " + resp.status); return; }
    const blob = await resp.blob();
    const cd = resp.headers.get("Content-Disposition") || "";
    let name = "前端对接说明.md";
    const star = /filename\*=UTF-8''([^;]+)/i.exec(cd);
    const plain = /filename="?([^";]+)"?/i.exec(cd);
    if (star) { try { name = decodeURIComponent(star[1]); } catch (e) { /* 忽略 */ } }
    else if (plain && !/^frontend-api-doc\.md$/.test(plain[1])) { name = plain[1]; }
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name;
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(a.href);
  } catch (e) { alert("下载异常：" + e.message); }
}

/* ---------------- 原型包下载 / 文件清单 ---------------- */
async function downloadPrototypeZip() {
  try {
    const resp = await fetch(PROTOTYPE_API + "/download", { headers: authHeaders() });
    if (!resp.ok) { alert("打包下载失败：HTTP " + resp.status); return; }
    const blob = await resp.blob();
    const cd = resp.headers.get("Content-Disposition") || "";
    let name = "operation-topology-prototype.zip";
    const star = /filename\*=UTF-8''([^;]+)/i.exec(cd);
    const plain = /filename="?([^";]+)"?/i.exec(cd);
    if (star) { try { name = decodeURIComponent(star[1]); } catch (e) { /* 忽略 */ } }
    else if (plain && plain[1]) { name = plain[1]; }
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name;
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(a.href);
  } catch (e) { alert("打包下载异常：" + e.message); }
}

async function loadPrototypeFiles() {
  const box = document.getElementById("protoList");
  if (!box) return;
  box.innerHTML = `<div class="doc-loading">加载文件清单…</div>`;
  try {
    const resp = await fetch(PROTOTYPE_API + "/files", { headers: authHeaders() });
    const j = await resp.json();
    if (!resp.ok || j.code !== 0) {
      box.innerHTML = `<div class="doc-err">读取文件清单失败：${(j && j.msg) || ("HTTP " + resp.status)}</div>`;
      return;
    }
    const files = (j.data && j.data.files) || [];
    if (!files.length) { box.innerHTML = `<div class="doc-err">原型目录为空或不存在</div>`; return; }
    let rows = files.map(f => {
      const url = API_BASE + "/prototype/" + f.rel; // 静态托管：/prototype/<相对路径>
      const size = f.sizeBytes ? ((f.sizeBytes / 1024).toFixed(1) + " KB") : "—";
      return `<div class="proto-file"><span class="proto-name">${escHtml(f.rel)}</span>` +
        `<span class="proto-size">${size}</span>` +
        `<a class="sync-log-btn" href="${escAttr(url)}" download="${escAttr(f.rel)}">下载</a></div>`;
    }).join("");
    box.innerHTML = `<div class="proto-head">共 ${files.length} 个文件，点击「下载」逐个获取（走静态服务 /prototype/…）</div>` + rows;
  } catch (e) {
    box.innerHTML = `<div class="doc-err">读取文件清单异常：${e.message}</div>`;
  }
}

/* ---------------- 接口调试 ---------------- */
function renderDocTestTab() {
  const el = document.getElementById("docBody");
  const opts = DOC_TESTS.map((t, i) => `<option value="${i}">${t.name}</option>`).join("");
  const savedToken = localStorage.getItem("op_topology_token") || localStorage.getItem("aiops-auth-token") || "";
  el.innerHTML =
    `<div class="doc-toolbar">
       <span class="doc-file">按《前端对接说明》预置接口，可直接发送；本地无需 token（生产填 JWT）</span>
     </div>
     <div class="api-form">
       <select id="docTestPreset">${opts}</select>
       <select id="docTestMethod"><option>GET</option><option>POST</option></select>
       <input id="docTestPath" placeholder="/graph" spellcheck="false">
       <input id="docTestQuery" placeholder="env=PRO&page=1" spellcheck="false">
       <input id="docTestToken" placeholder="token（可选）" value="${escAttr(savedToken)}" spellcheck="false">
       <button class="sync-btn" id="docTestSend">发送请求</button>
     </div>
     <div class="api-hint">完整地址：<span id="docTestUrl"></span></div>
     <div class="api-meta" id="docTestMeta"></div>
     <pre class="api-resp" id="docTestResp">（未发送）</pre>`;

  const preset = document.getElementById("docTestPreset");
  preset.addEventListener("change", () => {
    const t = DOC_TESTS[Number(preset.value)] || DOC_TESTS[0];
    document.getElementById("docTestMethod").value = t.method;
    document.getElementById("docTestPath").value = t.path;
    document.getElementById("docTestQuery").value = t.query;
    updateTestUrl();
  });
  ["docTestMethod", "docTestPath", "docTestQuery"].forEach(id => {
    document.getElementById(id).addEventListener("input", updateTestUrl);
    document.getElementById(id).addEventListener("change", updateTestUrl);
  });
  document.getElementById("docTestToken").addEventListener("change", e => {
    localStorage.setItem("op_topology_token", e.target.value.trim());
  });
  document.getElementById("docTestSend").addEventListener("click", docSendTest);
  // 默认选中第一个接口
  preset.dispatchEvent(new Event("change"));
}

function docBuildUrl() {
  const path = (document.getElementById("docTestPath").value || "").trim();
  const query = (document.getElementById("docTestQuery").value || "").trim();
  return API_BASE + "/api/external/operationTopology" + path + (query ? "?" + query : "");
}

function updateTestUrl() {
  const el = document.getElementById("docTestUrl");
  if (el) el.textContent = docBuildUrl();
}

async function docSendTest() {
  const method = document.getElementById("docTestMethod").value;
  const query = (document.getElementById("docTestQuery").value || "").trim();
  const token = (document.getElementById("docTestToken").value || "").trim();
  const meta = document.getElementById("docTestMeta");
  const out = document.getElementById("docTestResp");
  const url = docBuildUrl();
  const btn = document.getElementById("docTestSend");
  btn.disabled = true;
  out.textContent = "请求中…";
  meta.textContent = `${method} ${url}`;
  const t0 = performance.now();
  try {
    const resp = await fetch(url, { method, headers: token ? { Authorization: `Bearer ${token}` } : authHeaders() });
    const ms = Math.round(performance.now() - t0);
    const ct = resp.headers.get("Content-Type") || "";
    const cd = resp.headers.get("Content-Disposition") || "";
    const isFile = /attachment/i.test(cd);
    meta.className = "api-meta " + (resp.ok ? "ok" : "bad");
    meta.textContent = `${method} ${url} ｜ HTTP ${resp.status} ｜ ${ms} ms ｜ ${ct}` + (isFile ? " ｜ 附件下载" : "");
    if (isFile) {
      const blob = await resp.blob();
      out.textContent = `（附件响应，${blob.size} 字节，内容未展开）\n提示：需要保存文件请用《同步日志》面板的「下载」，或去掉 download=1 查看文本。`;
    } else {
      const text = await resp.text();
      let pretty = text;
      try { pretty = JSON.stringify(JSON.parse(text), null, 2); } catch (e) { /* 非 JSON 原样展示 */ }
      out.textContent = pretty.length > 200000 ? pretty.slice(0, 200000) + "\n…（已截断，仅展示前 200KB）" : pretty;
    }
  } catch (e) {
    meta.className = "api-meta bad";
    meta.textContent = `${method} ${url} ｜ 请求异常`;
    out.textContent = String((e && e.message) || e);
  } finally {
    btn.disabled = false;
  }
}

/* ---------------- 极简 Markdown 渲染（标题/表格/代码块/列表/引用/行内样式） ---------------- */
function escHtml(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
function escAttr(s) {
  return escHtml(s).replace(/"/g, "&quot;");
}
function mdInline(s) {
  return escHtml(s)
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
}
function mdToHtml(md) {
  const lines = String(md).replace(/\r\n/g, "\n").split("\n");
  const out = [];
  let i = 0, inCode = false, codeBuf = [];
  const isTableSep = s => /^\s*\|?[\s:|-]*-[\s:|-]*\|?\s*$/.test(s) && s.indexOf("|") >= 0;
  const parseRow = r => r.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map(c => c.trim());
  while (i < lines.length) {
    const ln = lines[i];
    if (/^\s*```/.test(ln)) {
      if (!inCode) { inCode = true; codeBuf = []; }
      else { inCode = false; out.push(`<pre class="md-code">${escHtml(codeBuf.join("\n"))}</pre>`); }
      i++; continue;
    }
    if (inCode) { codeBuf.push(ln); i++; continue; }
    if (/^\s*$/.test(ln)) { i++; continue; }
    // 表格
    if (/^\s*\|/.test(ln) && i + 1 < lines.length && isTableSep(lines[i + 1])) {
      const head = parseRow(ln); i += 2;
      const rows = [];
      while (i < lines.length && /^\s*\|/.test(lines[i])) { rows.push(parseRow(lines[i])); i++; }
      out.push(`<div class="md-table-wrap"><table class="md-table"><thead><tr>` +
        head.map(h => `<th>${mdInline(h)}</th>`).join("") + `</tr></thead><tbody>` +
        rows.map(r => `<tr>${head.map((_, k) => `<td>${mdInline(r[k] || "")}</td>`).join("")}</tr>`).join("") +
        `</tbody></table></div>`);
      continue;
    }
    // 标题
    const h = ln.match(/^(#{1,6})\s+(.*)$/);
    if (h) { out.push(`<h${h[1].length} class="md-h">${mdInline(h[2])}</h${h[1].length}>`); i++; continue; }
    // 分隔线
    if (/^\s*([-*_])\1{2,}\s*$/.test(ln)) { out.push(`<hr class="md-hr">`); i++; continue; }
    // 引用
    if (/^\s*>/.test(ln)) {
      const buf = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) { buf.push(lines[i].replace(/^\s*>\s?/, "")); i++; }
      out.push(`<blockquote class="md-quote">${buf.map(b => mdInline(b)).join("<br>")}</blockquote>`);
      continue;
    }
    // 无序列表
    if (/^\s*[-*+]\s+/.test(ln)) {
      const items = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) { items.push(lines[i].replace(/^\s*[-*+]\s+/, "")); i++; }
      out.push(`<ul class="md-ul">${items.map(t => `<li>${mdInline(t)}</li>`).join("")}</ul>`);
      continue;
    }
    // 有序列表
    if (/^\s*\d+\.\s+/.test(ln)) {
      const items = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) { items.push(lines[i].replace(/^\s*\d+\.\s+/, "")); i++; }
      out.push(`<ol class="md-ol">${items.map(t => `<li>${mdInline(t)}</li>`).join("")}</ol>`);
      continue;
    }
    // 段落
    const buf = [ln]; i++;
    while (i < lines.length && !/^\s*$/.test(lines[i]) && !/^\s*```/.test(lines[i]) &&
      !/^(#{1,6})\s/.test(lines[i]) && !/^\s*([-*+]|\d+\.)\s+/.test(lines[i]) &&
      !/^\s*\|/.test(lines[i]) && !/^\s*>/.test(lines[i])) { buf.push(lines[i]); i++; }
    out.push(`<p class="md-p">${buf.map(b => mdInline(b)).join("<br>")}</p>`);
  }
  if (inCode && codeBuf.length) out.push(`<pre class="md-code">${escHtml(codeBuf.join("\n"))}</pre>`);
  return out.join("\n");
}

/* ---------------- 初始化：入口按钮与页签绑定 ---------------- */
(function initDocPanel() {
  const btn = document.getElementById("btnDoc");
  if (btn) btn.addEventListener("click", () => openApiDocPanel("doc"));
  const t1 = document.getElementById("docTabDoc");
  const t2 = document.getElementById("docTabTest");
  if (t1) t1.addEventListener("click", () => { docTab = "doc"; renderDocPanel(); });
  if (t2) t2.addEventListener("click", () => { docTab = "test"; renderDocPanel(); });
  const mask = document.getElementById("docMask");
  if (mask) mask.addEventListener("click", e => { if (e.target === mask) closeApiDocPanel(); });
})();
