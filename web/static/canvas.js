// Cyntr live canvas (A2UI) reference renderer (ticket B9).
//
// Connects to the canvas WebSocket endpoint scoped to a (tenant, session),
// replays the persisted doc on connect, and re-renders on every live update.
// The renderer is intentionally minimal and supports exactly the node types
// the server validates: text, markdown, table, image, button. Unknown node
// types are skipped (the server already rejects them, but we fail safe).
(function () {
  "use strict";

  var ws = null;

  function el(id) { return document.getElementById(id); }

  function setStatus(text, cls) {
    var s = el("status");
    s.textContent = text;
    s.className = cls || "";
  }

  // Minimal, safe markdown: headings, bold, inline code, line breaks.
  // We escape first, then apply a tiny allowlist of transforms — never
  // innerHTML of raw model output.
  function escapeHtml(str) {
    return String(str)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  function renderMarkdown(text) {
    var html = escapeHtml(text)
      .replace(/^### (.*)$/gm, "<h3>$1</h3>")
      .replace(/^## (.*)$/gm, "<h2>$1</h2>")
      .replace(/^# (.*)$/gm, "<h1>$1</h1>")
      .replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>")
      .replace(/`([^`]+)`/g, "<code>$1</code>")
      .replace(/\n/g, "<br/>");
    var div = document.createElement("div");
    div.className = "node";
    div.innerHTML = html;
    return div;
  }

  function renderNode(node) {
    var div = document.createElement("div");
    div.className = "node";
    switch (node.type) {
      case "text": {
        var p = document.createElement("p");
        p.textContent = node.text || "";
        div.appendChild(p);
        return div;
      }
      case "markdown":
        return renderMarkdown(node.text || "");
      case "table": {
        var table = document.createElement("table");
        var thead = document.createElement("thead");
        var htr = document.createElement("tr");
        (node.columns || []).forEach(function (c) {
          var th = document.createElement("th");
          th.textContent = c;
          htr.appendChild(th);
        });
        thead.appendChild(htr);
        table.appendChild(thead);
        var tbody = document.createElement("tbody");
        (node.rows || []).forEach(function (row) {
          var tr = document.createElement("tr");
          row.forEach(function (cell) {
            var td = document.createElement("td");
            td.textContent = cell;
            tr.appendChild(td);
          });
          tbody.appendChild(tr);
        });
        table.appendChild(tbody);
        div.appendChild(table);
        return div;
      }
      case "image": {
        var img = document.createElement("img");
        img.src = node.url || "";
        img.alt = node.alt || "";
        div.appendChild(img);
        return div;
      }
      case "button": {
        var btn = document.createElement("button");
        btn.textContent = node.label || "Button";
        btn.addEventListener("click", function () {
          // Reference renderer: surface the declared action. A real dashboard
          // would route this back to the agent over its own channel.
          console.log("canvas button action:", node.action);
          setStatus("button: " + (node.action || node.label), "ok");
        });
        div.appendChild(btn);
        return div;
      }
      default:
        return null; // unknown type: fail safe, render nothing
    }
  }

  function render(doc) {
    el("title").textContent = doc.title || "";
    var root = el("canvas");
    root.innerHTML = "";
    if (!doc.nodes || doc.nodes.length === 0) {
      root.innerHTML = '<p class="empty">Empty canvas.</p>';
      return;
    }
    doc.nodes.forEach(function (n) {
      var node = renderNode(n);
      if (node) root.appendChild(node);
    });
  }

  function connect() {
    if (ws) { ws.close(); ws = null; }
    var tenant = encodeURIComponent(el("tenant").value.trim());
    var session = encodeURIComponent(el("session").value.trim());
    var key = encodeURIComponent(el("key").value.trim());
    var proto = location.protocol === "https:" ? "wss://" : "ws://";
    var url = proto + location.host +
      "/api/v1/tenants/" + tenant + "/canvas/" + session + "/ws" +
      (key ? "?key=" + key : "");

    setStatus("connecting…");
    ws = new WebSocket(url);

    ws.onopen = function () { setStatus("connected", "ok"); };
    ws.onclose = function () { setStatus("disconnected"); };
    ws.onerror = function () { setStatus("error (auth or network)", "err"); };
    ws.onmessage = function (ev) {
      try {
        render(JSON.parse(ev.data));
      } catch (e) {
        console.error("bad canvas message", e);
      }
    };
  }

  el("connect").addEventListener("click", connect);
})();
