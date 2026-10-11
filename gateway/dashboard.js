// Dashboard presentation follows DevBox Gateway. All VM mutations retain the
// gateway's same-origin, CSRF-protected form endpoints.
(() => {
  "use strict";

  const main = document.getElementById("dashboard-main");
  const list = document.getElementById("vm-list");
  const actions = document.getElementById("action-area");
  const createForm = document.getElementById("create-form");
  const openCreate = document.getElementById("open-create-button");
  let modal = null;
  let previousFocus = null;
  let pending = false;
  let mutationGeneration = 0;
  let refreshing = false;
  let measuring = false;
  let previousRTT = null;
  let jitter = 0;
  let disabledButtons = new Map();

  function enableInfo() {
    list.querySelectorAll("[data-info]").forEach((button) => { button.hidden = false; });
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
    if (!modal) return;
    modal.hidden = true;
    modal.setAttribute("aria-hidden", "true");
    modal = null;
    main.inert = false;
    document.body.classList.remove("gateway-modal-open");
    const target = previousFocus && previousFocus.isConnected ? previousFocus : openCreate;
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

  function busy(value) {
    pending = value;
    openCreate.setAttribute("aria-disabled", String(value));
    if (value) {
      disabledButtons = new Map();
      document.querySelectorAll("button:not([data-close-modal])").forEach((button) => {
        disabledButtons.set(button, button.disabled);
        button.disabled = true;
      });
    } else {
      disabledButtons.forEach((disabled, button) => { button.disabled = disabled; });
      disabledButtons.clear();
    }
  }

  function sessionExpired(response) {
    if (response.status === 401 || new URL(response.url).pathname === "/login") {
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
    if (nextMain.dataset.user !== main.dataset.user) {
      window.location.reload();
      return false;
    }
    const csrf = page.querySelector('input[name="csrf"]');
    if (csrf) {
      document.querySelectorAll('input[name="csrf"]').forEach((input) => { input.value = csrf.value; });
    }
    list.replaceChildren(...Array.from(next.childNodes, (node) => document.importNode(node, true)));
    enableInfo();
    if (modal && modal.id === "info-modal") {
      const row = Array.from(list.querySelectorAll("[data-vm-id]"))
        .find((item) => item.dataset.vmId === modal.dataset.vmId);
      if (row) updateInfo(row);
      else closeModal();
    }
    return true;
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
    if (pending) return;
    document.getElementById("create-error").hidden = true;
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
    mutationGeneration++;
    busy(true);
    notice(progress[action], "secondary", true);
    if (action === "create") {
      document.getElementById("create-error").hidden = true;
      document.getElementById("create-progress").hidden = false;
    }
    if (action === "delete") document.getElementById("remove-error").hidden = true;
    try {
      const response = await fetch(form.action, {
        method: "POST", credentials: "same-origin", cache: "no-store", body,
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
        if (!updateInventory(await response.text())) return;
        closeModal();
        if (action === "create") createForm.reset();
        const done = {
          create: "DevBox created. Start it when you are ready to connect.", start: "DevBox started.",
          stop: "Shutdown requested. The inventory will update when the DevBox stops.", delete: "DevBox removed.",
        };
        notice(done[action], "success");
      }
    } catch (error) {
      const message = error instanceof TypeError
        ? "The gateway connection was interrupted. Refresh to check the DevBox before retrying."
        : error.message;
      notice(message, "danger");
      const errorID = action === "create" ? "create-error" : action === "delete" ? "remove-error" : null;
      if (errorID) {
        const errorBox = document.getElementById(errorID);
        errorBox.textContent = message;
        errorBox.hidden = false;
      }
    } finally {
      busy(false);
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

  enableInfo();
  void measureLatency();
  window.setInterval(measureLatency, 2000);
  window.setInterval(refreshInventory, 5000);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      void refreshInventory();
      void measureLatency();
    }
  });
})();
