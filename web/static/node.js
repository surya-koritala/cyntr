// Reference web client for the Cyntr companion-app node protocol (B10).
//
// Protocol (one WebSocket, JSON frames):
//   1. client -> {type:"pair", code, node}        // authenticate with code
//   2. client -> {type:"hello", capabilities:[…]} // offer capabilities
//   3. server -> {type:"welcome", session, tenant, capabilities:[…]}
//   4. client <-> sample frames (voice/canvas/ping), server acks/errors.
//
// The server binds tenant + session from the validated pairing code; this
// client never asserts its own tenant. A bad/missing code fails closed: the
// server replies with an "error" frame and closes the socket.
//
// Native macOS/iOS/Android companion apps are deferred to separate projects.
// This file is the canonical reference for what those clients must speak.

(function () {
  "use strict";

  const $ = (id) => document.getElementById(id);
  const logEl = $("log");
  let ws = null;
  let negotiated = [];

  // Default the URL to the current origin's node endpoint.
  $("url").value =
    (location.protocol === "https:" ? "wss://" : "ws://") +
    location.host +
    "/api/v1/node/ws";

  function log(dir, obj) {
    const line = document.createElement("div");
    line.className = dir;
    const tag = { in: "<<", out: ">>", err: "!!", sys: "--" }[dir] || "--";
    line.textContent =
      tag + " " + (typeof obj === "string" ? obj : JSON.stringify(obj));
    logEl.appendChild(line);
    logEl.scrollTop = logEl.scrollHeight;
  }

  function send(frame) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify(frame));
    log("out", frame);
  }

  function setConnected(on) {
    $("connect").disabled = on;
    $("disconnect").disabled = !on;
    $("status").textContent = on ? "connected" : "disconnected";
    for (const id of ["sendVoice", "sendCanvas", "ping"]) $(id).disabled = !on;
  }

  function selectedCaps() {
    return Array.from(document.querySelectorAll(".cap:checked")).map(
      (c) => c.value
    );
  }

  function connect() {
    const url = $("url").value.trim();
    const code = $("code").value.trim();
    const node = $("node").value.trim() || "web-reference";
    if (!code) {
      log("err", "a pairing code is required (fail closed)");
      return;
    }
    negotiated = [];
    $("session").textContent = "negotiating…";

    try {
      ws = new WebSocket(url);
    } catch (e) {
      log("err", "could not open socket: " + e.message);
      return;
    }

    ws.onopen = () => {
      log("sys", "socket open — pairing");
      // Phase 1: authenticate with the pairing code.
      send({ type: "pair", code: code, node: node });
      // Phase 2: offer capabilities for negotiation.
      send({ type: "hello", capabilities: selectedCaps() });
    };

    ws.onmessage = (ev) => {
      let frame;
      try {
        frame = JSON.parse(ev.data);
      } catch {
        log("err", "non-JSON frame: " + ev.data);
        return;
      }
      log("in", frame);
      switch (frame.type) {
        case "welcome":
          negotiated = frame.capabilities || [];
          setConnected(true);
          $("session").innerHTML =
            'tenant <span class="badge">' +
            (frame.tenant || "?") +
            "</span> session <span class=badge>" +
            (frame.session || "?") +
            "</span> caps [" +
            negotiated.join(", ") +
            "]";
          break;
        case "error":
          log("err", (frame.error_code || "ERROR") + ": " + (frame.message || ""));
          break;
      }
    };

    ws.onclose = () => {
      log("sys", "socket closed");
      setConnected(false);
    };
    ws.onerror = () => log("err", "socket error");
  }

  function disconnect() {
    if (ws) ws.close();
  }

  $("connect").onclick = connect;
  $("disconnect").onclick = disconnect;

  $("sendVoice").onclick = () => {
    if (!negotiated.includes("voice")) {
      log("err", "voice not negotiated — server will reject");
    }
    // A real client would send base64 audio in `audio`; here a transcript stub.
    send({ type: "voice", transcript: "hello from the reference node" });
  };

  $("sendCanvas").onclick = () => {
    if (!negotiated.includes("canvas")) {
      log("err", "canvas not negotiated — server will reject");
    }
    send({
      type: "canvas",
      canvas: { op: "stroke", points: [[0, 0], [10, 10], [20, 5]] },
    });
  };

  $("ping").onclick = () => send({ type: "ping" });
})();
