(function () {
  'use strict';
  var $ = function (id) { return document.getElementById(id); };
  var state = { settings: null };

  async function api(path, opts) {
    opts = opts || {};
    var init = { method: opts.method || 'GET', headers: {}, cache: 'no-store' };
    if (opts.body !== undefined) { init.headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(opts.body); }
    var res = await fetch('/api/' + path, init);
    var text = await res.text();
    var data = null;
    try { data = text ? JSON.parse(text) : null; } catch (e) { data = { error: text }; }
    if (!res.ok) { throw new Error((data && data.error) || ('HTTP ' + res.status)); }
    return data;
  }
  var tt = null;
  function toast(msg, err) {
    var el = $('toast'); el.textContent = msg; el.className = 'toast show' + (err ? ' err' : '');
    clearTimeout(tt); tt = setTimeout(function () { el.className = 'toast'; }, 3500);
  }
  function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]; }); }

  function switchTab(name) {
    Array.prototype.forEach.call(document.querySelectorAll('.tab'), function (b) { b.classList.toggle('active', b.dataset.tab === name); });
    Array.prototype.forEach.call(document.querySelectorAll('.pane'), function (p) { p.classList.toggle('active', p.id === 'pane-' + name); });
    if (name === 'logs') { refreshLogs(); }
    if (name === 'settings') { loadSettings(); }
  }

  async function refreshStatus() {
    try {
      var s = await api('status');
      var pill = $('status-pill');
      var ok = s.cookie_ok;
      pill.className = 'pill ' + (ok ? 'ok' : 'bad');
      pill.querySelector('span').textContent = (ok ? '夸克已登录' : '夸克未配置') + ' · ' + s.channels + ' 个频道';
    } catch (e) { var p = $('status-pill'); p.className = 'pill bad'; p.querySelector('span').textContent = '连接失败'; }
  }

  async function doSearch() {
    var q = $('q').value.trim();
    if (q.length < 2) { toast('请输入至少 2 个字', true); return; }
    var btn = $('btn-search'); btn.disabled = true; btn.textContent = '搜索中…';
    $('search-status').textContent = '正在检索频道…';
    try {
      var d = await api('search?q=' + encodeURIComponent(q));
      renderResults(d.results || []);
      $('search-status').textContent = '找到 ' + (d.results || []).length + ' 条 · 耗时 ' + (d.took_ms || 0) + 'ms' +
        ((d.failed && d.failed.length) ? ' · ' + d.failed.length + ' 个频道不可达' : '');
    } catch (e) { $('search-status').textContent = '搜索失败：' + e.message; toast(e.message, true); }
    finally { btn.disabled = false; btn.textContent = '搜索'; }
  }

  function renderResults(list) {
    $('result-count').textContent = list.length ? (list.length + ' 条') : '';
    $('results').innerHTML = list.length ? list.map(function (r, i) {
      var cover = r.image
        ? '<img class="cover" loading="lazy" src="/api/image?url=' + encodeURIComponent(r.image) + '" alt="">'
        : '<div class="cover placeholder">' + esc((r.channel || '?').slice(0, 1)) + '</div>';
      return '<div class="result">' + cover +
        '<div class="result-body">' +
        '<h3>' + esc(r.title || '未命名资源') + '</h3>' +
        '<div class="meta"><span class="tag">' + esc(r.channel) + '</span>' +
        '<span class="tag check" data-i="' + i + '">检测中…</span>' +
        (r.passcode ? '<span>提取码 ' + esc(r.passcode) + '</span>' : '') +
        (r.pub_date ? '<span>' + esc(r.pub_date.slice(0, 10)) + '</span>' : '') + '</div>' +
        '<div class="url">' + esc(r.url) + '</div>' +
        '<div class="actions">' +
        '<a class="btn" href="' + esc(r.url) + '" target="_blank" rel="noopener">打开</a>' +
        '<button class="btn primary save" data-i="' + i + '">转存</button></div>' +
        '</div></div>';
    }).join('') : '<p class="hint">暂无结果。</p>';
    Array.prototype.forEach.call(document.querySelectorAll('.cover'), function (img) {
      if (img.tagName === 'IMG') { img.addEventListener('error', function () { img.style.display = 'none'; }); }
    });
    checkAll(list);
    Array.prototype.forEach.call(document.querySelectorAll('.save'), function (b) {
      b.addEventListener('click', function () {
        var r = list[parseInt(b.dataset.i, 10)];
        if (r.invalid) { toast('该链接已失效：' + (r.invalidReason || ''), true); return; }
        openPicker({ url: r.url, passcode: r.passcode || '' });
      });
    });
  }

  // ---------------------------------------------------------------- 链接有效性检测

  var checkCache = {};

  function setCheckTag(el, text, cls, title) {
    el.textContent = text;
    el.className = 'tag ' + (cls || '');
    if (title) el.title = title;
  }

  async function checkOne(r, el) {
    if (!el || !r) return;
    var cached = checkCache[r.share_id];
    if (cached) {
      if (cached.valid) setCheckTag(el, '✅ 有效', 'ok');
      else { setCheckTag(el, '❌ 失效', 'bad', cached.reason); r.invalid = true; r.invalidReason = cached.reason; }
      return;
    }
    try {
      var d = await api('check', { method: 'POST', body: { url: r.url, passcode: r.passcode || '' } });
      checkCache[r.share_id] = d;
      if (d.valid) setCheckTag(el, '✅ 有效', 'ok');
      else { setCheckTag(el, '❌ 失效', 'bad', d.reason || ''); r.invalid = true; r.invalidReason = d.reason || ''; }
    } catch (e) {
      setCheckTag(el, '？检测失败', '', e.message);
    }
  }

  function checkAll(list) {
    var els = Array.prototype.slice.call(document.querySelectorAll('.check'));
    var cursor = 0;
    async function worker() {
      while (cursor < els.length) {
        var i = cursor++;
        await checkOne(list[i], els[i]);
      }
    }
    for (var k = 0; k < 4; k++) worker();
  }

  var picker = { stack: [{ fid: '0', name: '根目录' }], pending: null };

  function currentFid() { return picker.stack[picker.stack.length - 1].fid; }

  function renderPickerPath() {
    $('picker-path').textContent = picker.stack.map(function (x) { return x.name; }).join(' / ');
  }

  async function loadPicker(fid) {
    $('picker-list').innerHTML = '<div class="picker-empty">读取中…</div>';
    try {
      var d = await api('quark/folders?fid=' + encodeURIComponent(fid));
      var list = d.folders || [];
      $('picker-list').innerHTML = list.length ? list.map(function (f, i) {
        return '<div class="picker-item" data-i="' + i + '">📁 ' + esc(f.file_name) + '</div>';
      }).join('') : '<div class="picker-empty">没有子目录</div>';
      Array.prototype.forEach.call($('picker-list').querySelectorAll('.picker-item'), function (el) {
        el.addEventListener('click', function () {
          var f = list[parseInt(el.dataset.i, 10)];
          picker.stack.push({ fid: f.fid, name: f.file_name });
          renderPickerPath();
          loadPicker(f.fid);
        });
      });
      renderPickerPath();
    } catch (e) { $('picker-list').innerHTML = '<div class="picker-empty">读取失败：' + esc(e.message) + '</div>'; }
  }

  function openPicker(pending) {
    picker.pending = pending;
    picker.stack = [{ fid: '0', name: '根目录' }];
    $('picker-modal').hidden = false;
    loadPicker('0');
  }

  function closePicker() { $('picker-modal').hidden = true; picker.pending = null; }

  async function confirmPicker() {
    if (!picker.pending) return;
    var btn = $('btn-picker-choose'); btn.disabled = true; btn.textContent = '转存中…';
    try {
      var d = await api('save', { method: 'POST', body: {
        url: picker.pending.url, passcode: picker.pending.passcode || '', to_fid: currentFid()
      }});
      toast('已转存：' + (d.title || '') + '（' + d.count + ' 项）');
      closePicker();
      refreshStatus();
    } catch (e) { toast(e.message, true); }
    finally { btn.disabled = false; btn.textContent = '转存到当前目录'; }
  }

  async function resolveLink() {
    var url = $('manual-url').value.trim();
    if (!url) { toast('请填写链接', true); return; }
    $('resolve-box').textContent = '解析中…';
    try {
      var d = await api('resolve', { method: 'POST', body: { url: url, passcode: $('manual-pass').value.trim() } });
      var names = (d.files || []).slice(0, 8).map(function (f) { return (f.dir ? '📁 ' : '📄 ') + f.file_name; }).join('<br>');
      $('resolve-box').innerHTML = '<b>' + esc(d.title) + '</b><br>' + names + ((d.files || []).length > 8 ? '<br>…' : '');
    } catch (e) { $('resolve-box').textContent = '解析失败：' + e.message; toast(e.message, true); }
  }

  async function loadSettings() {
    try {
      var s = await api('settings');
      state.settings = s;
      $('quark-cookie').value = s.quark_cookie || '';
      $('channels').value = (s.channels || []).join('\n');
      $('proxy-url').value = s.proxy_url || '';
      $('max-results').value = s.max_results || 60;
      $('strict-match').checked = s.strict_match !== false;
    } catch (e) { toast(e.message, true); }
  }

  async function saveSettings() {
    var s = JSON.parse(JSON.stringify(state.settings || {}));
    s.quark_cookie = $('quark-cookie').value.trim();
    s.channels = $('channels').value.split('\n').map(function (x) { return x.trim(); }).filter(Boolean);
    s.proxy_url = $('proxy-url').value.trim();
    s.max_results = parseInt($('max-results').value, 10) || 60;
    s.strict_match = $('strict-match').checked;
    try {
      var out = await api('settings', { method: 'PUT', body: s });
      state.settings = out;
      $('quark-cookie').value = out.quark_cookie || '';
      $('settings-status').textContent = '已保存';
      toast('设置已保存');
      refreshStatus();
    } catch (e) { toast(e.message, true); }
  }

  async function testProxy() {
    var btn = $('btn-test-proxy'); btn.disabled = true; btn.textContent = '测试中…';
    $('proxy-result').textContent = '正在通过代理访问 Telegram 和百度…';
    try {
      var d = await api('proxy/test', { method: 'POST', body: { proxy_url: $('proxy-url').value.trim() } });
      $('proxy-result').innerHTML = (d.checks || []).map(function (c) {
        return esc(c.target) + '：' + (c.ok ? ('✅ ' + c.latency_ms + 'ms') : ('❌ ' + esc(c.error || ('HTTP ' + c.status))));
      }).join('<br>');
    } catch (e) {
      $('proxy-result').textContent = '测试失败：' + e.message;
      toast(e.message, true);
    } finally { btn.disabled = false; btn.textContent = '测试代理'; }
  }

  async function testCookie() {
    var btn = $('btn-test-cookie'); btn.disabled = true;
    try {
      var d = await api('quark/test', { method: 'POST' });
      toast('Cookie 有效：' + d.name);
    } catch (e) { toast(e.message, true); }
    finally { btn.disabled = false; }
  }

  async function refreshLogs() {
    try { var d = await api('logs?lines=300'); var el = $('logs'); el.textContent = (d.lines || []).join('\n') || '（暂无日志）'; el.scrollTop = el.scrollHeight; }
    catch (e) { /* ignore */ }
  }

  async function init() {
    Array.prototype.forEach.call(document.querySelectorAll('.tab'), function (b) { b.addEventListener('click', function () { switchTab(b.dataset.tab); }); });
    $('btn-search').addEventListener('click', doSearch);
    $('q').addEventListener('keydown', function (e) { if (e.key === 'Enter') { doSearch(); } });
    $('btn-resolve').addEventListener('click', resolveLink);
    $('btn-save-url').addEventListener('click', function () { openPicker({ url: $('manual-url').value.trim(), passcode: $('manual-pass').value.trim() }); });
    $('btn-save').addEventListener('click', saveSettings);
    $('btn-test-cookie').addEventListener('click', testCookie);
    $('btn-test-proxy').addEventListener('click', testProxy);
    $('btn-picker-up').addEventListener('click', function () {
      if (picker.stack.length > 1) { picker.stack.pop(); loadPicker(currentFid()); }
    });
    $('btn-picker-cancel').addEventListener('click', closePicker);
    $('btn-picker-choose').addEventListener('click', confirmPicker);
    $('picker-modal').addEventListener('click', function (e) { if (e.target === $('picker-modal')) closePicker(); });
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape' && !$('picker-modal').hidden) closePicker(); });
    await refreshStatus();
    setInterval(refreshStatus, 15000);
  }
  if (document.readyState === 'loading') { document.addEventListener('DOMContentLoaded', init); } else { init(); }
})();
