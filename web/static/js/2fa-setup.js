(async function init() {
  const errorBanner = document.getElementById("errorBanner");
  const loadingState = document.getElementById("loadingState");
  const setupState = document.getElementById("setupState");
  const qrImage = document.getElementById("qrImage");
  const secretText = document.getElementById("secretText");
  const copySecretBtn = document.getElementById("copySecretBtn");
  const confirmForm = document.getElementById("confirmForm");
  const logoutBtn = document.getElementById("logoutBtn");

  let setupToken = null;

  logoutBtn.addEventListener("click", async () => {
    await Api.post("/api/auth/logout").catch(() => {});
    window.location.href = "/login.html";
  });

  try {
    const res = await Api.post("/api/auth/2fa/setup");
    setupToken = res.setup_token;
    qrImage.src = res.qr_data_uri;
    secretText.textContent = res.secret;
    loadingState.style.display = "none";
    setupState.style.display = "block";
    document.getElementById("totpCode").focus();
  } catch (e) {
    loadingState.style.display = "none";
    if (e.status === 401) {
      window.location.href = "/login.html";
      return;
    }
    showBanner(errorBanner, e.message);
  }

  copySecretBtn.addEventListener("click", async () => {
    try {
      await copyToClipboard(secretText.textContent);
      const original = copySecretBtn.textContent;
      copySecretBtn.textContent = "Copied!";
      setTimeout(() => { copySecretBtn.textContent = original; }, 1500);
    } catch (e) {
      showBanner(errorBanner, "Couldn't copy to clipboard — copy it manually.");
    }
  });

  confirmForm.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    hideBanner(errorBanner);
    const code = document.getElementById("totpCode").value;
    try {
      await Api.post("/api/auth/2fa/setup/confirm", { setup_token: setupToken, totp_code: code });
      window.location.href = "/dashboard.html";
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  });
})();
