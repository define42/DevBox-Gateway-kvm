"use strict";

// Run with: node --test internal/gateway/dashboard.test.cjs
const test = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const path = require("node:path");
const { createPerformancePanel } = require("./dashboard.js");

function fixture() {
  const nodes = Object.fromEntries([
    "subtitle", "status", "message", "sampled-at", "metrics", "cpu", "resident", "guest-memory",
    "connections", "encoder", "frames", "fps", "encode", "write", "ack",
  ].map((name) => [name, {
    textContent: "", className: "", attributes: {},
    setAttribute(key, value) { this.attributes[key] = value; },
  }]));
  const requests = [];
  const timers = new Map();
  let visible = true;
  let nextTimer = 0;
  let expired = false;
  const panel = createPerformancePanel({
    nodes,
    fetchSample(url, options) {
      return new Promise((resolve, reject) => requests.push({ url, options, resolve, reject }));
    },
    isVisible: () => visible,
    sessionExpired: (response) => { expired = response.status === 401; return expired; },
    schedule(callback, delay) { const id = ++nextTimer; timers.set(id, { callback, delay }); return id; },
    unschedule(id) { timers.delete(id); },
  });
  return {
    panel, nodes, requests, timers, expired: () => expired,
    visible(value) { visible = value; panel.visibilityChanged(); },
    tick() { const [id, timer] = timers.entries().next().value; timers.delete(id); timer.callback(); },
  };
}

function row(id = "vm-one", state = "running") { return { vmId: id, vmName: "Office " + id, vmState: state }; }

function sample(overrides = {}) {
  return {
    state: "live", sampledAt: "2026-10-18T12:00:00Z", cpuPercent: 185.5,
    residentBytes: 2 * 1024 ** 3, guestMemoryBytes: 4 * 1024 ** 3, connections: 1,
    encoder: "software", framesSent: 420, framesPerSecond: 30, encodeMs: 4.5, writeMs: 1.2, ackMs: 8,
    ...overrides,
  };
}

function reply(request, data = sample(), status = 200) {
  request.resolve({ ok: status === 200, status, json: async () => data });
}

async function settled() { await new Promise((resolve) => setImmediate(resolve)); }

test("performance is opt-in, encodes only the selected VM, and distinguishes zero from null", async () => {
  const f = fixture();
  assert.equal(f.requests.length, 0);
  f.panel.open(row("vm/one"));
  assert.equal(f.requests[0].url, "/vms/vm%2Fone/performance");
  assert.equal(f.requests[0].options.cache, "no-store");
  assert.equal(f.requests[0].options.credentials, "same-origin");
  reply(f.requests[0], sample({ cpuPercent: 185.5, framesPerSecond: 0, encodeMs: null, ackMs: 0 }));
  await settled();
  assert.equal(f.nodes.status.textContent, "Live");
  assert.match(f.nodes.cpu.textContent, /185/);
  assert.equal(f.nodes.fps.textContent, "0");
  assert.equal(f.nodes.encode.textContent, "Not sampled");
  assert.equal(f.nodes.ack.textContent, "0 ms");
  assert.equal(f.nodes.resident.textContent, "2 GiB");
  assert.equal(f.nodes["guest-memory"].textContent, "4 GiB");
  assert.equal(f.timers.size, 1);
  assert.equal([...f.timers.values()][0].delay, 5000);
  f.panel.close();
});

test("only one request is in flight and a close clears both values and timers", async () => {
  const f = fixture();
  f.panel.open(row());
  f.panel.update(row());
  assert.equal(f.requests.length, 1);
  assert.equal(f.timers.size, 0);
  reply(f.requests[0]);
  await settled();
  f.tick();
  assert.equal(f.requests.length, 2);
  assert.equal(f.timers.size, 0);
  f.panel.close();
  assert.equal(f.requests[1].options.signal.aborted, true);
  assert.equal(f.nodes.cpu.textContent, "Not sampled");
  reply(f.requests[1]);
  await settled();
  assert.equal(f.nodes.cpu.textContent, "Not sampled");
  assert.equal(f.timers.size, 0);
});

test("late responses cannot cross VM selection and cancelled work settles before the next fetch", async () => {
  const f = fixture();
  f.panel.open(row("first"));
  f.panel.open(row("second"));
  assert.equal(f.requests[0].options.signal.aborted, true);
  assert.equal(f.requests.length, 1);
  reply(f.requests[0]);
  await settled();
  assert.equal(f.nodes.cpu.textContent, "Not sampled");
  assert.equal(f.requests.length, 2);
  assert.equal(f.requests[1].url, "/vms/second/performance");
  reply(f.requests[1], sample({ cpuPercent: 12 }));
  await settled();
  assert.equal(f.nodes.cpu.textContent, "12%");
  assert.equal(f.nodes.subtitle.textContent, "Office second");
  f.panel.close();
});

test("hidden pages abort and clear, then fetch fresh data on return", async () => {
  const f = fixture();
  f.panel.open(row());
  reply(f.requests[0]);
  await settled();
  f.visible(false);
  assert.equal(f.timers.size, 0);
  assert.equal(f.nodes.status.textContent, "Paused");
  assert.equal(f.nodes.cpu.textContent, "Not sampled");
  f.visible(true);
  assert.equal(f.requests.length, 2);
  f.visible(false);
  assert.equal(f.requests[1].options.signal.aborted, true);
  reply(f.requests[1]);
  await settled();
  assert.equal(f.nodes.status.textContent, "Paused");
  assert.equal(f.timers.size, 0);
  f.panel.close();
});

test("inventory refresh preserves samples but a restart clears them immediately", async () => {
  const f = fixture();
  f.panel.open(row());
  reply(f.requests[0]);
  await settled();
  f.panel.update({ ...row(), vmName: "Renamed" });
  assert.equal(f.nodes.cpu.textContent, "185.5%");
  assert.equal(f.nodes.subtitle.textContent, "Renamed");
  assert.equal(f.requests.length, 1);
  f.panel.update(row("vm-one", "starting"));
  assert.equal(f.nodes.cpu.textContent, "Not sampled");
  assert.equal(f.nodes.frames.textContent, "Not sampled");
  assert.equal(f.requests.length, 2);
  reply(f.requests[1], sample({ state: "starting", sampledAt: null }));
  await settled();
  assert.equal(f.nodes.status.textContent, "Starting");
  assert.equal(f.nodes.cpu.textContent, "Unavailable");
  f.panel.close();
});

test("stale and stopped responses hide runtime numbers instead of claiming zero", async () => {
  for (const state of ["stale", "stopped", "unavailable"]) {
    const f = fixture();
    f.panel.open(row());
    reply(f.requests[0], sample({ state }));
    await settled();
    assert.equal(f.nodes.status.textContent.toLowerCase(), state);
    assert.equal(f.nodes.cpu.textContent, "Unavailable");
    assert.equal(f.nodes.frames.textContent, "Unavailable");
    assert.equal(f.nodes["guest-memory"].textContent, "4 GiB");
    f.panel.close();
  }
});

test("failed or malformed samples clear old metrics and retry without displaying server text", async () => {
  for (const failure of [null, sample({ cpuPercent: NaN }), sample({ state: "<script>" }), sample({ encoder: "<img>" })]) {
    const f = fixture();
    f.panel.open(row());
    reply(f.requests[0]);
    await settled();
    f.tick();
    if (failure === null) f.requests[1].reject(new Error("private backend failure"));
    else reply(f.requests[1], failure);
    await settled();
    assert.equal(f.nodes.status.textContent, "Unavailable");
    assert.equal(f.nodes.cpu.textContent, "Not sampled");
    assert.equal(f.nodes["sampled-at"].textContent, "Not sampled");
    assert.doesNotMatch(f.nodes.message.textContent, /private|script|img/);
    assert.equal(f.timers.size, 1);
    f.panel.close();
  }
});

test("expired sessions stop performance polling", async () => {
  const f = fixture();
  f.panel.open(row());
  reply(f.requests[0], null, 401);
  await settled();
  assert.equal(f.expired(), true);
  assert.equal(f.timers.size, 0);
  assert.equal(f.nodes.cpu.textContent, "Not sampled");
});

test("unchanged live samples do not repeat live-region state announcements", async () => {
  const f = fixture();
  f.panel.open(row());
  reply(f.requests[0]);
  await settled();
  const changes = [];
  let value = f.nodes.status.textContent;
  Object.defineProperty(f.nodes.status, "textContent", {
    get: () => value,
    set(next) { value = next; changes.push(next); },
  });
  f.tick();
  reply(f.requests[1], sample({ cpuPercent: 22 }));
  await settled();
  assert.deepEqual(changes, []);
  assert.equal(f.nodes.cpu.textContent, "22%");
  f.panel.close();
});

// These verify the server template/controller contract. The DOM-independent
// lifecycle tests above do not replace a browser layout or screen-reader test.
test("dashboard markup wires the accessible, progressively enhanced performance dialog", () => {
  const html = readFileSync(path.join(__dirname, "dashboard.html"), "utf8");
  const script = readFileSync(path.join(__dirname, "dashboard.js"), "utf8");
  const css = readFileSync(path.join(__dirname, "static/dashboard.css"), "utf8");
  assert.match(html, /<button[^>]*data-performance[^>]*aria-haspopup="dialog"[^>]*aria-controls="performance-modal"[^>]*hidden>/);
  assert.match(html, /id="performance-modal"[^>]*hidden aria-hidden="true"/);
  assert.match(html, /role="dialog" aria-modal="true" aria-labelledby="performance-title" aria-describedby="performance-description" tabindex="-1"/);
  assert.match(html, /id="performance-status" role="status" aria-live="polite" aria-atomic="true"/);
  assert.match(html, /<noscript>[\s\S]*Enable JavaScript to view live DevBox performance/);
  for (const name of Object.keys(fixture().nodes)) {
    assert.equal(html.split('id="performance-' + name + '"').length - 1, 1, name + " must have exactly one DOM node");
  }
  assert.match(script, /querySelectorAll\("\[data-info\], \[data-performance\]"\)/);
  assert.match(script, /main\.inert = true/);
  assert.match(script, /main\.inert = false/);
  assert.match(script, /event\.key === "Escape"[\s\S]*closeModal\(\)/);
  assert.match(script, /const target = previousFocus[\s\S]*replacement \|\| openCreate;\s*target\.focus\(\)/);
  assert.match(script, /if \(row\) vmPerformance\.update\(row\.dataset\);\s*else closeModal\(\)/);
  assert.match(css, /@media \(max-width: 575\.98px\)\s*\{\s*\.performance-grid\s*\{\s*grid-template-columns: minmax\(0, 1fr\)/);
});
