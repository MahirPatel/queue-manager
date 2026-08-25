(async function init() {
  const me = await requireSession();
  if (!me) return;

  const errorBanner = document.getElementById("errorBanner");
  const tbody = document.getElementById("adminsBody");
  const dialog = document.getElementById("adminDialog");
  const form = document.getElementById("adminForm");
  const formError = document.getElementById("adminFormError");
  const searchInput = document.getElementById("adminSearch");
  const loadMoreBtn = document.getElementById("adminsLoadMoreBtn");

  const PAGE_SIZE = 10;
  let admins = [];
  let visibleCount = PAGE_SIZE;

  function filtered() {
    return filterRows(admins, searchInput.value, (a) => [a.admin_name, a.admin_email]);
  }

  async function load() {
    try {
      admins = await Api.get("/api/admins");
      visibleCount = PAGE_SIZE;
      render();
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  }

  function twoFaLabel(a) {
    if (a.enable_2fa) return "On";
    if (a.require_2fa) return "Pending setup";
    return "Off";
  }

  function twoFaBadge(a) {
    if (a.enable_2fa) return '<span class="badge badge-success">On</span>';
    if (a.require_2fa) return '<span class="badge badge-pending">Pending setup</span>';
    return '<span class="badge badge-inactive">Off</span>';
  }

  function roleLabel(a) {
    return a.is_super_admin ? "Super Admin" : "Admin";
  }

  function roleBadge(a) {
    return a.is_super_admin
      ? '<span class="badge badge-role">Super Admin</span>'
      : '<span class="badge badge-inactive">Admin</span>';
  }

  // Enabling 2FA only ever affects your own account (you have to scan your
  // own QR code), so it's always self-service regardless of role. Disabling
  // an *already-enabled* 2FA removes a protection, not just adds one — kept
  // as a super-admin-only recovery action (also enforced server-side) so a
  // hijacked regular-admin session can't strip its own 2FA.
  function actionCell(a) {
    const actions = [];
    if (a.id === me.id && !a.enable_2fa) {
      actions.push(`<a class="btn btn-sm" href="/2fa-setup.html">Enable 2FA</a>`);
    }
    if (me.is_super_admin && (a.enable_2fa || a.require_2fa)) {
      actions.push(`<button class="btn btn-sm" data-disable-2fa="${a.id}">Disable 2FA</button>`);
    }
    return actions.length ? actions.join(" ") : "—";
  }

  function render() {
    if (admins.length === 0) {
      tbody.innerHTML = `<tr><td colspan="8" class="empty-state">No console admins yet.</td></tr>`;
      loadMoreBtn.style.display = "none";
      return;
    }

    const rows = filtered();
    if (rows.length === 0) {
      tbody.innerHTML = `<tr><td colspan="8" class="empty-state">No console admins match your search.</td></tr>`;
      loadMoreBtn.style.display = "none";
      return;
    }

    const shown = visibleSlice(rows, visibleCount);

    tbody.innerHTML = shown.map((a) => `
      <tr>
        <td><strong>${escapeHtml(a.admin_name)}</strong></td>
        <td>${escapeHtml(a.admin_email)}</td>
        <td>${escapeHtml(a.admin_contact) || "—"}</td>
        <td>${roleBadge(a)}</td>
        <td>${twoFaBadge(a)}</td>
        <td><span class="badge badge-${a.admin_status}">${a.admin_status}</span></td>
        <td>${formatDate(a.created_at)}</td>
        <td>${actionCell(a)}</td>
      </tr>
    `).join("");

    updateLoadMoreButton(loadMoreBtn, shown.length, rows.length);
  }

  function exportCsv(rows, filename) {
    downloadCsv(filename, [
      { header: "Name", value: (a) => a.admin_name },
      { header: "Email", value: (a) => a.admin_email },
      { header: "Contact", value: (a) => a.admin_contact },
      { header: "Role", value: (a) => roleLabel(a) },
      { header: "2FA", value: (a) => twoFaLabel(a) },
      { header: "Status", value: (a) => a.admin_status },
      { header: "Added", value: (a) => a.created_at },
    ], rows);
  }

  setupDropdown(document.getElementById("exportMenuBtn"), document.getElementById("exportMenu"));
  document.getElementById("exportVisibleBtn").addEventListener("click", () => exportCsv(filtered(), "console-admins-visible.csv"));
  document.getElementById("exportAllBtn").addEventListener("click", () => exportCsv(admins, "console-admins-all.csv"));

  searchInput.addEventListener("input", () => { visibleCount = PAGE_SIZE; render(); });
  loadMoreBtn.addEventListener("click", () => { visibleCount += PAGE_SIZE; render(); });

  const newAdminBtn = document.getElementById("newAdminBtn");
  if (!me.is_super_admin) {
    newAdminBtn.style.display = "none";
  }
  newAdminBtn.addEventListener("click", () => {
    hideBanner(formError);
    form.reset();
    dialog.showModal();
  });

  dialog.addEventListener("click", (ev) => {
    if (ev.target.hasAttribute("data-close")) dialog.close();
  });

  tbody.addEventListener("click", async (ev) => {
    const disableId = ev.target.getAttribute("data-disable-2fa");
    if (!disableId) return;
    const a = admins.find((a) => a.id === disableId);
    if (!a) return;
    if (!confirm(`Disable 2FA for "${a.admin_name}"? They'll be able to sign in with just their password again${a.require_2fa ? " — this also lifts the requirement to set it back up" : ""}.`)) return;
    try {
      await Api.patch(`/api/admins/${disableId}/disable-2fa`);
      await load();
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  });

  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    hideBanner(formError);
    const payload = {
      admin_name: document.getElementById("adminName").value,
      admin_contact: document.getElementById("adminContact").value,
      admin_email: document.getElementById("adminEmail").value,
      admin_password: document.getElementById("adminPassword").value,
      require_2fa: document.getElementById("adminRequire2FA").checked,
    };
    try {
      await Api.post("/api/admins", payload);
      dialog.close();
      await load();
    } catch (e) {
      showBanner(formError, e.message);
    }
  });

  await load();
})();
