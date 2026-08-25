(function init() {
  const errorBanner = document.getElementById("errorBanner");
  const loginForm = document.getElementById("loginForm");
  const totpForm = document.getElementById("totpForm");
  const loginSub = document.getElementById("loginSub");
  const footnote = document.getElementById("footnote");

  loginForm.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    hideBanner(errorBanner);
    const email = document.getElementById("email").value;
    const password = document.getElementById("password").value;
    try {
      const res = await Api.post("/api/auth/login", { admin_email: email, admin_password: password });
      if (res.status === "2fa_required") {
        loginForm.style.display = "none";
        loginSub.textContent = "Two-factor authentication is enabled on this account.";
        totpForm.style.display = "block";
        document.getElementById("totpCode").focus();
      } else if (res.status === "2fa_setup_required") {
        window.location.href = "/2fa-setup.html";
      } else {
        window.location.href = "/dashboard.html";
      }
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  });

  totpForm.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    hideBanner(errorBanner);
    const code = document.getElementById("totpCode").value;
    try {
      await Api.post("/api/auth/2fa/verify", { totp_code: code });
      window.location.href = "/dashboard.html";
    } catch (e) {
      showBanner(errorBanner, e.message);
    }
  });

  footnote.textContent = "Internal tool — access is limited to authorized console admins. New console admin accounts are added by an existing console admin.";
})();
