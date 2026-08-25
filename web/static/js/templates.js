(async function init() {
  if (!(await requireSession())) return;

  const errorBanner = document.getElementById("errorBanner");
  const tbody = document.getElementById("templatesBody");
  const dialog = document.getElementById("templateDialog");
  const form = document.getElementById("templateForm");
  const formError = document.getElementById("templateFormError");
  const dialogTitle = document.getElementById("templateDialogTitle");
  const productSelect = document.getElementById("templateProduct");
  const nameInput = document.getElementById("templateName");
  const nameHint = document.getElementById("templateNameHint");
  const entryPointInput = document.getElementById("templateEntryPoint");
  const entryPointHint = document.getElementById("templateEntryPointHint");
  const entryPointPreview = document.getElementById("entryPointPreview");
  const copyEntryPointBtn = document.getElementById("copyEntryPointBtn");
  const searchInput = document.getElementById("templateSearch");
  const loadMoreBtn = document.getElementById("templatesLoadMoreBtn");
  const routingFieldWrap = document.getElementById("routingFieldWrap");
  const routingFieldInput = document.getElementById("templateRoutingField");
  const destinationsList = document.getElementById("destinationsList");
  const addDestinationBtn = document.getElementById("addDestinationBtn");
  const stepIndicator = document.getElementById("stepIndicator");
  const wizardBackBtn = document.getElementById("wizardBackBtn");
  const wizardNextBtn = document.getElementById("wizardNextBtn");
  const reviewContent = document.getElementById("reviewContent");
  const downloadSampleDocBtn = document.getElementById("downloadSampleDocBtn");

  const PAGE_SIZE = 10;
  const TOTAL_STEPS = 3;
  let templates = [];
  let products = [];
  let visibleCount = PAGE_SIZE;
  let entryPointTouched = false; // stops auto-slugging once the admin edits it directly
  let currentStep = 1;

  function filtered() {
    return filterRows(templates, searchInput.value, (t) => [
      t.template_name,
      productName(t.product_id),
      t.template_entry_point,
      ...t.destinations.map((d) => d.destination_name),
    ]);
  }

  function productName(id) {
    const p = products.find((p) => p.id === id);
    return p ? p.product_name : id;
  }

  function fullEntryPointUrl(slugOrPath) {
    const path = slugOrPath.startsWith("/ingest/") ? slugOrPath : `/ingest/${slugOrPath.replace(/^\/+/, "")}`;
    return window.location.origin + path;
  }

  async function copyEntryPointUrl(button, path) {
    try {
      await copyToClipboard(fullEntryPointUrl(path));
      const original = button.textContent;
      button.textContent = "Copied!";
      setTimeout(() => { button.textContent = original; }, 1500);
    } catch (e) {
      showBanner(errorBanner, "Couldn't copy to clipboard — copy the URL manually.");
    }
  }

  async function load() {
    try {
      [templates, products] = await Promise.all([Api.get("/api/templates"), Api.get("/api/products")]);
      productSelect.innerHTML = products.map((p) => `<option value="${p.id}">${escapeHtml(p.product_name)}</option>`).join("");
      visibleCount = PAGE_SIZE;
      render();
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  }

  function render() {
    if (templates.length === 0) {
      tbody.innerHTML = `<tr><td colspan="7" class="empty-state"><div class="empty-title">No transmission channels yet</div>Create one to start routing an integration's transmission requests to one or more destinations.</td></tr>`;
      loadMoreBtn.style.display = "none";
      return;
    }

    const rows = filtered();
    if (rows.length === 0) {
      tbody.innerHTML = `<tr><td colspan="7" class="empty-state"><div class="empty-title">No transmission channels match your search</div>Try a different name, integration, entry point, or destination.</td></tr>`;
      loadMoreBtn.style.display = "none";
      return;
    }

    const shown = visibleSlice(rows, visibleCount);

    tbody.innerHTML = shown.map((t) => `
      <tr>
        <td><strong>${escapeHtml(t.template_name)}</strong></td>
        <td>${escapeHtml(productName(t.product_id))}</td>
        <td class="mono">
          ${escapeHtml(t.template_entry_point)}
          <button type="button" class="btn btn-sm copy-btn" data-copy-row="${escapeHtml(t.template_entry_point)}" title="Copy full entry point URL">Copy</button>
        </td>
        <td title="${escapeHtml(t.destinations.map((d) => d.destination_name).join(", "))}">${t.destinations.length} destination${t.destinations.length === 1 ? "" : "s"}</td>
        <td>${t.template_request_ttl_days}d</td>
        <td><span class="badge badge-${t.template_status}">${t.template_status}</span></td>
        <td>
          <button class="btn btn-sm" data-edit="${t.id}">Edit</button>
          ${t.template_status === "active"
            ? `<button class="btn btn-sm" data-deactivate="${t.id}">Deactivate</button>`
            : `<button class="btn btn-sm" data-activate="${t.id}">Activate</button>`}
          <button class="btn btn-sm btn-danger" data-delete="${t.id}">Delete</button>
        </td>
      </tr>
    `).join("");

    updateLoadMoreButton(loadMoreBtn, shown.length, rows.length);
  }

  function exportCsv(rows, filename) {
    downloadCsv(filename, [
      { header: "Name", value: (t) => t.template_name },
      { header: "Integration", value: (t) => productName(t.product_id) },
      { header: "Entry point", value: (t) => t.template_entry_point },
      { header: "Routing field", value: (t) => t.template_routing_field || "" },
      { header: "Destinations", value: (t) => t.destinations.map((d) => d.destination_name).join("; ") },
      { header: "TTL (days)", value: (t) => t.template_request_ttl_days },
      { header: "Status", value: (t) => t.template_status },
    ], rows);
  }

  setupDropdown(document.getElementById("exportMenuBtn"), document.getElementById("exportMenu"));
  document.getElementById("exportVisibleBtn").addEventListener("click", () => exportCsv(filtered(), "transmission-channels-visible.csv"));
  document.getElementById("exportAllBtn").addEventListener("click", () => exportCsv(templates, "transmission-channels-all.csv"));

  searchInput.addEventListener("input", () => { visibleCount = PAGE_SIZE; render(); });
  loadMoreBtn.addEventListener("click", () => { visibleCount += PAGE_SIZE; render(); });

  // --- destinations editor: an in-dialog repeatable list. Rows are fully
  // re-rendered on add/remove (reading whatever's currently in the DOM
  // first so nothing typed is lost) rather than tracked as separate state,
  // since that's simpler than keeping a parallel model in sync. ---

  function emptyDestination() {
    return { id: "", destination_name: "", destination_routing_value: "", destination_end_point: "", destination_throttling_second: 1 };
  }

  function destinationRowHtml(d, index) {
    return `
      <div class="destination-row">
        <input type="hidden" class="dest-id" value="${escapeHtml(d.id || "")}">
        <div class="destination-row-header">
          <strong>Destination ${index + 1}</strong>
          <button type="button" class="btn btn-sm btn-danger dest-remove">Remove</button>
        </div>
        <div class="field-row">
          <div class="field">
            <label>Name</label>
            <input type="text" class="dest-name" required value="${escapeHtml(d.destination_name || "")}">
          </div>
          <div class="field dest-routing-value-field">
            <label>Routing value</label>
            <input type="text" class="dest-routing-value" placeholder="order.created" value="${escapeHtml(d.destination_routing_value || "")}">
          </div>
        </div>
        <div class="field">
          <label>Destination endpoint URL</label>
          <input type="text" class="dest-endpoint" required placeholder="https://internal.example.com/inventory/purchase" value="${escapeHtml(d.destination_end_point || "")}">
        </div>
        <div class="field">
          <label>Gap between sends (seconds)</label>
          <input type="number" class="dest-throttle" required min="0" step="0.1" value="${d.destination_throttling_second ?? 1}">
        </div>
      </div>
    `;
  }

  function readDestinationsFromDom() {
    return Array.from(destinationsList.querySelectorAll(".destination-row")).map((row) => ({
      id: row.querySelector(".dest-id").value,
      destination_name: row.querySelector(".dest-name").value,
      destination_routing_value: row.querySelector(".dest-routing-value").value,
      destination_end_point: row.querySelector(".dest-endpoint").value,
      destination_throttling_second: parseFloat(row.querySelector(".dest-throttle").value) || 0,
    }));
  }

  function renderDestinationRows(destinations) {
    destinationsList.innerHTML = destinations.map((d, i) => destinationRowHtml(d, i)).join("");
    const multi = destinations.length > 1;
    routingFieldWrap.style.display = multi ? "" : "none";

    destinationsList.querySelectorAll(".destination-row").forEach((row, i) => {
      row.querySelector(".dest-routing-value-field").style.display = multi ? "" : "none";
      const removeBtn = row.querySelector(".dest-remove");
      removeBtn.style.display = destinations.length > 1 ? "" : "none";
      removeBtn.addEventListener("click", () => {
        const current = readDestinationsFromDom();
        current.splice(i, 1);
        renderDestinationRows(current);
      });
    });
  }

  addDestinationBtn.addEventListener("click", () => {
    const current = readDestinationsFromDom();
    current.push(emptyDestination());
    renderDestinationRows(current);
  });

  // --- 3-step wizard: Channel Detail -> Channel Setup -> Review. All
  // fields live in one <form> the whole time (so one submit still collects
  // everything); each step is just shown/hidden. ---

  function showStep(step) {
    currentStep = step;
    for (let n = 1; n <= TOTAL_STEPS; n++) {
      document.getElementById(`step${n}`).style.display = n === step ? "" : "none";
    }
    stepIndicator.querySelectorAll(".step-indicator-item").forEach((el) => {
      const n = Number(el.dataset.step);
      el.classList.toggle("active", n === step);
      el.classList.toggle("done", n < step);
    });
    wizardBackBtn.textContent = step === 1 ? "Cancel" : "Back";
    wizardNextBtn.textContent = step === TOTAL_STEPS ? "Save transmission channel" : "Next";
    if (step === TOTAL_STEPS) renderReview();
  }

  // Scoped to just the active step's fields, so an admin can't get stuck
  // on a later step because of a hidden, unrelated invalid field elsewhere
  // in the form — and can't skip a required field on the active step either,
  // since hidden fields are excluded from native validation but this one
  // isn't hidden while it's being checked.
  function validateCurrentStep() {
    const invalid = document.getElementById(`step${currentStep}`).querySelector(":invalid");
    if (invalid) {
      invalid.reportValidity();
      return false;
    }
    return true;
  }

  wizardBackBtn.addEventListener("click", () => {
    if (currentStep === 1) {
      dialog.close();
      return;
    }
    showStep(currentStep - 1);
  });

  wizardNextBtn.addEventListener("click", () => {
    if (!validateCurrentStep()) return;
    if (currentStep === TOTAL_STEPS) {
      form.requestSubmit();
      return;
    }
    showStep(currentStep + 1);
  });

  function renderReview() {
    const destinations = readDestinationsFromDom();
    const multi = destinations.length > 1;
    const productLabel = productSelect.selectedOptions[0] ? productSelect.selectedOptions[0].textContent : "—";
    const description = document.getElementById("templateDescription").value;
    const ttl = document.getElementById("templateTtl").value;

    reviewContent.innerHTML = `
      <div class="detail-block"><div class="detail-label">Integration</div>${escapeHtml(productLabel)}</div>
      <div class="detail-block"><div class="detail-label">Name</div>${escapeHtml(nameInput.value)}</div>
      ${description ? `<div class="detail-block"><div class="detail-label">Description</div>${escapeHtml(description)}</div>` : ""}
      <div class="detail-block"><div class="detail-label">Entry point</div><span class="mono">${escapeHtml(fullEntryPointUrl(entryPointInput.value))}</span></div>
      <div class="detail-block"><div class="detail-label">Keep transmission requests for</div>${escapeHtml(ttl)} days</div>
      ${multi ? `<div class="detail-block"><div class="detail-label">Routing field</div><span class="mono">${escapeHtml(routingFieldInput.value)}</span></div>` : ""}
      <div class="detail-block">
        <div class="detail-label">Destinations (${destinations.length})</div>
        ${destinations.map((d) => `
          <div class="destination-row">
            <strong>${escapeHtml(d.destination_name || "(unnamed)")}</strong>
            ${multi ? ` — routes when <span class="mono">${escapeHtml(routingFieldInput.value)}</span> = <span class="mono">${escapeHtml(d.destination_routing_value)}</span>` : ""}
            <br><span class="mono" style="font-size:12px">${escapeHtml(d.destination_end_point)}</span>
            <br><span style="font-size:12px;color:var(--ink-muted)">Throttle: ${d.destination_throttling_second}s</span>
          </div>
        `).join("")}
      </div>
    `;
  }

  // Encrypts a sample payload exactly the way a real third party would —
  // AES-256-CBC (PKCS7-padded, which is what SubtleCrypto's AES-CBC does
  // natively), then base64 + URL-encoded — using the integration's real
  // key/IV, so the sample cURL call in the generated doc is a genuine
  // working request, not just a placeholder. Returns null (caller falls
  // back to a placeholder) if the page isn't in a secure context —
  // SubtleCrypto is unavailable on plain http except on localhost, and
  // this app may well be deployed on a plain-http internal host.
  async function encryptForSample(payloadObj, key, iv) {
    if (!window.isSecureContext || !window.crypto || !window.crypto.subtle) return null;
    if (!key || !iv || key.length !== 32 || iv.length !== 16) return null;
    try {
      const enc = new TextEncoder();
      const cryptoKey = await crypto.subtle.importKey("raw", enc.encode(key), { name: "AES-CBC" }, false, ["encrypt"]);
      const ciphertext = await crypto.subtle.encrypt({ name: "AES-CBC", iv: enc.encode(iv) }, cryptoKey, enc.encode(JSON.stringify(payloadObj)));
      let binary = "";
      new Uint8Array(ciphertext).forEach((b) => { binary += String.fromCharCode(b); });
      return encodeURIComponent(btoa(binary));
    } catch (e) {
      return null;
    }
  }

  // Builds a hand-off doc for whoever implements the sending side — full
  // ingest URL, headers, the encryption recipe, the integration's actual
  // key/IV (already visible to any console admin elsewhere in the app —
  // this is a display convenience, not a new exposure), the routing key
  // if this channel fans out, a sample plaintext + encrypted body per
  // destination, and a ready-to-run cURL call for each.
  async function buildSampleDoc() {
    const product = products.find((p) => p.id === productSelect.value);
    const destinations = readDestinationsFromDom();
    const multi = destinations.length > 1;
    const url = fullEntryPointUrl(entryPointInput.value);
    const routingField = routingFieldInput.value;
    const ttl = document.getElementById("templateTtl").value;
    const key = product ? product.product_authentication_key : "";
    const iv = product ? product.product_authentication_iv : "";

    const targets = multi
      ? destinations.map((d) => ({
          label: d.destination_name || "(unnamed destination)",
          payload: { [routingField || "your_routing_field"]: d.destination_routing_value || "..." },
        }))
      : [{
          label: destinations[0] ? (destinations[0].destination_name || "(unnamed destination)") : "destination",
          payload: { your_field: "your_value" },
        }];

    for (const t of targets) {
      t.encrypted = await encryptForSample(t.payload, key, iv);
    }

    const lines = [];
    lines.push(`# ${nameInput.value || "Transmission Channel"} — Integration Guide`);
    lines.push("");
    lines.push(`Integration: ${product ? product.product_name : ""}`);
    lines.push(`Entry point: POST ${url}`);
    lines.push("");
    lines.push("## Headers");
    lines.push("");
    lines.push("```");
    lines.push("Content-Type: text/plain");
    lines.push("```");
    lines.push("");
    lines.push("No separate API key or auth header is needed — the AES-256 key and IV below are what authenticates and decrypts each request, so guard them the way you'd guard a password.");
    lines.push("");
    lines.push("## Encryption");
    lines.push("");
    lines.push("AES-256-CBC. Encrypt your JSON payload, then base64-encode and URL-encode the ciphertext:");
    lines.push("");
    lines.push("```php");
    lines.push("$encrypted = openssl_encrypt(json_encode($data), 'AES-256-CBC', $key, OPENSSL_RAW_DATA, $iv);");
    lines.push("$encoded   = urlencode(base64_encode($encrypted));");
    lines.push("```");
    lines.push("");
    lines.push(`Authentication key (32 chars): ${key || "(select an integration)"}`);
    lines.push(`Authentication IV (16 chars): ${iv}`);
    lines.push("");
    lines.push("## Routing key");
    lines.push("");
    if (multi) {
      lines.push(`Include "${routingField}" in your JSON payload with one of these values — Queue Manager reads it out of your decrypted payload and fans the request out to the matching destination:`);
      lines.push("");
      destinations.forEach((d) => {
        lines.push(`- "${d.destination_routing_value}" → ${d.destination_name} (${d.destination_end_point})`);
      });
    } else {
      lines.push("Not needed — this channel has a single destination, so every request that arrives is sent straight there. No routing field is read from the payload.");
    }
    lines.push("");
    lines.push("## Sample request bodies");
    lines.push("");
    targets.forEach((t) => {
      lines.push(`### ${t.label}`);
      lines.push("");
      lines.push("Plaintext JSON (before encryption):");
      lines.push("");
      lines.push("```json");
      lines.push(JSON.stringify(t.payload, null, 2));
      lines.push("```");
      lines.push("");
      if (t.encrypted) {
        lines.push("Encrypted body to POST — generated live with this integration's real key/IV, safe to send as-is for a real test call:");
        lines.push("");
        lines.push("```");
        lines.push(t.encrypted);
        lines.push("```");
      } else {
        lines.push("Encrypted body to POST — run the PHP snippet above on the JSON payload to produce this (couldn't generate it live here, most likely because this page isn't loaded over HTTPS or localhost):");
        lines.push("");
        lines.push("```");
        lines.push("<paste the output of urlencode(base64_encode(openssl_encrypt(...))) here>");
        lines.push("```");
      }
      lines.push("");
    });
    lines.push("## Sample cURL request");
    lines.push("");
    targets.forEach((t) => {
      lines.push(`# ${t.label}`);
      lines.push("```bash");
      lines.push(`curl -X POST "${url}" \\`);
      lines.push(`  -H "Content-Type: text/plain" \\`);
      lines.push(`  --data '${t.encrypted || "<ENCRYPTED_PAYLOAD>"}'`);
      lines.push("```");
      lines.push("");
    });
    lines.push("## Response");
    lines.push("");
    lines.push("Return an HTTP 2xx status for the delivery to be marked successful. Any non-2xx status or connection error is recorded as failed and can be manually retried from the admin console.");
    lines.push("");
    lines.push("## Pacing & retention");
    lines.push("");
    destinations.forEach((d) => {
      lines.push(`- ${d.destination_name || "(unnamed destination)"}: throttled to one send every ${d.destination_throttling_second}s`);
    });
    lines.push(`- Requests are kept for ${ttl} days`);
    lines.push("");
    return lines.join("\n");
  }

  downloadSampleDocBtn.addEventListener("click", async () => {
    const filename = `${slugify(nameInput.value) || "transmission-channel"}-integration-guide.md`;
    downloadText(filename, await buildSampleDoc(), "text/markdown;charset=utf-8;");
  });

  // --- unique slug suggestion, linked to the Name field until the admin
  // edits Entry point directly (same UX as e.g. a CMS permalink field) ---

  function uniqueSlug(base, excludeId) {
    const taken = new Set(
      templates
        .filter((t) => t.id !== excludeId)
        .map((t) => t.template_entry_point.replace(/^\/ingest\//, ""))
    );
    const root = base || "template";
    let slug = root;
    let n = 2;
    while (taken.has(slug)) {
      slug = `${root}-${n}`;
      n++;
    }
    return slug;
  }

  function updateEntryPointPreview() {
    const slug = entryPointInput.value.trim();
    entryPointPreview.textContent = slug ? fullEntryPointUrl(slug).replace(/^https?:\/\//, "") : "/ingest/…";
  }

  function currentEditingId() {
    return document.getElementById("templateId").value || undefined;
  }

  function checkNameUnique() {
    const name = nameInput.value.trim().toLowerCase();
    const conflict = name && templates.some((t) => t.id !== currentEditingId() && t.template_name.trim().toLowerCase() === name);
    nameHint.textContent = conflict ? "Another transmission channel already uses this name." : "";
    nameHint.classList.toggle("show", !!conflict);
    return !conflict;
  }

  function checkEntryPointUnique() {
    const slug = entryPointInput.value.trim();
    const conflict = slug && templates.some((t) => t.id !== currentEditingId() && t.template_entry_point.replace(/^\/ingest\//, "") === slug);
    entryPointHint.textContent = conflict ? "Another transmission channel already uses this entry point." : "";
    entryPointHint.classList.toggle("show", !!conflict);
    return !conflict;
  }

  nameInput.addEventListener("input", () => {
    if (!entryPointTouched) {
      entryPointInput.value = uniqueSlug(slugify(nameInput.value), currentEditingId());
      updateEntryPointPreview();
      checkEntryPointUnique();
    }
    checkNameUnique();
  });

  entryPointInput.addEventListener("input", () => {
    entryPointTouched = true;
    entryPointInput.value = slugify(entryPointInput.value);
    updateEntryPointPreview();
    checkEntryPointUnique();
  });

  copyEntryPointBtn.addEventListener("click", () => copyEntryPointUrl(copyEntryPointBtn, entryPointInput.value));

  function openDialog(tmpl) {
    hideBanner(formError);
    form.reset();
    nameHint.classList.remove("show");
    entryPointHint.classList.remove("show");
    entryPointTouched = !!tmpl; // don't auto-slug over an existing, possibly already-integrated entry point
    document.getElementById("templateId").value = tmpl ? tmpl.id : "";
    productSelect.value = tmpl ? tmpl.product_id : (products[0] && products[0].id) || "";
    nameInput.value = tmpl ? tmpl.template_name : "";
    document.getElementById("templateDescription").value = tmpl ? tmpl.template_description : "";
    entryPointInput.value = tmpl ? tmpl.template_entry_point.replace(/^\/ingest\//, "") : "";
    document.getElementById("templateTtl").value = tmpl ? tmpl.template_request_ttl_days : 60;
    routingFieldInput.value = tmpl ? (tmpl.template_routing_field || "") : "";
    renderDestinationRows(tmpl && tmpl.destinations && tmpl.destinations.length ? tmpl.destinations : [emptyDestination()]);
    updateEntryPointPreview();
    dialogTitle.textContent = tmpl ? "Edit transmission channel" : "New transmission channel";
    showStep(1);
    dialog.showModal();
  }

  document.getElementById("newTemplateBtn").addEventListener("click", () => {
    if (products.length === 0) {
      showBanner(errorBanner, "Add an integration before creating a transmission channel.");
      return;
    }
    openDialog(null);
  });

  dialog.addEventListener("click", (ev) => {
    if (ev.target.hasAttribute("data-close")) dialog.close();
  });

  tbody.addEventListener("click", async (ev) => {
    const editId = ev.target.getAttribute("data-edit");
    const deleteId = ev.target.getAttribute("data-delete");
    const activateId = ev.target.getAttribute("data-activate");
    const deactivateId = ev.target.getAttribute("data-deactivate");
    const copyRowPath = ev.target.getAttribute("data-copy-row");

    if (copyRowPath) {
      await copyEntryPointUrl(ev.target, copyRowPath);
      return;
    }

    try {
      if (editId) {
        openDialog(templates.find((t) => t.id === editId));
      } else if (deleteId) {
        const t = templates.find((t) => t.id === deleteId);
        if (!confirm(`Delete transmission channel "${t.template_name}"? This stops its consumer(s) and can't be undone.`)) return;
        await Api.del(`/api/templates/${deleteId}`);
        await load();
      } else if (activateId) {
        const t = templates.find((t) => t.id === activateId);
        if (!confirm(`Activate transmission channel "${t.template_name}"? This starts processing for every destination on it.`)) return;
        await Api.patch(`/api/templates/${activateId}/activate`);
        await load();
      } else if (deactivateId) {
        const t = templates.find((t) => t.id === deactivateId);
        if (!confirm(`Deactivate transmission channel "${t.template_name}"? This pauses processing — anything already queued stays put and picks up again when reactivated.`)) return;
        await Api.patch(`/api/templates/${deactivateId}/deactivate`);
        await load();
      }
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  });

  form.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    hideBanner(formError);
    if (!checkNameUnique() || !checkEntryPointUnique()) {
      showBanner(formError, "Fix the highlighted fields before saving.");
      return;
    }
    const id = document.getElementById("templateId").value;
    const payload = {
      product_id: productSelect.value,
      template_name: nameInput.value,
      template_description: document.getElementById("templateDescription").value,
      template_entry_point: entryPointInput.value,
      template_routing_field: routingFieldInput.value,
      destinations: readDestinationsFromDom(),
      template_request_ttl_days: parseInt(document.getElementById("templateTtl").value, 10),
    };
    try {
      if (id) {
        await Api.put(`/api/templates/${id}`, payload);
      } else {
        await Api.post("/api/templates", payload);
      }
      dialog.close();
      await load();
    } catch (e) {
      showBanner(formError, e.message);
    }
  });

  await load();
})();
