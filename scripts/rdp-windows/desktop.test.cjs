"use strict";

// Run with: node --test scripts/rdp-windows/desktop.test.cjs
const test = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const html = readFileSync(path.join(__dirname, "desktop.html"), "utf8");
const quadrantIds = ["top-left", "top-right", "bottom-left", "bottom-right"];
const firstPattern = ["rgb(220, 40, 40)", "rgb(40, 200, 60)", "rgb(40, 80, 220)", "rgb(220, 190, 40)"];
const secondPattern = [firstPattern[2], firstPattern[3], firstPattern[0], firstPattern[1]];

function fixture() {
  const scripts = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)];
  assert.equal(scripts.length, 1);
  const nodes = Object.fromEntries([...quadrantIds, "status"].map((id) => [id, {
    style: {}, textContent: "",
  }]));
  const listeners = new Map();
  const document = {
    body: { dataset: {} },
    getElementById(id) { assert.ok(nodes[id], "unknown fixture element: " + id); return nodes[id]; },
    addEventListener(name, callback) { assert.equal(name, "keydown"); listeners.set(name, callback); },
  };
  vm.runInNewContext(scripts[0][1], { document });
  return {
    colors: () => quadrantIds.map((id) => nodes[id].style.backgroundColor),
    state: () => document.body.dataset.state,
    status: () => nodes.status.textContent,
    press(key, repeat = false) {
      let prevented = false;
      listeners.get("keydown")({ key, repeat, preventDefault() { prevented = true; } });
      return prevented;
    },
  };
}

test("fixture starts with the expected RGB quadrant contract and reload resets it", () => {
  const first = fixture();
  assert.deepEqual(first.colors(), firstPattern);
  assert.equal(first.state(), "0");
  assert.match(first.status(), /state 0/);
  first.press("F8");
  assert.deepEqual(fixture().colors(), firstPattern);
});

test("F8 changes all four quadrants and a second F8 restores the initial state", () => {
  const f = fixture();
  assert.equal(f.press("F8"), true);
  assert.deepEqual(f.colors(), secondPattern);
  assert.equal(f.state(), "1");
  assert.match(f.status(), /state 1/);
  assert.equal(f.press("F8"), true);
  assert.deepEqual(f.colors(), firstPattern);
  assert.equal(f.state(), "0");
});

test("other keys and F8 auto-repeat do not introduce false input transitions", () => {
  const f = fixture();
  assert.equal(f.press("F11"), false);
  assert.deepEqual(f.colors(), firstPattern);
  assert.equal(f.press("F8", true), true);
  assert.deepEqual(f.colors(), firstPattern);
  f.press("F8");
  assert.equal(f.press("F8", true), true);
  assert.deepEqual(f.colors(), secondPattern);
});

test("fixture fills a gap-free viewport without loading external resources", () => {
  assert.match(html, /html, body\s*\{[^}]*margin:\s*0;[^}]*overflow:\s*hidden;/);
  assert.match(html, /#quadrants\s*\{[^}]*position:\s*fixed;[^}]*inset:\s*0;/);
  assert.match(html, /grid-template-columns:\s*1fr 1fr;/);
  assert.match(html, /grid-template-rows:\s*1fr 1fr;/);
  assert.match(html, /gap:\s*0;/);
  assert.equal((html.match(/class="quadrant"/g) || []).length, 4);
  assert.doesNotMatch(html, /\b(?:src|href)\s*=|\b(?:fetch|XMLHttpRequest|localStorage|sessionStorage)\b/);
});
