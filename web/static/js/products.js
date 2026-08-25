(async function init() {
  if (!(await requireSession())) return;

  const errorBanner = document.getElementById("errorBanner");
  const tbody = document.getElementById("productsBody");
  const dialog = document.getElementById("productDialog");
  const form = document.getElementById("productForm");
  const formError = document.getElementById("productFormError");
  const dialogTitle = document.getElementById("productDialogTitle");
  const searchInput = document.getElementById("productSearch");
  const loadMoreBtn = document.getElementById("productsLoadMoreBtn");

  const PAGE_SIZE = 10;
  let products = [];
  let visibleCount = PAGE_SIZE;

  function filtered() {
    return filterRows(products, searchInput.value, (p) => [p.product_name, p.product_description]);
  }

  async function load() {
    try {
      products = await Api.get("/api/products");
      visibleCount = PAGE_SIZE;
      render();
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  }

  function maskedCell(id, field, label) {
    return `
      <div class="secret-cell">
        <span class="secret-mask">••••••••••••</span>
        <button type="button" class="btn btn-sm copy-btn" data-copy="${field}" data-copy-id="${id}" title="Copy ${label}">Copy</button>
      </div>
    `;
  }

  function render() {
    if (products.length === 0) {
      tbody.innerHTML = `<tr><td colspan="6" class="empty-state"><div class="empty-title">No integrations yet</div>Add the first third-party system that will send data into the queue.</td></tr>`;
      loadMoreBtn.style.display = "none";
      return;
    }

    const rows = filtered();
    if (rows.length === 0) {
      tbody.innerHTML = `<tr><td colspan="6" class="empty-state"><div class="empty-title">No integrations match your search</div>Try a different name or description.</td></tr>`;
      loadMoreBtn.style.display = "none";
      return;
    }

    const shown = visibleSlice(rows, visibleCount);

    tbody.innerHTML = shown.map((p) => `
      <tr>
        <td><strong>${escapeHtml(p.product_name)}</strong></td>
        <td>${escapeHtml(p.product_description) || "—"}</td>
        <td>${maskedCell(p.id, "key", "authentication key")}</td>
        <td>${maskedCell(p.id, "iv", "authentication IV")}</td>
        <td>${formatDate(p.product_modified_at)}</td>
        <td>
          <button class="btn btn-sm" data-edit="${p.id}">Edit</button>
          <button class="btn btn-sm btn-danger" data-delete="${p.id}">Delete</button>
        </td>
      </tr>
    `).join("");

    updateLoadMoreButton(loadMoreBtn, shown.length, rows.length);
  }

  function exportCsv(rows, filename) {
    downloadCsv(filename, [
      { header: "Name", value: (p) => p.product_name },
      { header: "Description", value: (p) => p.product_description },
      { header: "Authentication Key", value: (p) => p.product_authentication_key },
      { header: "Authentication IV", value: (p) => p.product_authentication_iv },
      { header: "Updated", value: (p) => p.product_modified_at },
    ], rows);
  }

  setupDropdown(document.getElementById("exportMenuBtn"), document.getElementById("exportMenu"));
  document.getElementById("exportVisibleBtn").addEventListener("click", () => exportCsv(filtered(), "integrations-visible.csv"));
  document.getElementById("exportAllBtn").addEventListener("click", () => exportCsv(products, "integrations-all.csv"));

  searchInput.addEventListener("input", () => { visibleCount = PAGE_SIZE; render(); });
  loadMoreBtn.addEventListener("click", () => { visibleCount += PAGE_SIZE; render(); });

  function openDialog(product) {
    hideBanner(formError);
    form.reset();
    document.getElementById("productId").value = product ? product.id : "";
    document.getElementById("productName").value = product ? product.product_name : "";
    document.getElementById("productDescription").value = product ? product.product_description : "";
    document.getElementById("productKey").value = product ? product.product_authentication_key : "";
    document.getElementById("productIv").value = product ? product.product_authentication_iv : "";
    dialogTitle.textContent = product ? "Edit integration" : "New integration";
    dialog.showModal();
  }

  document.getElementById("newProductBtn").addEventListener("click", () => openDialog(null));

  dialog.addEventListener("click", (ev) => {
    if (ev.target.hasAttribute("data-close")) dialog.close();
  });

  tbody.addEventListener("click", async (ev) => {
    const editId = ev.target.getAttribute("data-edit");
    const deleteId = ev.target.getAttribute("data-delete");
    const copyField = ev.target.getAttribute("data-copy");
    const copyId = ev.target.getAttribute("data-copy-id");

    if (copyField) {
      const product = products.find((p) => p.id === copyId);
      if (!product) return;
      const value = copyField === "key" ? product.product_authentication_key : product.product_authentication_iv;
      try {
        await copyToClipboard(value);
        const original = ev.target.textContent;
        ev.target.textContent = "Copied!";
        setTimeout(() => { ev.target.textContent = original; }, 1500);
      } catch (e) {
        showBanner(errorBanner, "Couldn't copy to clipboard — copy it manually from the edit dialog.");
      }
      return;
    }

    if (editId) {
      openDialog(products.find((p) => p.id === editId));
    } else if (deleteId) {
      const product = products.find((p) => p.id === deleteId);
      if (!confirm(`Delete integration "${product.product_name}"? This can't be undone.`)) return;
      try {
        await Api.del(`/api/products/${deleteId}`);
        await load();
      } catch (e) {
        showBanner(errorBanner, e.message);
      }
    }
  });

  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    hideBanner(formError);
    const id = document.getElementById("productId").value;
    const payload = {
      product_name: document.getElementById("productName").value,
      product_description: document.getElementById("productDescription").value,
      product_authentication_key: document.getElementById("productKey").value,
      product_authentication_iv: document.getElementById("productIv").value,
    };
    try {
      if (id) {
        await Api.put(`/api/products/${id}`, payload);
      } else {
        await Api.post("/api/products", payload);
      }
      dialog.close();
      await load();
    } catch (e) {
      showBanner(formError, e.message);
    }
  });

  await load();
})();
