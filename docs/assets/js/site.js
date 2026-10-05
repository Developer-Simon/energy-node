/* Energy Node docs: theme toggle, table of contents, mobile navigation,
   search loader and forwarding of anchors that moved to a subpage.
   No build step, no dependencies. */
(function () {
  "use strict";

  var root = document.documentElement;
  var KEY = "en-docs-theme";
  var base = root.dataset.base || "/";

  // ---- Theme ---------------------------------------------------------------

  function systemDark() {
    return window.matchMedia && matchMedia("(prefers-color-scheme: dark)").matches;
  }
  function effectiveTheme() {
    return root.dataset.theme || (systemDark() ? "dark" : "light");
  }
  function setTheme(mode) {
    root.dataset.theme = mode;
    try { localStorage.setItem(KEY, mode); } catch (e) { /* private mode */ }
    document.dispatchEvent(new CustomEvent("en:themechange", { detail: mode }));
  }
  function initTheme() {
    var btn = document.getElementById("theme-toggle");
    if (!btn) return;
    btn.addEventListener("click", function () {
      setTheme(effectiveTheme() === "dark" ? "light" : "dark");
    });
  }

  // ---- Anchors that moved to a subpage ---------------------------------------

  function initAnchorMoves() {
    var el = document.getElementById("anchor-moves");
    var id = decodeURIComponent(location.hash.slice(1));
    if (!el || !id || document.getElementById(id)) return;
    var moves;
    try { moves = JSON.parse(el.textContent); } catch (e) { return; }
    if (moves && typeof moves[id] === "string") location.replace(moves[id]);
  }

  // ---- Table of contents -----------------------------------------------------

  function initToc() {
    var toc = document.getElementById("toc");
    var body = document.querySelector(".doc-body");
    if (!toc || !body) return;
    var heads = Array.prototype.filter.call(body.querySelectorAll("h2[id], h3[id]"), function (h) {
      return !h.closest(".hero");
    });
    if (heads.length < 2) return;

    var title = document.createElement("p");
    title.className = "toc__title";
    title.textContent = "On this page";
    var list = document.createElement("ul");
    var links = {};
    heads.forEach(function (h) {
      var li = document.createElement("li");
      li.className = h.tagName === "H3" ? "toc-h3" : "toc-h2";
      var a = document.createElement("a");
      a.href = "#" + h.id;
      a.textContent = h.textContent;
      li.appendChild(a);
      list.appendChild(li);
      links[h.id] = a;
    });
    toc.appendChild(title);
    toc.appendChild(list);
    toc.hidden = false;

    if (!("IntersectionObserver" in window)) return;
    var visible = {};
    var current = null;
    var obs = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) { visible[e.target.id] = e.isIntersecting; });
      var first = heads.find(function (h) { return visible[h.id]; });
      if (!first) return;
      if (current) current.classList.remove("is-active");
      current = links[first.id];
      current.classList.add("is-active");
    }, { rootMargin: "-60px 0px -65% 0px" });
    heads.forEach(function (h) { obs.observe(h); });
  }

  // ---- Wide tables scroll in their own box -----------------------------------

  function wrapTables() {
    document.querySelectorAll(".doc-body table").forEach(function (t) {
      if (t.parentElement.classList.contains("table-wrap")) return;
      var wrap = document.createElement("div");
      wrap.className = "table-wrap";
      t.parentNode.insertBefore(wrap, t);
      wrap.appendChild(t);
    });
  }

  // ---- Mobile navigation -----------------------------------------------------

  function initNav() {
    var btn = document.getElementById("nav-toggle");
    var backdrop = document.getElementById("nav-backdrop");
    var sidebar = document.getElementById("sidebar");
    if (!btn || !backdrop || !sidebar) return;
    function set(open) {
      document.body.classList.toggle("nav-open", open);
      backdrop.hidden = !open;
      btn.setAttribute("aria-expanded", String(open));
      btn.setAttribute("aria-label", open ? "Close navigation" : "Open navigation");
    }
    btn.addEventListener("click", function () { set(!document.body.classList.contains("nav-open")); });
    backdrop.addEventListener("click", function () { set(false); });
    document.addEventListener("keydown", function (e) { if (e.key === "Escape") set(false); });
    sidebar.addEventListener("click", function (e) { if (e.target.closest("a")) set(false); });
    var cur = sidebar.querySelector('[aria-current="page"]');
    if (cur && cur.scrollIntoView) cur.scrollIntoView({ block: "center" });
  }

  // ---- Search (Pagefind) -----------------------------------------------------

  function initSearch() {
    var box = document.getElementById("doc-search");
    if (!box || !window.fetch) return;
    fetch(base + "pagefind/pagefind-entry.json", { method: "HEAD" }).then(function (r) {
      if (r.ok) box.hidden = false;
    }).catch(function () {});
  }

  // ---- Start -----------------------------------------------------------------

  window.EnergyDocs = { setTheme: setTheme };
  initAnchorMoves();
  initTheme();
  wrapTables();
  initToc();
  initNav();
  initSearch();
})();
