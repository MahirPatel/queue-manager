(async function init() {
  if (!(await requireSession())) return;

  const errorBanner = document.getElementById("errorBanner");
  const tbody = document.getElementById("requestsBody");
  const filterTemplate = document.getElementById("filterTemplate");
  const filterStatus = document.getElementById("filterStatus");
  const filterFrom = document.getElementById("filterFrom");
  const filterTo = document.getElementById("filterTo");
  const loadMoreBtn = document.getElementById("loadMoreBtn");
  const detailDialog = document.getElementById("detailDialog");
  const detailBody = document.getElementById("detailBody");
  const retryFromDialogBtn = document.getElementById("retryFromDialogBtn");
  const searchInput = document.getElementById("requestSearch");
  const exportAllBtn = document.getElementById("exportAllBtn");

  let templates = [];
  let rows = [];
  let cursor = null; // {after_at, after_id} of the last row loaded
  let currentDetailId = null;

  function filtered() {
    return filterRows(rows, searchInput.value, (r) => [r.request_id, templateName(r.template_id), destinationName(r.template_id, r.destination_id)]);
  }

  const params = new URLSearchParams(window.location.search);

  function buildQuery(extra) {
    const q = new URLSearchParams();
    if (filterTemplate.value) q.set("template_id", filterTemplate.value);
    if (filterStatus.value) q.set("process_status", filterStatus.value);
    if (filterFrom.value) q.set("from", new Date(filterFrom.value).toISOString());
    if (filterTo.value) q.set("to", new Date(filterTo.value).toISOString());
    q.set("limit", String(PAGE_SIZE));
    if (extra) Object.entries(extra).forEach(([k, v]) => q.set(k, v));
    return q.toString();
  }

  function templateName(id) {
    const t = templates.find((t) => t.id === id);
    return t ? t.template_name : id;
  }

  function destinationName(templateId, destinationId) {
    const t = templates.find((t) => t.id === templateId);
    const d = t && t.destinations.find((d) => d.id === destinationId);
    return d ? d.destination_name : destinationId;
  }

  async function loadTemplates() {
    templates = await Api.get("/api/templates");
    filterTemplate.innerHTML = `<option value="">All transmission channels</option>` +
      templates.map((t) => `<option value="${t.id}">${escapeHtml(t.template_name)}</option>`).join("");
    if (params.get("template_id")) filterTemplate.value = params.get("template_id");
    if (params.get("process_status")) filterStatus.value = params.get("process_status");
  }

  const PAGE_SIZE = 50;

  async function loadFirstPage() {
    cursor = null;
    rows = await Api.get(`/api/requests?${buildQuery()}`);
    render();
    updateLoadMore(rows.length);
  }

  async function loadNextPage() {
    if (!cursor) return;
    const more = await Api.get(`/api/requests?${buildQuery(cursor)}`);
    rows = rows.concat(more);
    render();
    updateLoadMore(more.length);
  }

  function updateLoadMore(lastBatchSize) {
    if (rows.length === 0) {
      loadMoreBtn.style.display = "none";
      return;
    }
    const last = rows[rows.length - 1];
    cursor = { after_at: last.request_at, after_id: last.id };
    // A page shorter than PAGE_SIZE means there's nothing left to load.
    loadMoreBtn.style.display = lastBatchSize < PAGE_SIZE ? "none" : "";
  }

  function render() {
    if (rows.length === 0) {
      tbody.innerHTML = `<tr><td colspan="7" class="empty-state"><div class="empty-title">No transmission requests match these filters</div>Try widening the date range or clearing a filter.</td></tr>`;
      return;
    }
    const visible = filtered();
    if (visible.length === 0) {
      tbody.innerHTML = `<tr><td colspan="7" class="empty-state"><div class="empty-title">No loaded rows match your search</div>Try a different request ID or transmission channel, or load more rows first.</td></tr>`;
      return;
    }
    tbody.innerHTML = visible.map((r) => `
      <tr class="row-signal ${r.request_process_status}">
        <td class="mono">${escapeHtml(r.request_id)}</td>
        <td>${escapeHtml(templateName(r.template_id))} <span class="mono" style="font-size:11px;color:var(--ink-faint)">→ ${escapeHtml(destinationName(r.template_id, r.destination_id))}</span></td>
        <td>${statusBadge(r.request_process_status)}</td>
        <td class="mono">${r.request_status_code || "—"}</td>
        <td>${formatDate(r.request_at)}</td>
        <td>${r.retry_count}</td>
        <td>
          <button class="btn btn-sm" data-view="${r.id}">View</button>
          ${r.request_process_status === "failed" ? `<button class="btn btn-sm btn-primary" data-retry="${r.id}">Retry</button>` : ""}
        </td>
      </tr>
    `).join("");
  }

  function openDetail(id) {
    const r = rows.find((r) => r.id === id);
    if (!r) return;
    currentDetailId = id;
    detailBody.innerHTML = `
      <div class="detail-block"><div class="detail-label">Status</div>${statusBadge(r.request_process_status)} <span class="mono">HTTP ${r.request_status_code || "—"}</span></div>
      <div class="detail-block"><div class="detail-label">Transmission Channel</div>${escapeHtml(templateName(r.template_id))}</div>
      <div class="detail-block"><div class="detail-label">Destination</div>${escapeHtml(destinationName(r.template_id, r.destination_id))}</div>
      <div class="detail-block"><div class="detail-label">Requested</div>${formatDate(r.request_at)}</div>
      ${r.recent_request_retry_at ? `<div class="detail-block"><div class="detail-label">Last retried</div>${formatDate(r.recent_request_retry_at)} (${r.retry_count} total)</div>` : ""}
      <div class="detail-block"><div class="detail-label">Destination endpoint</div><pre>${escapeHtml(r.destination_end_point)}</pre></div>
      <div class="detail-block"><div class="detail-label">Transmission request body (decrypted)</div><pre>${escapeHtml(prettyJson(r.request_body))}</pre></div>
      <div class="detail-block"><div class="detail-label">Response</div><pre>${escapeHtml(prettyJson(r.request_response) || "(no response recorded)")}</pre></div>
    `;
    retryFromDialogBtn.style.display = r.request_process_status === "failed" ? "" : "none";
    detailDialog.showModal();
  }

  function prettyJson(text) {
    if (!text) return "";
    try { return JSON.stringify(JSON.parse(text), null, 2); } catch (e) { return text; }
  }

  async function retry(id) {
    try {
      await Api.post(`/api/requests/${id}/retry`);
      await loadFirstPage();
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  }

  document.getElementById("applyFilters").addEventListener("click", () => loadFirstPage().catch((e) => showBanner(errorBanner, e.message)));
  document.getElementById("refreshBtn").addEventListener("click", () => loadFirstPage().catch((e) => showBanner(errorBanner, e.message)));
  loadMoreBtn.addEventListener("click", () => loadNextPage().catch((e) => showBanner(errorBanner, e.message)));
  searchInput.addEventListener("input", render);

  function exportCsv(list, filename) {
    downloadCsv(filename, [
      { header: "Transmission Request", value: (r) => r.request_id },
      { header: "Transmission Channel", value: (r) => templateName(r.template_id) },
      { header: "Destination", value: (r) => destinationName(r.template_id, r.destination_id) },
      { header: "Status", value: (r) => r.request_process_status },
      { header: "HTTP", value: (r) => r.request_status_code },
      { header: "Requested", value: (r) => r.request_at },
      { header: "Retries", value: (r) => r.retry_count },
    ], list);
  }

  setupDropdown(document.getElementById("exportMenuBtn"), document.getElementById("exportMenu"));
  document.getElementById("exportVisibleBtn").addEventListener("click", () => exportCsv(filtered(), "transmission-requests-visible.csv"));

  // Export all: walks the same cursor the "Load more" button uses, but to
  // completion, against whatever filters are currently applied above (the
  // free-text search box only affects what's on screen, so it's ignored
  // here — exporting "all matching filters" means the server-side filters).
  exportAllBtn.addEventListener("click", async () => {
    const original = exportAllBtn.textContent;
    exportAllBtn.disabled = true;
    exportAllBtn.textContent = "Exporting…";
    try {
      let all = [];
      let cur = undefined;
      for (;;) {
        const batch = await Api.get(`/api/requests?${buildQuery(cur)}`);
        all = all.concat(batch);
        if (batch.length < PAGE_SIZE) break;
        const last = batch[batch.length - 1];
        cur = { after_at: last.request_at, after_id: last.id };
      }
      exportCsv(all, "transmission-requests-all-matching-filters.csv");
    } catch (e) {
      showBanner(errorBanner, e.message);
    } finally {
      exportAllBtn.disabled = false;
      exportAllBtn.textContent = original;
    }
  });

  tbody.addEventListener("click", (ev) => {
    const viewId = ev.target.getAttribute("data-view");
    const retryId = ev.target.getAttribute("data-retry");
    if (viewId) openDetail(viewId);
    if (retryId) retry(retryId);
  });

  detailDialog.addEventListener("click", (ev) => {
    if (ev.target.hasAttribute("data-close")) detailDialog.close();
  });

  retryFromDialogBtn.addEventListener("click", async () => {
    if (!currentDetailId) return;
    await retry(currentDetailId);
    detailDialog.close();
  });

  try {
    await loadTemplates();
    await loadFirstPage();
  } catch (e) {
    showBanner(errorBanner, e.message);
  }
})();
