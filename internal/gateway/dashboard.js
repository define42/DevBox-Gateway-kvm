// Dashboard presentation follows DevBox Gateway. All VM mutations retain the
// gateway's same-origin, CSRF-protected form endpoints.
(() => {
  "use strict";

  // Keep polling scoped to one visible dialog. A cancelled request retains its
  // slot until it settles, so closing/reopening cannot overlap requests or let
  // an old VM's response overwrite a newer selection.
  function createPerformancePanel({ nodes, fetchSample, isVisible, sessionExpired,
    schedule = setTimeout, unschedule = clearTimeout }) {
    const metrics = ["cpu", "resident", "guest-memory", "connections", "encoder", "frames", "fps", "encode", "write", "ack"];
    const statuses = {
      stopped: ["Stopped", "secondary", "This DevBox is stopped. Runtime measurements are unavailable."],
      starting: ["Starting", "secondary", "Waiting for the new VM process to produce a sample."],
      live: ["Live", "success", "Measurements describe this VM process. Frame rates and timings cover the latest sampling interval."],
      stale: ["Stale", "warning", "The last sample is out of date. Measurements are hidden until a fresh sample arrives."],
      unavailable: ["Unavailable", "secondary", "Performance measurements are unavailable. This panel will retry automatically."],
    };
    const encoders = { "": "Not active", bitmap: "Bitmap", software: "Software H.264", nvenc: "NVIDIA NVENC", mixed: "Mixed" };
    const number = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
    let selected = null;
    let generation = 0;
    let timer = null;
    let active = null;

    function text(name, value) {
      if (nodes[name].textContent !== value) nodes[name].textContent = value;
    }

    function status(label, tone, message) {
      text("status", label);
      nodes.status.className = "badge text-bg-" + tone;
      text("message", message);
    }

    function clear(label = "Loading", message = "Waiting for a performance sample.") {
      for (const name of metrics) text(name, "Not sampled");
      text("sampled-at", "Not sampled");
      status(label, "secondary", message);
    }

    function invalidate() {
      generation++;
      if (timer !== null) unschedule(timer);
      timer = null;
      if (active) active.abort();
    }

    function finite(value) { return typeof value === "number" && Number.isFinite(value) && value >= 0; }
    function count(value) { return finite(value) && Number.isSafeInteger(value); }

    function validate(sample) {
      if (!sample || typeof sample !== "object" || Array.isArray(sample) || !Object.hasOwn(statuses, sample.state) ||
        !Object.hasOwn(encoders, sample.encoder) || !count(sample.guestMemoryBytes) ||
        !count(sample.connections) || !count(sample.framesSent) ||
        (sample.sampledAt !== null && (typeof sample.sampledAt !== "string" || !Number.isFinite(Date.parse(sample.sampledAt))))) {
        throw new Error("Invalid performance sample");
      }
      for (const name of ["cpuPercent", "framesPerSecond", "encodeMs", "writeMs", "ackMs"]) {
        if (sample[name] !== null && !finite(sample[name])) throw new Error("Invalid performance measurement");
      }
      if (sample.residentBytes !== null && !count(sample.residentBytes)) throw new Error("Invalid resident memory");
      return sample;
    }

    function format(value, unit = "") { return value === null ? "Not sampled" : number.format(value) + unit; }

    function bytes(value) {
      if (value === null) return "Not sampled";
      const gib = value >= 1024 ** 3;
      return format(value / (gib ? 1024 ** 3 : 1024 ** 2), gib ? " GiB" : " MiB");
    }

    function render(sample) {
      status(...statuses[sample.state]);
      text("sampled-at", sample.sampledAt === null ? "Not sampled" : new Date(sample.sampledAt).toLocaleString());
      text("guest-memory", bytes(sample.guestMemoryBytes));
      if (sample.state !== "live") {
        for (const name of metrics) if (name !== "guest-memory") text(name, "Unavailable");
        return;
      }
      text("cpu", format(sample.cpuPercent, "%"));
      text("resident", bytes(sample.residentBytes));
      text("connections", number.format(sample.connections));
      text("encoder", encoders[sample.encoder]);
      text("frames", number.format(sample.framesSent));
      text("fps", format(sample.framesPerSecond));
      text("encode", format(sample.encodeMs, " ms"));
      text("write", format(sample.writeMs, " ms"));
      text("ack", format(sample.ackMs, " ms"));
    }

    async function poll() {
      if (!selected || !isVisible() || active) return;
      const current = generation;
      const id = selected.id;
      const controller = new AbortController();
      active = controller;
      nodes.metrics.setAttribute("aria-busy", "true");
      try {
        const response = await fetchSample("/vms/" + encodeURIComponent(id) + "/performance", {
          credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" },
          signal: AbortSignal.any([controller.signal, AbortSignal.timeout(5000)]),
        });
        if (generation !== current || !selected || !isVisible()) return;
        if (sessionExpired(response)) { close(); return; }
        if (!response.ok) throw new Error("Performance request failed");
        const sample = validate(await response.json());
        if (generation === current && selected && isVisible()) render(sample);
      } catch {
        if (generation === current && selected && isVisible()) {
          clear("Unavailable", "Could not load performance measurements. This panel will retry automatically.");
        }
      } finally {
        active = null;
        nodes.metrics.setAttribute("aria-busy", "false");
        if (selected && isVisible()) {
          if (generation !== current) void poll();
          else timer = schedule(() => { timer = null; void poll(); }, 5000);
        }
      }
    }

    function open(row) {
      invalidate();
      selected = { id: row.vmId, state: row.vmState };
      text("subtitle", row.vmName);
      clear();
      void poll();
    }

    function close() {
      invalidate();
      selected = null;
      clear();
    }

    function update(row) {
      if (!selected) return;
      text("subtitle", row.vmName);
      if (selected.state !== row.vmState) {
        invalidate();
        selected.state = row.vmState;
        clear("Updating", "The DevBox state changed. Waiting for a fresh performance sample.");
        void poll();
      }
    }

    function visibilityChanged() {
      if (!selected) return;
      invalidate();
      clear(isVisible() ? "Loading" : "Paused", isVisible()
        ? "Waiting for a fresh performance sample."
        : "Updates are paused while this page is hidden.");
      void poll();
    }

    return { open, close, update, visibilityChanged };
  }

  // The controller can be exercised with Node's built-in test runner without
  // adding browser packages or exposing a frontend API in the running page.
  if (typeof document === "undefined") {
    if (typeof module !== "undefined") module.exports = { createPerformancePanel };
    return;
  }

  const main = document.getElementById("dashboard-main");
  const list = document.getElementById("vm-list");
  const actions = document.getElementById("action-area");
  const createForm = document.getElementById("create-form");
  const openCreate = document.getElementById("open-create-button");
  let modal = null;
  let previousFocus = null;
  let pending = false;
  let creationActive = false;
  let creationPercent = 0;
  let mutationGeneration = 0;
  let refreshing = false;
  let measuring = false;
  let previousRTT = null;
  let jitter = 0;
  let disabledButtons = new Map();
  let imagesAvailable = Array.from(document.getElementById("vm-base-image").options)
    .some((option) => option.value !== "");
  const performanceNodes = Object.fromEntries([
    "subtitle", "status", "message", "sampled-at", "metrics", "cpu", "resident", "guest-memory",
    "connections", "encoder", "frames", "fps", "encode", "write", "ack",
  ].map((name) => [name, document.getElementById("performance-" + name)]));
  const vmPerformance = createPerformancePanel({
    nodes: performanceNodes, fetchSample: (...args) => fetch(...args), sessionExpired,
    isVisible: () => !document.hidden && modal?.id === "performance-modal",
  });

  function enableInfo() {
    list.querySelectorAll("[data-info], [data-performance]").forEach((button) => { button.hidden = false; });
  }

  function showModal(id, focusID) {
    if (pending) return;
    if (modal) closeModal();
    previousFocus = document.activeElement;
    modal = document.getElementById(id);
    modal.hidden = false;
    modal.setAttribute("aria-hidden", "false");
    main.inert = true;
    document.body.classList.add("gateway-modal-open");
    (document.getElementById(focusID) || modal.querySelector("[role=dialog]")).focus();
  }

  function closeModal() {
    if (!modal || creationActive) return;
    const performanceVM = modal.id === "performance-modal" ? modal.dataset.vmId : null;
    if (modal.id === "performance-modal") vmPerformance.close();
    modal.hidden = true;
    modal.setAttribute("aria-hidden", "true");
    modal = null;
    main.inert = false;
    document.body.classList.remove("gateway-modal-open");
    const replacement = performanceVM && Array.from(list.querySelectorAll("[data-vm-id]"))
      .find((row) => row.dataset.vmId === performanceVM)?.querySelector("[data-performance]");
    const target = previousFocus && previousFocus.isConnected ? previousFocus : replacement || openCreate;
    target.focus();
  }

  function notice(message, kind = "secondary", spinning = false) {
    const alert = document.createElement("div");
    alert.className = `alert alert-${kind} mb-0`;
    if (spinning) {
      const spinner = document.createElement("span");
      spinner.className = "spinner-border spinner-border-sm me-2";
      spinner.setAttribute("aria-hidden", "true");
      alert.append(spinner);
    }
    alert.append(document.createTextNode(message));
    actions.replaceChildren(alert);
  }

  function updateCreateAvailability() {
    const disabled = pending || !imagesAvailable;
    openCreate.setAttribute("aria-disabled", String(disabled));
    openCreate.classList.toggle("disabled", disabled);
    openCreate.tabIndex = disabled ? -1 : 0;
    document.getElementById("empty-images-notice").hidden = imagesAvailable;
  }

  function busy(value, creating = false) {
    pending = value;
    updateCreateAvailability();
    if (value) {
      disabledButtons = new Map();
      const controls = Array.from(document.querySelectorAll("button"));
      if (creating) controls.push(...createForm.querySelectorAll('input:not([type="hidden"]), select'));
      controls.forEach((control) => {
        if (!creating && control.hasAttribute("data-close-modal")) return;
        disabledButtons.set(control, control.disabled);
        control.disabled = true;
      });
    } else {
      disabledButtons.forEach((disabled, button) => { button.disabled = disabled; });
      disabledButtons.clear();
    }
  }

  function sessionExpired(response) {
    if (response.status === 401 || new URL(response.url).pathname === "/login") {
      creationActive = false;
      window.location.assign("/login");
      return true;
    }
    return false;
  }

  // Parse server-rendered markup into an inert document. Only the trusted VM
  // inventory is imported, and refreshed CSRF fields are copied as plain values.
  function updateInventory(html) {
    const page = new DOMParser().parseFromString(html, "text/html");
    const next = page.getElementById("vm-list");
    const nextMain = page.getElementById("dashboard-main");
    if (!next || !nextMain) throw new Error("The gateway returned an unexpected page. Refresh and try again.");
    // Another tab may have changed the signed-in account. Reload all account
    // context before adopting its CSRF token, inventory, or provisioning fields.
    if (nextMain.dataset.user !== main.dataset.user || nextMain.dataset.admin !== main.dataset.admin) {
      creationActive = false;
      window.location.reload();
      return false;
    }
    const csrf = page.querySelector('input[name="csrf"]');
    if (csrf) {
      document.querySelectorAll('input[name="csrf"]').forEach((input) => { input.value = input.defaultValue = csrf.value; });
    }
    list.replaceChildren(...Array.from(next.childNodes, (node) => document.importNode(node, true)));
    updateImages(page);
    enableInfo();
    if (modal && modal.id === "info-modal") {
      const row = Array.from(list.querySelectorAll("[data-vm-id]"))
        .find((item) => item.dataset.vmId === modal.dataset.vmId);
      if (row) updateInfo(row);
      else closeModal();
    }
    if (modal && modal.id === "performance-modal") {
      const row = Array.from(list.querySelectorAll("[data-vm-id]"))
        .find((item) => item.dataset.vmId === modal.dataset.vmId);
      if (row) vmPerformance.update(row.dataset);
      else closeModal();
    }
    return true;
  }

  function updateImages(page) {
    const select = document.getElementById("vm-base-image");
    const next = page.getElementById("vm-base-image");
    if (!next || select.innerHTML === next.innerHTML) return;
    const selected = select.value;
    const needsChoice = select.selectedOptions[0]?.dataset.reselect === "true";
    select.replaceChildren(...Array.from(next.childNodes, (node) => document.importNode(node, true)));
    const exists = selected !== "" && Array.from(select.options).some((option) => option.value === selected);
    if (exists) select.value = selected;
    else if (selected !== "" || needsChoice) {
      // Do not silently provision a different image if an administrator removed
      // the user's selection while the create dialog was open.
      const placeholder = new Option("Choose a base image", "", true, true);
      placeholder.disabled = true;
      placeholder.dataset.reselect = "true";
      select.prepend(placeholder);
      select.value = "";
    }
    imagesAvailable = Array.from(select.options).some((option) => option.value !== "");
    updateCreateAvailability();
    const unavailable = !imagesAvailable;
    const button = document.getElementById("create-button");
    if (pending) disabledButtons.set(button, unavailable);
    else button.disabled = unavailable;
  }

  function responseError(text) {
    if (text.trimStart().startsWith("<")) {
      const page = new DOMParser().parseFromString(text, "text/html");
      return page.querySelector('[role="alert"]')?.textContent?.trim()
        || "The request failed. Refresh and try again.";
    }
    return text.trim() || "The request failed. Refresh and try again.";
  }

  function updateInfo(row) {
    document.getElementById("info-subtitle").textContent = row.dataset.vmName;
    document.getElementById("info-user").textContent = row.dataset.vmOwner;
    document.getElementById("info-image").textContent = row.dataset.vmImage || "Configured base image";
    document.getElementById("info-state").textContent = row.dataset.vmState;
    const created = new Date(row.dataset.vmCreated);
    document.getElementById("info-created").textContent = Number.isNaN(created.getTime())
      ? "n/a" : created.toLocaleString();
    document.getElementById("info-modal").dataset.vmId = row.dataset.vmId;
  }

  openCreate.setAttribute("href", "#create-modal");
  openCreate.setAttribute("aria-haspopup", "dialog");
  openCreate.addEventListener("click", (event) => {
    event.preventDefault();
    if (pending || !imagesAvailable) return;
    document.getElementById("create-error").hidden = true;
    document.getElementById("create-progress").hidden = true;
    showModal("create-modal", "vm-name");
  });

  document.addEventListener("click", (event) => {
    if (!(event.target instanceof Element)) return;
    if (event.target.closest("[data-close-modal]")) {
      closeModal();
      return;
    }
    const info = event.target.closest("[data-info]");
    if (info) {
      updateInfo(info.closest("[data-vm-id]"));
      showModal("info-modal");
      return;
    }
    const performanceButton = event.target.closest("[data-performance]");
    if (performanceButton) {
      if (pending) return;
      const row = performanceButton.closest("[data-vm-id]");
      showModal("performance-modal");
      modal.dataset.vmId = row.dataset.vmId;
      vmPerformance.open(row.dataset);
      return;
    }
    const remove = event.target.closest("[data-remove]");
    if (remove) {
      event.preventDefault();
      if (pending) return;
      const row = remove.closest("[data-vm-id]");
      document.getElementById("remove-name").textContent = row.dataset.vmName;
      document.getElementById("remove-form").action = row.querySelector('[data-vm-action="delete"]').action;
      document.getElementById("remove-error").hidden = true;
      showModal("remove-modal", "remove-cancel");
    }
  });

  document.addEventListener("keydown", (event) => {
    if (!modal) return;
    if (event.key === "Escape") {
      event.preventDefault();
      closeModal();
      return;
    }
    if (event.key !== "Tab") return;
    const focusable = Array.from(modal.querySelectorAll(
      'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), [tabindex="0"]',
    )).filter((element) => element.getClientRects().length > 0);
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (!first) {
      event.preventDefault();
      modal.querySelector("[role=dialog]").focus();
    } else if (event.shiftKey && (document.activeElement === first || document.activeElement.getAttribute("role") === "dialog")) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  const creationInterrupted = "The progress connection was interrupted. Check the dashboard before trying to create this DevBox again.";

  function renderCreationProgress(phase, copiedBytes = 0, totalBytes = 0) {
    const preparing = phase === "preparing";
    const copying = phase === "copying";
    const labels = {
      preparing: "Preparing DevBox...",
      copying: "Creating qcow2 disk image...",
      configuring: "Disk image copied; configuring DevBox...",
      finalizing: "Disk image copied; finalizing DevBox...",
      complete: "DevBox created.",
    };
    if (!Object.hasOwn(labels, phase)) return;
    if (copying) {
      if (!Number.isSafeInteger(copiedBytes) || !Number.isSafeInteger(totalBytes) || copiedBytes < 0 || totalBytes <= 0) {
        throw new Error(creationInterrupted);
      }
      creationPercent = Math.max(creationPercent, Math.min(100, copiedBytes / totalBytes * 100));
    } else if (!preparing) creationPercent = 100;
    const label = document.getElementById("create-progress-label");
    if (label.textContent !== labels[phase]) label.textContent = labels[phase];
    document.getElementById("create-progress").hidden = false;
    document.getElementById("create-progress-spinner").hidden = !preparing;
    document.getElementById("create-progress-track").hidden = preparing;
    document.getElementById("create-progress-value").textContent = preparing ? "" : `${Math.round(creationPercent)}%`;
    const bar = document.getElementById("create-progress-bar");
    bar.value = creationPercent;
    bar.textContent = `${Math.round(creationPercent)}%`;
    bar.classList.toggle("gateway-progress-finalizing", phase === "configuring" || phase === "finalizing");
  }

  async function readCreationStream(response) {
    if (!response.body) throw new Error(creationInterrupted);
    const reader = response.body.getReader();
    const decoder = new TextDecoder("utf-8", { fatal: true });
    const maxLineBytes = 64 * 1024;
    let buffered = "";
    let bufferedBytes = 0;
    let complete = false;

    function consumeLine() {
      const line = buffered.trim();
      buffered = "";
      bufferedBytes = 0;
      if (!line) return;
      let event;
      try { event = JSON.parse(line); }
      catch { throw new Error(creationInterrupted); }
      if (!event || typeof event !== "object" || Array.isArray(event) || typeof event.phase !== "string") {
        throw new Error(creationInterrupted);
      }
      if (event.phase === "error") {
        throw new Error(typeof event.error === "string" && event.error.trim() ? event.error : "Failed to create DevBox.");
      }
      renderCreationProgress(event.phase, event.copiedBytes, event.totalBytes);
      complete = event.phase === "complete";
    }

    function append(bytes) {
      bufferedBytes += bytes.byteLength;
      if (bufferedBytes > maxLineBytes) throw new Error(creationInterrupted);
      buffered += decoder.decode(bytes, { stream: true });
    }

    try {
      while (!complete) {
        let chunk;
        try { chunk = await reader.read(); }
        catch { throw new Error(creationInterrupted); }
        if (chunk.value) {
          let start = 0;
          for (let index = 0; index < chunk.value.byteLength; index++) {
            if (chunk.value[index] !== 10) continue;
            append(chunk.value.subarray(start, index + 1));
            consumeLine();
            start = index + 1;
            if (complete) break;
          }
          if (!complete) append(chunk.value.subarray(start));
        }
        if (chunk.done && !complete) {
          buffered += decoder.decode();
          consumeLine();
          if (!complete) throw new Error(creationInterrupted);
        }
      }
    } finally {
      // A terminal result is authoritative even if closing the reader fails.
      try { await reader.cancel(); } catch { /* The connection may already be closed. */ }
      reader.releaseLock();
    }
  }

  async function creationInventory() {
    const response = await fetch("/", {
      credentials: "same-origin", cache: "no-store", signal: AbortSignal.timeout(10000),
    });
    if (sessionExpired(response)) return false;
    if (!response.ok) throw new Error("Could not refresh the inventory.");
    return updateInventory(await response.text());
  }

  document.addEventListener("submit", async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.dataset.vmAction) return;
    event.preventDefault();
    if (pending) return;
    const action = form.dataset.vmAction;
    const body = new URLSearchParams(new FormData(form));
    const progress = {
      create: "Preparing DevBox...", start: "Starting DevBox...", stop: "Shutting down DevBox...",
      delete: "Removing DevBox...", rdp: "Preparing RDP connection...",
    };
    let creationComplete = false;
    let closeOnSuccess = false;
    mutationGeneration++;
    creationActive = action === "create";
    busy(true, creationActive);
    notice(progress[action], "secondary", true);
    if (creationActive) {
      creationPercent = 0;
      createForm.setAttribute("aria-busy", "true");
      document.getElementById("create-error").hidden = true;
      renderCreationProgress("preparing");
      document.querySelector("#create-modal [role=dialog]").focus();
    }
    if (action === "delete") document.getElementById("remove-error").hidden = true;
    try {
      const response = await fetch(form.action, {
        method: "POST", credentials: "same-origin", cache: "no-store", body,
        headers: action === "create" ? { Accept: "application/x-ndjson" } : {},
      });
      if (sessionExpired(response)) return;
      if (!response.ok) throw new Error(responseError(await response.text()));
      if (action === "rdp") {
        if (!response.headers.get("Content-Type")?.startsWith("application/x-rdp")) {
          throw new Error("The gateway did not return an RDP file. Refresh and try again.");
        }
        const objectURL = URL.createObjectURL(await response.blob());
        const download = document.createElement("a");
        download.href = objectURL;
        download.download = `${form.closest("[data-vm-id]").dataset.vmName}.rdp`;
        document.body.append(download);
        download.click();
        download.remove();
        window.setTimeout(() => URL.revokeObjectURL(objectURL), 60000);
        notice("RDP file downloaded. Open it within two minutes and keep this page signed in. Keep the username in the file unchanged; sign in with your DevBox account inside the desktop.", "success");
      } else {
        const mediaType = (response.headers.get("Content-Type") || "").split(";", 1)[0].trim().toLowerCase();
        if (action === "create" && mediaType === "application/x-ndjson") {
          await readCreationStream(response);
          creationComplete = true;
          creationActive = false;
          closeOnSuccess = true;
          if (!await creationInventory()) return;
        } else {
          if (!updateInventory(await response.text())) return;
          creationComplete = action === "create";
        }
        closeOnSuccess = true;
        if (action === "create") createForm.reset();
        const done = {
          create: "DevBox created. Start it when you are ready to connect.", start: "DevBox started.",
          stop: "Shutdown requested. The inventory will update when the DevBox stops.", delete: "DevBox removed.",
        };
        notice(done[action], "success");
      }
    } catch (error) {
      if (creationComplete) {
        // Provisioning succeeded; a failed follow-up GET must not invite a
        // second creation attempt or turn an authoritative success into failure.
        createForm.reset();
        notice("DevBox created, but the inventory could not be refreshed. Refresh this page to see it.", "warning");
      } else {
        const message = error instanceof TypeError
          ? action === "create" ? creationInterrupted : "The gateway connection was interrupted. Refresh to check the DevBox before retrying."
          : error.message;
        notice(message, "danger");
        const errorID = action === "create" ? "create-error" : action === "delete" ? "remove-error" : null;
        if (errorID) {
          const errorBox = document.getElementById(errorID);
          errorBox.textContent = message;
          errorBox.hidden = false;
          if (action === "create") errorBox.focus();
        }
      }
    } finally {
      creationActive = false;
      busy(false);
      createForm.setAttribute("aria-busy", "false");
      if (closeOnSuccess) closeModal();
      document.getElementById("create-progress").hidden = true;
    }
  });

  async function refreshInventory() {
    // Do not replace controls while the user is interacting with the table.
    if (pending || refreshing || document.hidden || list.contains(document.activeElement)) return;
    refreshing = true;
    const generation = mutationGeneration;
    try {
      const response = await fetch("/", { credentials: "same-origin", cache: "no-store", signal: AbortSignal.timeout(10000) });
      if (sessionExpired(response)) return;
      if (response.ok) {
        const html = await response.text();
        // A poll started before a mutation must not overwrite that mutation's
        // newer response, even if the operation completed while the poll waited.
        if (generation !== mutationGeneration || pending || list.contains(document.activeElement)) return;
        if (!updateInventory(html)) return;
      }
    } catch {
      // Latency indicators report connectivity without erasing the inventory.
    } finally {
      refreshing = false;
    }
  }

  function metric(id, label, value, warning, danger) {
    const badge = document.getElementById(id);
    const tone = value === null ? "secondary" : value > danger ? "danger" : value > warning ? "warning" : "success";
    badge.classList.remove("text-bg-secondary", "text-bg-success", "text-bg-warning", "text-bg-danger");
    badge.classList.add(`text-bg-${tone}`);
    badge.querySelector("span").textContent = value === null ? `${label}: ––` : `${label}: ${Math.round(value)} ms`;
  }

  async function measureLatency() {
    if (measuring || document.hidden) return;
    measuring = true;
    const started = performance.now();
    try {
      const response = await fetch("/health/latency", {
        credentials: "same-origin", cache: "no-store", signal: AbortSignal.timeout(5000),
      });
      if (sessionExpired(response)) return;
      if (!response.ok) throw new Error("Gateway is unavailable");
      const rtt = performance.now() - started;
      const hasJitter = previousRTT !== null;
      if (hasJitter) jitter += (Math.abs(rtt - previousRTT) - jitter) / 16;
      previousRTT = rtt;
      metric("rtt-indicator", "RTT", rtt, 30, 50);
      metric("jitter-indicator", "Jitter", hasJitter ? jitter : null, 20, 50);
    } catch {
      previousRTT = null;
      jitter = 0;
      metric("rtt-indicator", "RTT", null, 30, 50);
      metric("jitter-indicator", "Jitter", null, 20, 50);
    } finally {
      measuring = false;
    }
  }

  window.addEventListener("beforeunload", (event) => {
    if (!creationActive) return;
    event.preventDefault();
    event.returnValue = "";
  });
  enableInfo();
  void measureLatency();
  window.setInterval(measureLatency, 2000);
  window.setInterval(refreshInventory, 5000);
  document.addEventListener("visibilitychange", () => {
    vmPerformance.visibilityChanged();
    if (!document.hidden) {
      void refreshInventory();
      void measureLatency();
    }
  });
})();
