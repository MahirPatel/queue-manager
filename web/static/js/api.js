const Api = {
  async request(method, path, body) {
    const res = await fetch(path, {
      method,
      headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
      credentials: "same-origin",
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    let data = null;
    const text = await res.text();
    if (text) {
      try { data = JSON.parse(text); } catch (e) { /* non-JSON response */ }
    }

    if (!res.ok) {
      const message = (data && data.error) || `Request failed (${res.status})`;
      const err = new Error(message);
      err.status = res.status;
      throw err;
    }
    return data;
  },
  get(path) { return this.request("GET", path); },
  post(path, body) { return this.request("POST", path, body === undefined ? {} : body); },
  put(path, body) { return this.request("PUT", path, body); },
  patch(path, body) { return this.request("PATCH", path, body === undefined ? {} : body); },
  del(path) { return this.request("DELETE", path); },
};

// requireSession redirects to the login page if there's no valid session,
// otherwise fills in the sidebar's "logged in as" line, wires logout, and
// returns the logged-in admin's own profile (id, admin_email,
// is_super_admin, ...) so pages can tailor what they show per-role.
async function requireSession() {
  let me;
  try {
    me = await Api.get("/api/auth/me");
    const who = document.getElementById("whoami");
    if (who) who.textContent = me.admin_email;
  } catch (e) {
    if (e.status === 401) {
      window.location.href = "/login.html";
      return null;
    }
    throw e;
  }

  const logoutBtn = document.getElementById("logoutBtn");
  if (logoutBtn) {
    logoutBtn.addEventListener("click", async () => {
      await Api.post("/api/auth/logout").catch(() => {});
      window.location.href = "/login.html";
    });
  }
  return me;
}

function showBanner(el, message, kind) {
  if (!el) return;
  el.textContent = message;
  el.className = "banner show banner-" + (kind || "error");
}

function hideBanner(el) {
  if (!el) return;
  el.className = "banner";
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function formatDate(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric", month: "short", day: "numeric",
    hour: "2-digit", minute: "2-digit", second: "2-digit",
  });
}

function statusBadge(status) {
  const cls = { pending: "badge-pending", processing: "badge-processing", success: "badge-success", failed: "badge-failed" }[status] || "badge-inactive";
  return `<span class="badge ${cls}">${escapeHtml(status)}</span>`;
}

function slugify(text) {
  return (text || "")
    .toString()
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// filterRows keeps rows where any of getFields(row) contains query
// (case-insensitive substring match). Used for the client-side search box
// on each table page.
function filterRows(rows, query, getFields) {
  const q = (query || "").trim().toLowerCase();
  if (!q) return rows;
  return rows.filter((row) => getFields(row).some((f) => String(f ?? "").toLowerCase().includes(q)));
}

// visibleSlice returns the first visibleCount rows — pairs with a "Load
// more" button so every table in the console uses the same reveal-more
// pattern (matching the Transmission Requests page) instead of numbered
// pagination.
function visibleSlice(rows, visibleCount) {
  return rows.slice(0, visibleCount);
}

// updateLoadMoreButton shows/hides btn depending on whether there are more
// rows beyond what's currently visible, and updates its label with the
// remaining count.
function updateLoadMoreButton(btn, shownCount, totalCount) {
  if (!btn) return;
  const remaining = totalCount - shownCount;
  if (remaining <= 0) {
    btn.style.display = "none";
    return;
  }
  btn.style.display = "";
  btn.textContent = `Load more (${remaining} more)`;
}

// setupDropdown wires a trigger button to its menu panel: click toggles it
// open/closed, clicking a menu item or outside the menu (or Escape) closes
// it. Used for the per-table "Export ▾" menu on every list page.
function setupDropdown(btn, menu) {
  function close() {
    menu.hidden = true;
    btn.setAttribute("aria-expanded", "false");
  }
  btn.addEventListener("click", (ev) => {
    ev.stopPropagation();
    const opening = menu.hidden;
    menu.hidden = !opening;
    btn.setAttribute("aria-expanded", String(opening));
  });
  menu.addEventListener("click", (ev) => {
    if (ev.target.closest("button")) close();
  });
  document.addEventListener("click", (ev) => {
    if (!menu.hidden && !menu.contains(ev.target) && ev.target !== btn) close();
  });
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && !menu.hidden) { close(); btn.focus(); }
  });
}

function csvCell(value) {
  const s = String(value ?? "");
  return /[",\r\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
}

// triggerDownload saves a Blob to the user's machine via a throwaway <a
// download> click — the shared mechanic behind every export/download button
// in the console.
function triggerDownload(filename, blob) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// downloadCsv builds a CSV client-side (UTF-8 BOM so Excel doesn't mangle
// non-ASCII text) and triggers a browser download. columns: [{header, value(row)}].
function downloadCsv(filename, columns, rows) {
  const lines = [columns.map((c) => csvCell(c.header)).join(",")]
    .concat(rows.map((row) => columns.map((c) => csvCell(c.value(row))).join(",")));
  triggerDownload(filename, new Blob(["﻿" + lines.join("\r\n")], { type: "text/csv;charset=utf-8;" }));
}

// downloadText triggers a plain-text (or markdown, etc.) file download —
// used for the sample integration doc on the transmission channel dialog.
function downloadText(filename, content, mimeType) {
  triggerDownload(filename, new Blob([content], { type: mimeType || "text/plain;charset=utf-8;" }));
}

// copyToClipboard works over plain http too (navigator.clipboard requires a
// secure context, which excludes http on anything but localhost — this
// tool may well be deployed on a plain-http internal host).
async function copyToClipboard(text) {
  if (window.isSecureContext && navigator.clipboard) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  document.execCommand("copy");
  document.body.removeChild(textarea);
}
