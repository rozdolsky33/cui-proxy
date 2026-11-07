(function () {
  "use strict";
  var MAX_ROWS = 200;

  // el builds a DOM node; all text goes through textContent / text nodes.
  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined && text !== null) n.textContent = String(text);
    return n;
  }
  function $(id) { return document.getElementById(id); }
  function clear(n) { while (n.firstChild) n.removeChild(n.firstChild); }
  function setBadge(ok, text) {
    var b = $("badge");
    b.className = "badge " + (ok ? "ok" : "bad");
    b.textContent = text;
  }
  function pairs(obj) {
    return Object.keys(obj || {}).sort().map(function (k) { return k + "=" + obj[k]; }).join(", ");
  }
  function fmtMs(n) { return (Number(n) || 0).toFixed(1) + " ms"; }
  function fmtUptime(s) {
    s = Number(s) || 0;
    var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
    return h > 0 ? h + "h " + m + "m" : m + "m " + (s % 60) + "s";
  }
  function getJSON(url) {
    return fetch(url, { cache: "no-store" }).then(function (r) {
      if (!r.ok) throw new Error(url + " " + r.status);
      return r.json();
    });
  }
  function postJSON(url, body) {
    return fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) })
      .then(function (r) { return r.json().then(function (j) { return { status: r.status, body: j }; }); });
  }

  // Metrics cards
  function card(label, val, sub) {
    var c = el("div", "card");
    c.appendChild(el("div", "label", label));
    c.appendChild(el("div", "val", val));
    if (sub) c.appendChild(el("div", "sub", sub));
    return c;
  }
  function renderMetrics(m) {
    var box = $("cards");
    clear(box);
    box.appendChild(card("Requests / min", m.requests_per_minute));
    box.appendChild(card("Requests", m.requests, "passthrough " + (m.passthrough || 0)));
    box.appendChild(card("Replacements", m.replacements, pairs(m.replacements_by_rule)));
    box.appendChild(card("Restores", m.restores));
    box.appendChild(card("Blocked", m.blocked, pairs(m.blocked_by_reason)));
    box.appendChild(card("Errors", m.errors));
    box.appendChild(card("Feed drops", m.feed_dropped || 0));
    var o = m.overhead_ms || {};
    box.appendChild(card("Overhead p50 / p95", fmtMs(o.p50) + " / " + fmtMs(o.p95), (o.samples || 0) + " samples"));
    box.appendChild(card("Vault size", m.vault_size));
    box.appendChild(card("Uptime", fmtUptime(m.uptime_seconds)));
  }
  function pollMetrics() {
    getJSON("/api/metrics").then(renderMetrics).catch(function () {});
  }

  // Config panel
  function renderConfig(c) {
    $("version").textContent = c.version || "";
    $("upstreams").textContent = Object.keys(c.upstreams || {}).sort().map(function (k) { return k + " -> " + c.upstreams[k]; }).join("  ");
    var kv = $("config");
    clear(kv);
    [
      ["listen", c.listen], ["ui listen", c.ui_listen],
      ["on_error", c.policy.on_error], ["max_body_bytes", c.policy.max_body_bytes],
      ["vault mode", c.vault.mode], ["vault key file", c.vault.key_file || "-"], ["vault file", c.vault.file || "-"]
    ].forEach(function (p) { kv.appendChild(el("dt", null, p[0])); kv.appendChild(el("dd", null, p[1])); });
    var tb = $("rules").tBodies[0];
    clear(tb);
    (c.rules || []).forEach(function (r) {
      var tr = el("tr");
      tr.appendChild(el("td", null, r.name));
      tr.appendChild(el("td", null, r.type));
      tr.appendChild(el("td", null, r.synth || ""));
      tr.appendChild(el("td", "code", r.pattern || r.file || ""));
      tb.appendChild(tr);
    });
  }
  // loadConfig resolves true only when the config was fetched and rendered.
  function loadConfig() {
    return getJSON("/api/config").then(function (c) { renderConfig(c); setBadge(true, "config ok"); return true; })
      .catch(function () { setBadge(false, "config unavailable"); return false; });
  }
  $("reload").addEventListener("click", function () {
    var btn = $("reload"), msg = $("reload-msg");
    btn.disabled = true;
    msg.textContent = "reloading...";
    postJSON("/api/reload").then(function (r) {
      if (r.body.ok) {
        msg.textContent = "reloaded " + new Date().toLocaleTimeString();
        return loadConfig().then(function (ok) { if (ok) setBadge(true, "reload ok"); });
      }
      msg.textContent = "reload failed: " + (r.body.error || "error");
      setBadge(false, "reload failed");
    }).catch(function () {
      msg.textContent = "reload failed";
      setBadge(false, "reload failed");
    }).then(function () { btn.disabled = false; });
  });

  // Live feed
  function evKey(e) { return [e.time, e.method, e.path, e.status, e.bytes_in, e.bytes_out].join("|"); }
  function addRow(e, prepend) {
    var tb = $("feed").tBodies[0];
    var tr = el("tr", e.blocked ? "blocked" : "");
    var t = e.time ? new Date(e.time) : null;
    tr.appendChild(el("td", null, t && !isNaN(t) ? t.toLocaleTimeString() : "\u2014"));
    tr.appendChild(el("td", null, e.adapter || ""));
    tr.appendChild(el("td", "code", e.method + " " + e.path));
    tr.appendChild(el("td", "num", e.status));
    tr.appendChild(el("td", "num", e.bytes_in + " / " + e.bytes_out));
    tr.appendChild(el("td", null, pairs(e.rules)));
    tr.appendChild(el("td", "num", fmtMs(e.latency_ms)));
    tr.appendChild(el("td", null, e.blocked || ""));
    if (prepend) tb.insertBefore(tr, tb.firstChild); else tb.appendChild(tr);
    while (tb.rows.length > MAX_ROWS) tb.removeChild(tb.lastChild);
  }
  function feedStatus(text, bad) {
    var s = $("feed-status");
    s.textContent = text;
    s.className = bad ? "bad" : "muted";
  }

  var MAX_RETRIES = 10, attempts = 0, source = null, retryTimer = null;

  // connect opens the stream first, queues what arrives while /api/events is
  // loading, then renders the snapshot and replays the queue minus duplicates,
  // so no entry falls into the gap between the two requests.
  function connect() {
    if (source) source.close();
    var es = new EventSource("/api/stream");
    source = es;
    var ready = false, queue = [];
    function live(e) { addRow(e, true); }
    es.onopen = function () {
      attempts = 0;
      feedStatus("connected", false);
      getJSON("/api/events").then(function (list) {
        if (source !== es) return;
        var tb = $("feed").tBodies[0], seen = {};
        clear(tb);
        list.slice().reverse().forEach(function (e) { seen[evKey(e)] = true; addRow(e, false); });
        queue.forEach(function (e) { if (!seen[evKey(e)]) live(e); });
        queue = [];
        ready = true;
      }).catch(function () { ready = true; queue.forEach(live); queue = []; });
    };
    es.onmessage = function (ev) {
      var e;
      try { e = JSON.parse(ev.data); } catch (x) { return; }
      if (ready) live(e); else queue.push(e);
    };
    es.onerror = function () {
      if (es.readyState === 2) { // closed for good: back off, bounded
        es.close();
        if (attempts >= MAX_RETRIES) { feedStatus("live feed failed (reload the page)", true); return; }
        var delay = Math.min(30000, 1000 * Math.pow(2, attempts));
        attempts++;
        feedStatus("disconnected, retrying in " + Math.round(delay / 1000) + "s", true);
        retryTimer = setTimeout(connect, delay);
      } else {
        feedStatus("reconnecting...", true);
      }
    };
  }
  function startFeed() { feedStatus("connecting...", false); connect(); }

  // Test box
  $("test-run").addEventListener("click", function () {
    var btn = $("test-run"), out = $("test-out"), ul = $("test-matches");
    btn.disabled = true;
    clear(ul);
    postJSON("/api/test", { text: $("test-input").value }).then(function (r) {
      if (r.status !== 200) { out.textContent = "error: " + (r.body.error || r.status); return; }
      out.textContent = r.body.sanitized;
      (r.body.matches || []).forEach(function (m) { ul.appendChild(el("li", null, m.rule + " (" + m.class + ") x" + m.count)); });
      if (!(r.body.matches || []).length) ul.appendChild(el("li", "muted", "no matches"));
    }).catch(function () { out.textContent = "error"; }).then(function () { btn.disabled = false; });
  });

  loadConfig();
  pollMetrics();
  setInterval(pollMetrics, 2000);
  startFeed();
})();
