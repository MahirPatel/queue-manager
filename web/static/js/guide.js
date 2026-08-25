(async function () {
  const me = await requireSession();
  if (!me) return;

  const menuButtons = Array.from(document.querySelectorAll("#guideMenu button"));
  const topics = Array.from(document.querySelectorAll(".guide-topic"));

  function showTopic(key) {
    menuButtons.forEach((b) => b.classList.toggle("active", b.dataset.topic === key));
    topics.forEach((t) => t.classList.toggle("active", t.id === `topic-${key}`));
    if (history.replaceState) history.replaceState(null, "", `#${key}`);
  }

  menuButtons.forEach((btn) => {
    btn.addEventListener("click", () => showTopic(btn.dataset.topic));
  });

  const initial = (window.location.hash || "").replace("#", "");
  if (initial && menuButtons.some((b) => b.dataset.topic === initial)) {
    showTopic(initial);
  }
})();
