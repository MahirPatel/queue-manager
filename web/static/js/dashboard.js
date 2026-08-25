(async function init() {
  if (!(await requireSession())) return;

  const errorBanner = document.getElementById("errorBanner");
  const statGrid = document.getElementById("statGrid");
  const tbody = document.getElementById("recentFailuresBody");

  try {
    const [products, templates, failed] = await Promise.all([
      Api.get("/api/products"),
      Api.get("/api/templates"),
      Api.get("/api/requests?process_status=failed&limit=200"),
    ]);

    const activeTemplates = templates.filter((t) => t.template_status === "active").length;
    const failedLabel = failed.length >= 200 ? "200+" : String(failed.length);

    statGrid.innerHTML = `
      <div class="stat-tile"><div class="stat-value">${products.length}</div><div class="stat-label">Integrations</div></div>
      <div class="stat-tile"><div class="stat-value">${activeTemplates} / ${templates.length}</div><div class="stat-label">Active transmission channels</div></div>
      <div class="stat-tile"><div class="stat-value">${failedLabel}</div><div class="stat-label">Transmission requests needing retry</div></div>
    `;

    const templateNameById = {};
    templates.forEach((t) => { templateNameById[t.id] = t.template_name; });

    if (failed.length === 0) {
      tbody.innerHTML = `<tr><td colspan="5" class="empty-state"><div class="empty-title">Nothing failing right now</div>New failures will show up here as they happen.</td></tr>`;
    } else {
      tbody.innerHTML = failed.slice(0, 5).map((r) => `
        <tr>
          <td class="mono">${escapeHtml(r.request_id)}</td>
          <td>${escapeHtml(templateNameById[r.template_id] || r.template_id)}</td>
          <td class="mono">${r.request_status_code || "—"}</td>
          <td>${formatDate(r.request_at)}</td>
          <td><a href="/requests.html?process_status=failed&template_id=${encodeURIComponent(r.template_id)}">View</a></td>
        </tr>
      `).join("");
    }
  } catch (e) {
    showBanner(errorBanner, e.message);
  }
})();
