"use strict";

// Run with: node --test internal/gateway/admin.test.cjs
const test = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = readFileSync(path.join(__dirname, "admin.js"), "utf8");

function fixture() {
  let document;
  function element() {
    return {
      textContent: "", hidden: false, disabled: false, dataset: {}, attributes: {},
      childNodes: [], listeners: {}, classList: { add() {}, remove() {} },
      setAttribute(name, value) { this.attributes[name] = String(value); },
      removeAttribute(name) { delete this.attributes[name]; },
      addEventListener(name, handler) { this.listeners[name] = handler; },
      querySelectorAll() { return []; },
      replaceChildren(...children) { this.childNodes = children; },
      contains() { return false; },
      focus() { document.activeElement = this; },
      dispatch(name) { this.listeners[name]({ preventDefault() {}, target: this }); },
    };
  }

  const nodes = Object.fromEntries([
    "admin-main", "vm-list", "image-list", "base-images-panel", "base-images-dialog",
    "base-images-button", "base-images-body", "image-upload-form", "image-file",
    "image-error", "image-message", "image-progress", "image-progress-bar",
    "image-progress-label", "image-progress-detail", "image-upload-cancel", "action-area",
  ].map((id) => [id, element()]));
  const form = nodes["image-upload-form"];
  const file = nodes["image-file"];
  const bar = nodes["image-progress-bar"];
  const previouslyDisabled = element();
  previouslyDisabled.disabled = true;
  const controls = [nodes["base-images-button"], file, nodes["image-upload-cancel"], previouslyDisabled];
  const limit = element();
  const csrf = { value: "csrf-token", defaultValue: "csrf-token" };
  const requests = [];
  nodes["admin-main"].dataset = { user: "admin", maxUploadBytes: "10000000" };
  nodes["image-message"].hidden = true;
  nodes["image-error"].hidden = true;
  nodes["image-progress"].hidden = true;
  file.files = [{ name: "base.qcow2", size: 1000000 }];
  form.action = "/admin/images";
  form.elements = { csrf };
  form.resets = 0;
  form.reset = () => { form.resets++; file.files = []; };
  Object.defineProperty(bar, "value", {
    get() { return Number(this.attributes.value || 0); },
    set(value) { this.setAttribute("value", value); },
  });

  document = {
    // Suppress unrelated periodic latency and catalog requests in this fixture.
    hidden: true,
    activeElement: null,
    body: element(),
    getElementById: (id) => nodes[id],
    createElement: element,
    importNode: (node) => node,
    addEventListener() {},
    querySelector: () => limit,
    querySelectorAll(selector) {
      if (selector === "button, input[type=file]") return controls;
      if (selector === 'input[name="csrf"]') return [csrf];
      return [];
    },
  };
  class XMLHttpRequest {
    constructor() {
      this.upload = {};
      this.headers = {};
      requests.push(this);
    }
    open(method, url) {
      this.method = method;
      this.url = url;
      this.uploadHandlersAtOpen = { progress: this.upload.onprogress, load: this.upload.onload };
    }
    setRequestHeader(name, value) { this.headers[name] = value; }
    send(body) { this.body = body; }
    abort() { this.onabort(); this.onloadend(); }
    progress(loaded, total, lengthComputable = true) {
      this.upload.onprogress({ loaded, total, lengthComputable });
    }
    respond(status = 200, text = "catalog") {
      this.status = status;
      this.responseText = text;
      this.responseURL = "https://gateway.test/admin";
      this.onload();
      this.onloadend();
    }
    fail() { this.onerror(); this.onloadend(); }
  }
  class DOMParser {
    parseFromString() {
      const nextMain = element();
      nextMain.dataset = { user: "admin", maxUploadBytes: "10000000" };
      const nextImages = element();
      nextImages.childNodes = [{ image: "imported-base" }];
      return {
        getElementById: (id) => ({ "admin-main": nextMain, "vm-list": element(), "image-list": nextImages })[id],
        querySelector: () => csrf,
      };
    }
  }
  vm.runInNewContext(source, {
    document, XMLHttpRequest, DOMParser, URL,
    FormData: class {
      constructor(value) {
        assert.equal(value, form);
        assert.equal(file.disabled, false, "capture the file before disabling the form");
      }
    },
    window: {
      addEventListener() {}, setInterval() {},
      location: { assign() {}, reload() {} },
    },
    location: { href: "https://gateway.test/admin", hash: "" },
  }, { filename: "admin.js" });

  return {
    nodes, form, file, bar, controls, requests,
    start() { form.dispatch("submit"); return requests.at(-1); },
    notice() { return nodes["action-area"].childNodes[0].textContent; },
  };
}

function assertPreparing(f) {
  assert.equal(f.nodes["image-progress"].hidden, false);
  assert.equal(f.bar.attributes.value, undefined, "preparation uses an indeterminate progress bar");
  assert.match(f.nodes["image-progress-label"].textContent, /Validating and preparing/);
  assert.match(f.nodes["image-progress-detail"].textContent, /may take several minutes/);
  assert.equal(f.nodes["image-message"].hidden, true, "transfer completion is not import success");
  assert.equal(f.form.resets, 0);
  assert.equal(f.file.disabled, true);
}

function assertFinished(f) {
  assert.equal(f.nodes["image-progress"].hidden, true);
  assert.equal(f.form.attributes["aria-busy"], "false");
  assert.equal(f.nodes["admin-main"].attributes["aria-busy"], "false");
  assert.deepEqual(f.controls.map((control) => control.disabled), [false, false, false, true]);
}

test("upload handlers are attached before opening the request", () => {
  const f = fixture();
  const xhr = f.start();
  assert.equal(typeof xhr.uploadHandlersAtOpen.progress, "function");
  assert.equal(typeof xhr.uploadHandlersAtOpen.load, "function");
  assert.equal(xhr.method, "POST");
  assert.equal(xhr.url, f.form.action);
  assert.equal(xhr.headers["X-CSRF-Token"], "csrf-token");
  assert.equal(f.file.disabled, true);
  assert.equal(f.nodes["image-upload-cancel"].disabled, false);
});

test("an incomplete transfer never rounds up to 100 percent", () => {
  const f = fixture();
  const xhr = f.start();
  xhr.progress(500, 1000);
  assert.equal(f.bar.value, 50);
  xhr.progress(999, 1000);
  assert.equal(f.bar.value, 99);
  assert.equal(f.nodes["image-progress-label"].textContent, "Uploading image: 99%");
  assert.equal(f.nodes["image-message"].hidden, true);
});

test("the last progress event immediately explains preparation while the server is still busy", () => {
  for (const loaded of [1000, 1001]) {
    const f = fixture();
    const xhr = f.start();
    xhr.progress(loaded, 1000);
    assertPreparing(f);
    assert.match(f.notice(), /Validating and preparing/);
    assert.equal(f.requests.length, 1);
  }
});

test("unknown and zero totals remain indeterminate until the upload load event", () => {
  for (const [total, lengthComputable] of [[1000, false], [0, true]]) {
    const f = fixture();
    const xhr = f.start();
    xhr.progress(500, 1000);
    xhr.progress(0, total, lengthComputable);
    assert.equal(f.bar.attributes.value, undefined);
    assert.equal(f.nodes["image-progress-label"].textContent, "Uploading image...");
    xhr.upload.onload();
    assertPreparing(f);
  }
});

test("late progress cannot replace the preparation state", () => {
  const f = fixture();
  const xhr = f.start();
  xhr.upload.onload();
  xhr.progress(800, 1000);
  assertPreparing(f);
  xhr.progress(0, 0, false);
  assertPreparing(f);
});

test("only a successful server response adds the image and completes the import", () => {
  const f = fixture();
  const xhr = f.start();
  xhr.upload.onload();
  assertPreparing(f);
  assert.equal(f.nodes["image-list"].childNodes.length, 0);
  xhr.respond();
  assertFinished(f);
  assert.equal(f.nodes["image-message"].hidden, false);
  assert.match(f.nodes["image-message"].textContent, /Base image added/);
  assert.equal(f.nodes["image-list"].childNodes[0].image, "imported-base");
  assert.equal(f.form.resets, 1);
});

test("server rejection and connection failure release controls without claiming success", () => {
  for (const failure of ["response", "connection"]) {
    const f = fixture();
    const xhr = f.start();
    xhr.upload.onload();
    if (failure === "response") xhr.respond(422, "Invalid QCOW2 image");
    else xhr.fail();
    assertFinished(f);
    assert.equal(f.nodes["image-error"].hidden, false);
    assert.match(f.nodes["image-error"].textContent, failure === "response" ? /Invalid QCOW2/ : /connection was interrupted/);
    assert.equal(f.nodes["image-message"].hidden, true);
    assert.equal(f.form.resets, 0);
  }
});

test("cancellation releases controls and retry starts with fresh transfer progress", () => {
  const f = fixture();
  const first = f.start();
  first.upload.onload();
  f.nodes["image-upload-cancel"].dispatch("click");
  assertFinished(f);
  assert.match(f.nodes["image-message"].textContent, /Upload canceled/);
  const retry = f.start();
  assert.notEqual(retry, first);
  assert.equal(f.bar.value, 0);
  assert.equal(f.nodes["image-progress-label"].textContent, "Uploading image...");
  assert.equal(f.nodes["image-message"].hidden, true);
  retry.progress(250, 1000);
  assert.equal(f.bar.value, 25);
  first.progress(1000, 1000);
  first.upload.onload();
  assert.equal(f.bar.value, 25);
  assert.equal(f.nodes["image-progress-label"].textContent, "Uploading image: 25%");
});
