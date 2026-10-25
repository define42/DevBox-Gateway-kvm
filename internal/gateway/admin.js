// Admin actions use the same CSRF-protected forms as the HTML fallback.
(() => {
  "use strict";

  const main = document.getElementById("admin-main");
  const list = document.getElementById("vm-list");
  const images = document.getElementById("image-list");
  const panel = document.getElementById("base-images-panel");
  const dialog = document.getElementById("base-images-dialog");
  const openImages = document.getElementById("base-images-button");
  const uploadForm = document.getElementById("image-upload-form");
  const fileInput = document.getElementById("image-file");
  const errorBox = document.getElementById("image-error");
  const imageMessage = document.getElementById("image-message");
  const progress = document.getElementById("image-progress");
  const progressBar = document.getElementById("image-progress-bar");
  const progressLabel = document.getElementById("image-progress-label");
  const progressDetail = document.getElementById("image-progress-detail");
  const cancelUpload = document.getElementById("image-upload-cancel");
  let modalOpen = false;
  let pending = false;
  let refreshing = false;
  let mutationGeneration = 0;
  let disabledControls = new Map();
  let activeUpload = null;
  let measuring = false;
  let previousRTT = null;
  let jitter = 0;

  function formatBytes(value) {
    const bytes = Number(value);
    if (!Number.isFinite(bytes) || bytes < 0) return "n/a";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    const index = bytes === 0 ? 0 : Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
    return `${(bytes / (1024 ** index)).toLocaleString(undefined, { maximumFractionDigits: index ? 1 : 0 })} ${units[index]}`;
  }

  function formatSizes() {
    document.querySelectorAll("[data-bytes]").forEach((element) => {
      element.textContent = formatBytes(element.dataset.bytes);
      element.title = `${Number(element.dataset.bytes).toLocaleString()} bytes`;
    });
  }

  function notice(message, kind = "secondary") {
    const alert = document.createElement("div");
    alert.className = `alert alert-${kind} mb-0`;
    if (kind === "danger") alert.setAttribute("role", "alert");
    alert.textContent = message;
    document.getElementById("action-area").replaceChildren(alert);
  }

  function imageError(message) {
    errorBox.textContent = message;
    errorBox.hidden = false;
    imageMessage.hidden = true;
    if (modalOpen) errorBox.focus();
    notice(message, "danger");
  }

  function imageSuccess(message) {
    errorBox.hidden = true;
    imageMessage.textContent = message;
    imageMessage.hidden = false;
    notice(message, "success");
  }

  function busy(value) {
    pending = value;
    main.setAttribute("aria-busy", String(value));
    uploadForm.setAttribute("aria-busy", String(value));
    if (value) {
      disabledControls = new Map();
      document.querySelectorAll("button, input[type=file]").forEach((control) => {
        if (control === cancelUpload) return;
        disabledControls.set(control, control.disabled);
        control.disabled = true;
      });
    } else {
      disabledControls.forEach((disabled, control) => { control.disabled = disabled; });
      disabledControls.clear();
    }
  }

  function openModal() {
    if (pending || modalOpen) return;
    modalOpen = true;
    panel.hidden = false;
    panel.setAttribute("aria-hidden", "false");
    main.inert = true;
    document.body.classList.add("gateway-modal-open");
    fileInput.focus();
  }

  function closeModal() {
    if (pending || !modalOpen) return;
    modalOpen = false;
    panel.hidden = true;
    panel.setAttribute("aria-hidden", "true");
    main.inert = false;
    document.body.classList.remove("gateway-modal-open");
    openImages.focus();
  }

  function sessionExpired(status, responseURL) {
    if (status === 401 || (responseURL && new URL(responseURL, location.href).pathname === "/login")) {
      window.location.assign("/login");
      return true;
    }
    return false;
  }

  function responseError(text, status) {
    if (text.trimStart().startsWith("<")) {
      const page = new DOMParser().parseFromString(text, "text/html");
      return page.querySelector('[role="alert"]')?.textContent?.trim()
        || `The request failed (${status}). Refresh and try again.`;
    }
    return text.trim() || `The request failed (${status}). Refresh and try again.`;
  }

  function updateInventory(html) {
    const page = new DOMParser().parseFromString(html, "text/html");
    const nextMain = page.getElementById("admin-main");
    const nextList = page.getElementById("vm-list");
    const nextImages = page.getElementById("image-list");
    if (!nextMain || !nextList || !nextImages) {
      throw new Error("The gateway returned an unexpected page. Refresh and try again.");
    }
    if (nextMain.dataset.user !== main.dataset.user) {
      window.location.reload();
      return false;
    }
    const csrf = page.querySelector('input[name="csrf"]');
    if (csrf) document.querySelectorAll('input[name="csrf"]').forEach((input) => { input.value = input.defaultValue = csrf.value; });
    list.replaceChildren(...Array.from(nextList.childNodes, (node) => document.importNode(node, true)));
    images.replaceChildren(...Array.from(nextImages.childNodes, (node) => document.importNode(node, true)));
    main.dataset.maxUploadBytes = nextMain.dataset.maxUploadBytes;
    const limit = document.querySelector("#image-upload-limit [data-bytes]");
    limit.dataset.bytes = main.dataset.maxUploadBytes;
    formatSizes();
    return true;
  }

  function confirmRemoval(form) {
    if (form.dataset.adminAction === "delete-vm") {
      const row = form.closest("[data-vm-id]");
      return window.confirm(`Permanently delete “${row.dataset.vmName}”, owned by ${row.dataset.vmOwner}, and all its saved files?\n\nThis cannot be undone.`);
    }
    if (form.dataset.adminAction === "delete-image") {
      const row = form.closest("[data-image-id]");
      return window.confirm(`Remove “${row.dataset.imageName}” from the image catalog?\n\nExisting DevBoxes keep their disks. New DevBoxes will no longer be able to use this image.`);
    }
    return true;
  }

  document.addEventListener("click", (event) => {
    if (!(event.target instanceof Element)) return;
    if (event.target.closest("[data-close-modal]")) closeModal();
    const remove = event.target.closest("[data-remove]");
    if (remove) {
      event.preventDefault();
      if (!pending) remove.closest("details").querySelector("form").requestSubmit();
    }
  });

  document.addEventListener("keydown", (event) => {
    if (!modalOpen) return;
    if (event.key === "Escape") {
      event.preventDefault();
      closeModal();
      return;
    }
    if (event.key !== "Tab") return;
    const focusable = Array.from(panel.querySelectorAll(
      'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), summary, [tabindex="0"]',
    )).filter((element) => element.getClientRects().length > 0);
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (!first) {
      event.preventDefault();
      dialog.focus();
    } else if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog || document.activeElement === errorBox)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  document.addEventListener("submit", async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.dataset.adminAction) return;
    event.preventDefault();
    if (pending || !confirmRemoval(form)) return;
    const action = form.dataset.adminAction;
    const body = new URLSearchParams(new FormData(form));
    mutationGeneration++;
    busy(true);
    errorBox.hidden = true;
    imageMessage.hidden = true;
    notice({ start: "Starting DevBox...", stop: "Requesting graceful shutdown...", "delete-vm": "Removing DevBox...", "delete-image": "Removing base image..." }[action]);
    try {
      const response = await fetch(form.action, { method: "POST", credentials: "same-origin", cache: "no-store", body });
      if (sessionExpired(response.status, response.url)) return;
      if (!response.ok) throw new Error(responseError(await response.text(), response.status));
      if (!updateInventory(await response.text())) return;
      const message = { start: "DevBox started.", stop: "Shutdown requested. The inventory will update when the DevBox stops.", "delete-vm": "DevBox removed.", "delete-image": "Base image removed. Existing DevBoxes keep their disks." }[action];
      if (action === "delete-image") imageSuccess(message);
      else notice(message, "success");
      if (modalOpen) dialog.focus();
    } catch (error) {
      const message = error instanceof TypeError
        ? "The gateway connection was interrupted. Refresh to check the result before retrying."
        : error.message;
      if (action === "delete-image") imageError(message);
      else notice(message, "danger");
    } finally {
      busy(false);
    }
  });

  function preparingImage() {
    progressBar.removeAttribute("value");
    progressLabel.textContent = "Validating and preparing image...";
    progressDetail.textContent = "Transfer complete. Preparing large or compressed images may take several minutes. Keep this page open.";
    notice("Upload complete. Validating and preparing the base image...");
  }

  uploadForm.addEventListener("submit", (event) => {
    event.preventDefault();
    if (pending) return;
    const file = fileInput.files[0];
    if (!file) return;
    const limit = Number(main.dataset.maxUploadBytes);
    if (!/\.(qcow2|img|raw)$/i.test(file.name)) {
      imageError("Choose a QCOW2 image with a .qcow2, .img, or .raw filename.");
      return;
    }
    if (file.size === 0 || (limit > 0 && file.size > limit)) {
      imageError(file.size === 0 ? "The selected file is empty." : `This file exceeds the ${formatBytes(limit)} upload limit.`);
      return;
    }
    // Construct before disabling the file control. DOM order sends csrf first,
    // followed by the image, so the server can authenticate before streaming it.
    const body = new FormData(uploadForm);
    const xhr = new XMLHttpRequest();
    let transferComplete = false;
    const prepareImage = () => {
      if (activeUpload !== xhr || transferComplete) return;
      transferComplete = true;
      preparingImage();
    };
    activeUpload = xhr;
    mutationGeneration++;
    busy(true);
    errorBox.hidden = true;
    imageMessage.hidden = true;
    progress.hidden = false;
    progressBar.value = 0;
    progressLabel.textContent = "Uploading image...";
    progressDetail.textContent = `Uploading ${file.name}. Keep this page open until the image is ready.`;
    cancelUpload.disabled = false;
    cancelUpload.focus();
    notice("Uploading base image...");
    xhr.upload.onprogress = (upload) => {
      if (activeUpload !== xhr || transferComplete) return;
      if (!upload.lengthComputable || upload.total <= 0) {
        progressBar.removeAttribute("value");
        progressLabel.textContent = "Uploading image...";
        return;
      }
      // Byte transfer can finish well before the import response arrives.
      // Switch phases here too, without waiting for the upload load event.
      if (upload.loaded >= upload.total) {
        prepareImage();
        return;
      }
      const percentage = Math.min(99, Math.floor(upload.loaded / upload.total * 100));
      progressBar.value = percentage;
      progressLabel.textContent = `Uploading image: ${percentage}%`;
    };
    xhr.upload.onload = prepareImage;
    xhr.onload = () => {
      activeUpload = null;
      if (sessionExpired(xhr.status, xhr.responseURL)) return;
      try {
        if (xhr.status < 200 || xhr.status >= 300) throw new Error(responseError(xhr.responseText, xhr.status));
        if (!updateInventory(xhr.responseText)) return;
        uploadForm.reset();
        imageSuccess("Base image added. It is now available when creating a DevBox.");
      } catch (error) {
        imageError(error.message);
      }
    };
    xhr.onerror = () => imageError("The upload connection was interrupted. Check the image catalog before retrying.");
    xhr.onabort = () => imageSuccess("Upload canceled. Checking the catalog for any import that already completed.");
    xhr.onloadend = () => {
      activeUpload = null;
      progress.hidden = true;
      busy(false);
      if (document.activeElement === cancelUpload) fileInput.focus();
      void refreshInventory();
    };
    // Register upload listeners before open for browser compatibility.
    xhr.open("POST", uploadForm.action);
    xhr.setRequestHeader("X-CSRF-Token", uploadForm.elements.csrf.value);
    xhr.send(body);
  });

  cancelUpload.addEventListener("click", () => {
    if (activeUpload) activeUpload.abort();
  });

  async function refreshInventory() {
    if (pending || refreshing || document.hidden || list.contains(document.activeElement) || images.contains(document.activeElement)) return;
    refreshing = true;
    const generation = mutationGeneration;
    try {
      const response = await fetch("/admin", { credentials: "same-origin", cache: "no-store", signal: AbortSignal.timeout(10000) });
      if (sessionExpired(response.status, response.url)) return;
      if (response.status === 403) {
        window.location.assign("/");
        return;
      }
      if (!response.ok) return;
      const html = await response.text();
      if (generation !== mutationGeneration || pending || list.contains(document.activeElement) || images.contains(document.activeElement)) return;
      updateInventory(html);
    } catch {
      // Preserve the last inventory; health badges report lost connectivity.
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
      const response = await fetch("/health/latency", { credentials: "same-origin", cache: "no-store", signal: AbortSignal.timeout(5000) });
      if (sessionExpired(response.status, response.url)) return;
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

  // Enhance the fully usable HTML image section into the upstream-style modal.
  panel.className = "terminal-modal";
  panel.hidden = true;
  panel.setAttribute("aria-hidden", "true");
  dialog.className = "terminal-modal__dialog terminal-modal__dialog--form";
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  document.getElementById("base-images-body").className = "terminal-modal__body";
  panel.querySelectorAll("[data-close-modal]").forEach((element) => { element.hidden = false; });
  openImages.setAttribute("aria-haspopup", "dialog");
  openImages.addEventListener("click", (event) => { event.preventDefault(); openModal(); });
  window.addEventListener("beforeunload", (event) => {
    if (!activeUpload) return;
    event.preventDefault();
    event.returnValue = "";
  });
  formatSizes();
  if (location.hash === "#base-images-panel") openModal();
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
