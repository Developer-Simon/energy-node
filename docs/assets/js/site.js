/* Energy Node docs: theme toggle, table of contents, mobile navigation,
   search loader, forwarding of anchors that moved to a subpage, the What's new view switch, opening a linked details block, the image dialog and the gallery of pages with gallery: true.
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

  var rebuildToc = function () {};
  var tocObserver = null;

  function initToc() {
    var toc = document.getElementById("toc");
    var body = document.querySelector(".doc-body");
    if (!toc || !body) return;
    rebuildToc = function () { buildToc(toc, body); };
    buildToc(toc, body);
  }

  // Headings in a hidden view (What's new) are left out, so the list follows the view on screen.
  function buildToc(toc, body) {
    if (tocObserver) { tocObserver.disconnect(); tocObserver = null; }
    toc.innerHTML = "";
    var heads = Array.prototype.filter.call(body.querySelectorAll("h2[id], h3[id]"), function (h) {
      return !h.closest(".hero") && !h.closest("[hidden]");
    });
    toc.hidden = heads.length < 2;
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
      // The label only: a What's new release heading carries its date in a .wn-date span.
      var label = h.cloneNode(true);
      Array.prototype.forEach.call(label.querySelectorAll(".wn-date"), function (d) { d.remove(); });
      a.textContent = label.textContent.trim();
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
    tocObserver = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) { visible[e.target.id] = e.isIntersecting; });
      var first = heads.find(function (h) { return visible[h.id]; });
      if (!first) return;
      if (current) current.classList.remove("is-active");
      current = links[first.id];
      current.classList.add("is-active");
    }, { rootMargin: "-60px 0px -65% 0px" });
    heads.forEach(function (h) { tocObserver.observe(h); });
  }

  // ---- What's new: By release / By area --------------------------------------

  var WN_KEY = "en-docs-whats-new";

  function initWhatsNew() {
    var sw = document.querySelector(".wn-switch");
    if (!sw) return;
    var buttons = sw.querySelectorAll("button[data-view]");
    var views = document.querySelectorAll(".wn-view");

    function show(name) {
      Array.prototype.forEach.call(buttons, function (b) {
        b.setAttribute("aria-pressed", String(b.dataset.view === name));
      });
      Array.prototype.forEach.call(views, function (v) {
        v.hidden = v.dataset.view !== name;
      });
      rebuildToc();
    }
    function choose(name) {
      show(name);
      try { localStorage.setItem(WN_KEY, name); } catch (e) { /* private mode */ }
      if (name === "area") history.replaceState(null, "", "#by-area");
      else if (location.hash === "#by-area") history.replaceState(null, "", location.pathname + location.search);
    }
    // An anchor inside a hidden view opens that view.
    function fromHash() {
      var el = document.getElementById(decodeURIComponent(location.hash.slice(1)));
      var view = el && el.closest(".wn-view");
      if (!view) return false;
      show(view.dataset.view);
      el.scrollIntoView();
      return true;
    }

    Array.prototype.forEach.call(buttons, function (b) {
      b.addEventListener("click", function () { choose(b.dataset.view); });
    });
    window.addEventListener("hashchange", fromHash);
    if (!fromHash()) {
      var stored = null;
      try { stored = localStorage.getItem(WN_KEY); } catch (e) { /* private mode */ }
      show(stored === "area" ? "area" : "release");
    }
  }

  // ---- A link to a collapsed block opens it ----------------------------------

  // The release notes link to one release on the version overview.
  function initDetailsAnchor() {
    function open() {
      var el = document.getElementById(decodeURIComponent(location.hash.slice(1)));
      if (!el || el.tagName !== "DETAILS") return;
      el.open = true;
      el.scrollIntoView();
    }
    window.addEventListener("hashchange", open);
    open();
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

  // ---- Images open enlarged in a dialog --------------------------------------

  // Content images and the cropped picture tiles of What's new (a div with the
  // image as background) open the whole image. Images inside a link, SVGs and
  // the page icon stay as they are.
  function initLightbox() {
    var body = document.querySelector(".doc-body");
    if (!body || typeof HTMLDialogElement === "undefined") return;
    var items = Array.prototype.filter.call(body.querySelectorAll("img, div.wn-img"), function (el) {
      if (el.closest("a") || el.classList.contains("page-icon")) return false;
      return !/\.svg(\?|$)/.test(sourceOf(el));
    });
    if (!items.length) return;

    var dialog = document.createElement("dialog");
    dialog.className = "lightbox";
    dialog.setAttribute("aria-label", "Enlarged image");
    dialog.innerHTML = '<button type="button" class="lightbox__close" aria-label="Close">&times;</button><img class="lightbox__img" alt="">';
    document.body.appendChild(dialog);
    var big = dialog.querySelector(".lightbox__img");
    dialog.addEventListener("click", function () { dialog.close(); });
    dialog.addEventListener("close", function () { big.removeAttribute("src"); });

    items.forEach(function (el) {
      el.classList.add("zoomable");
      el.setAttribute("tabindex", "0");
      el.setAttribute("role", "button");
      var label = el.getAttribute("alt") || el.getAttribute("aria-label") || "";
      el.setAttribute("aria-label", "Enlarge image" + (label ? ": " + label : ""));
      function open(e) {
        e.preventDefault();
        e.stopPropagation();
        big.src = sourceOf(el);
        big.alt = label;
        dialog.showModal();
      }
      el.addEventListener("click", open);
      el.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") open(e);
      });
    });
  }

  function sourceOf(el) {
    if (el.tagName === "IMG") return el.currentSrc || el.src;
    var m = /url\(["']?([^"')]+)["']?\)/.exec(el.style.backgroundImage || "");
    return m ? m[1] : "";
  }

  // ---- Gallery: pages with `gallery: true` ----------------------------------

  // Each `## Title` with a `### Category` field is one item. The item's nodes
  // move into a dialog when its tile opens, so the lightbox handlers on its
  // images stay attached. Runs after initLightbox: the tile thumbnails are new
  // images and must not become zoomable.
  function initGallery() {
    var body = document.querySelector("article.doc-body[data-gallery]");
    if (!body) return;

    var items = [];
    Array.prototype.slice.call(body.children).forEach(function (h2) {
      if (h2.tagName !== "H2" || !h2.id) return;
      var nodes = [h2];
      for (var n = h2.nextSibling; n && n.nodeName !== "H2"; n = n.nextSibling) nodes.push(n);
      var isItem = nodes.some(function (x) { return x.nodeName === "H3" && x.textContent.trim() === "Category"; });
      if (!isItem) return;
      var section = document.createElement("section");
      section.className = "gallery-item";
      section.setAttribute("data-item", h2.id);
      section.hidden = true;
      body.insertBefore(section, h2);
      nodes.forEach(function (x) { section.appendChild(x); });
      items.push(itemOf(section, h2));
    });
    if (!items.length) { rebuildToc(); return; }

    var current = null;   // item shown in the sheet
    var opener = null;    // tile that opened it

    var gallery = document.createElement("div");
    gallery.className = "gallery";

    var categories = [];
    items.forEach(function (it) { if (categories.indexOf(it.category) < 0) categories.push(it.category); });
    var filter = document.createElement("div");
    filter.className = "gallery-filter";
    filter.setAttribute("role", "group");
    filter.setAttribute("aria-label", "Filter");
    function applyFilter(label) {
      Array.prototype.forEach.call(filter.querySelectorAll("button"), function (b) {
        b.setAttribute("aria-pressed", String(b.textContent === label));
      });
      items.forEach(function (it) { it.tile.hidden = label !== "All" && it.category !== label; });
    }
    if (categories.length >= 2) {
      ["All"].concat(categories).forEach(function (label) {
        var b = document.createElement("button");
        b.type = "button";
        b.textContent = label;
        b.setAttribute("aria-pressed", String(label === "All"));
        b.addEventListener("click", function () { applyFilter(label); });
        filter.appendChild(b);
      });
      gallery.appendChild(filter);
    }

    var grid = document.createElement("div");
    grid.className = "gallery-grid";
    items.forEach(function (it) {
      var tile = document.createElement("button");
      tile.type = "button";
      tile.className = "gallery-tile";
      tile.setAttribute("aria-haspopup", "dialog");
      tile.setAttribute("data-item", it.id);
      if (it.img) {
        var pic = document.createElement("img");
        pic.src = it.img.getAttribute("src");
        pic.alt = it.img.getAttribute("alt") || "";
        pic.loading = "lazy";
        tile.appendChild(pic);
      } else {
        var ph = document.createElement("span");
        ph.className = "gallery-tile__ph";
        ph.textContent = it.category;
        tile.appendChild(ph);
      }
      var title = document.createElement("b");
      title.textContent = it.title;
      var meta = document.createElement("span");
      meta.className = "gallery-tile__meta";
      meta.textContent = it.creates ? it.category + " \u00b7 " + it.creates : it.category;
      tile.appendChild(title);
      tile.appendChild(meta);
      tile.addEventListener("click", function () { opener = tile; show(it); });
      it.tile = tile;
      grid.appendChild(tile);
    });
    gallery.appendChild(grid);
    body.insertBefore(gallery, items[0].section);

    var sheet = document.createElement("dialog");
    sheet.className = "gallery-sheet doc-body";
    sheet.innerHTML =
      '<div class="gallery-sheet__head"><button type="button" class="gallery-sheet__close" aria-label="Close">\u2715</button></div>' +
      '<div class="gallery-sheet__content"></div>' +
      '<div class="gallery-sheet__foot"><button type="button" class="gallery-sheet__prev"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15 5l-7 7 7 7"/></svg></button><button type="button" class="gallery-sheet__next"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15 5l-7 7 7 7"/></svg></button></div>';
    document.body.appendChild(sheet);
    var content = sheet.querySelector(".gallery-sheet__content");
    var prevBtn = sheet.querySelector(".gallery-sheet__prev");
    var nextBtn = sheet.querySelector(".gallery-sheet__next");

    // The neighbour in the visible tiles, cyclic. Works from the page order.
    function neighbour(dir) {
      var pos = items.indexOf(current);
      var visible = items.filter(function (it) { return !it.tile.hidden; });
      if (dir > 0) {
        return visible.find(function (it) { return items.indexOf(it) > pos; }) || visible[0];
      }
      return visible.slice().reverse().find(function (it) { return items.indexOf(it) < pos; }) || visible[visible.length - 1];
    }
    // Moves the shown item's nodes back into its section.
    function putBack() {
      if (!current) return;
      while (content.firstChild) current.section.appendChild(content.firstChild);
      current = null;
    }
    function show(it) {
      putBack();
      current = it;
      while (it.section.firstChild) content.appendChild(it.section.firstChild);
      content.scrollTop = 0;
      sheet.setAttribute("aria-labelledby", it.id);
      // Icon-only buttons: the neighbour's title is the tooltip and the accessible name.
      [[prevBtn, "Previous: " + neighbour(-1).title], [nextBtn, "Next: " + neighbour(1).title]].forEach(function (b) {
        b[0].title = b[1];
        b[0].setAttribute("aria-label", b[1]);
      });
      if (!sheet.open) sheet.showModal();
      history.replaceState(null, "", "#" + it.id);
    }

    sheet.addEventListener("close", function () {
      putBack();
      history.replaceState(null, "", location.pathname + location.search);
      if (opener) opener.focus();
      opener = null;
    });
    sheet.querySelector(".gallery-sheet__close").addEventListener("click", function () { sheet.close(); });
    prevBtn.addEventListener("click", function () { show(neighbour(-1)); });
    nextBtn.addEventListener("click", function () { show(neighbour(1)); });
    // A click on the backdrop is a click on the dialog outside its box.
    sheet.addEventListener("click", function (e) {
      if (e.target !== sheet) return;
      var r = sheet.getBoundingClientRect();
      if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) sheet.close();
    });
    document.addEventListener("keydown", function (e) {
      if (!sheet.open || document.querySelector("dialog.lightbox[open]")) return;
      if (e.altKey || e.ctrlKey || e.metaKey) return;
      var t = e.target;
      if (/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || t.isContentEditable) return;
      if (e.key === "ArrowLeft") show(neighbour(-1));
      else if (e.key === "ArrowRight") show(neighbour(1));
    });

    // A link to an item, or to an element inside it, opens that item.
    function fromHash() {
      var el = document.getElementById(decodeURIComponent(location.hash.slice(1)));
      if (!el) return;
      var sec = el.closest(".gallery-item");
      var owner = null;
      if (sec) owner = items.find(function (it) { return it.section === sec; });
      else if (current && sheet.contains(el)) owner = current;
      if (!owner || (owner === current && sheet.open)) return;
      show(owner);
    }
    window.addEventListener("hashchange", fromHash);
    fromHash();
    rebuildToc();
  }

  function itemOf(section, h2) {
    var fields = Array.prototype.filter.call(section.children, function (el) { return el.tagName === "H3"; });
    fields.forEach(function (h) { h.classList.add("gallery-field"); });
    var firstField = fields[0];
    var img = section.querySelector("img");
    // The picture is the first image before the first field.
    if (img && firstField && !(img.compareDocumentPosition(firstField) & Node.DOCUMENT_POSITION_FOLLOWING)) img = null;
    function field(name) {
      var h = fields.find(function (f) { return f.textContent.trim() === name; });
      if (!h) return "";
      var parts = [];
      for (var el = h.nextElementSibling; el && el.tagName !== "H3"; el = el.nextElementSibling) parts.push(el.textContent.trim());
      return parts.join(" ").trim();
    }
    return {
      section: section,
      id: h2.id,
      title: h2.textContent.trim(),
      img: img,
      category: field("Category"),
      creates: field("Creates")
    };
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
    if (cur) {
      var sr = sidebar.getBoundingClientRect(), cr = cur.getBoundingClientRect();
      sidebar.scrollTop += cr.top - sr.top - sidebar.clientHeight / 2 + cr.height / 2;
    }
  }

  // ---- Search (Pagefind) -----------------------------------------------------

  function initSearch() {
    var box = document.getElementById("doc-search");
    var trigger = document.getElementById("search-trigger");
    if (!box || !trigger || !window.fetch) return;
    fetch(base + "pagefind/pagefind-entry.json", { method: "HEAD" }).then(function (r) {
      if (r.ok) box.hidden = false;
    }).catch(function () {});

    var loaded = false;
    function load() {
      if (loaded) return;
      loaded = true;
      var css = document.createElement("link");
      css.rel = "stylesheet";
      css.href = base + "pagefind/pagefind-ui.css";
      document.head.appendChild(css);
      var s = document.createElement("script");
      s.src = base + "pagefind/pagefind-ui.js";
      s.onload = function () {
        trigger.remove();
        /* global PagefindUI */
        new PagefindUI({ element: "#doc-search", showSubResults: true, showImages: false, resetStyles: false });
        var input = box.querySelector("input");
        if (input) input.focus();
      };
      document.head.appendChild(s);
    }
    trigger.addEventListener("click", load);
    document.addEventListener("keydown", function (e) {
      if (e.key === "/" && !/^(INPUT|TEXTAREA)$/.test(document.activeElement.tagName)) {
        e.preventDefault();
        load();
      }
    });
  }

  // ---- Start -----------------------------------------------------------------

  window.EnergyDocs = { setTheme: setTheme };
  initAnchorMoves();
  initTheme();
  wrapTables();
  initToc();
  initWhatsNew();
  initDetailsAnchor();
  initLightbox();
  initGallery();
  initNav();
  initSearch();
})();
