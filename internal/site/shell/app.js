/* benchbook documentation shell - tool-owned, overwritten on every `benchbook build`.
 *
 * Everything project-specific comes from manifest.json: there is no documentation
 * content, no navigation and no project name in this file. The shell is deliberately
 * small and dependency-free: hash routing, a sidebar built from the manifest, a
 * per-page table of contents with scroll spy, three themes, a filter box, and the
 * compare page, which is the only place benchbook logic appears in the browser.
 */
(function () {
  "use strict";

  var MANIFEST_PATH = "manifest.json";
  var DEFAULT_PAGE = "content/benchmarks.md";
  var LOADING_TEXT = "Loading documentation…";
  var NOT_FOUND_TEXT = "Looks like this page wandered off…";
  var MANIFEST_HINT =
    "The generated site is incomplete: manifest.json was not found. Run `benchbook build` in your project and reload.";

  var THEME_ORDER = ["light", "dark", "neon"];
  var DARK_THEMES = { dark: true, neon: true };
  var THEME_LIGHT_CSS = "assets/benchbook/vendor/styles/github.min.css";
  var THEME_DARK_CSS = "assets/benchbook/vendor/styles/github-dark.min.css";
  var VENDOR_LANGUAGE_PATH = "assets/benchbook/vendor/languages/";
  var CODE_LANGUAGE_CLASS = "language-";
  var SCROLL_SPY_OFFSET = 96;
  var MIN_HEADING_LEVEL = 2;
  var MAX_HEADING_LEVEL = 3;
  var COMPARE_MOUNT_ID = "bb-compare";
  var PERMALINK_METRIC_KEY = "metric";
  var PERMALINK_FROM_KEY = "from";
  var PERMALINK_TO_KEY = "to";

  var state = {
    manifest: null,
    page: "",
    headings: [],
    compareCache: {}
  };

  /* ------------------------------------------------------------------ helpers */

  function byId(id) {
    return document.getElementById(id);
  }

  function element(tag, className, text) {
    var node = document.createElement(tag);
    if (className) {
      node.className = className;
    }
    if (typeof text === "string") {
      node.textContent = text;
    }
    return node;
  }

  function slugify(text) {
    return String(text)
      .toLowerCase()
      .replace(/[^\w\s-]/g, "")
      .trim()
      .replace(/\s+/g, "-");
  }

  function humanizeNs(nanoseconds) {
    var absolute = Math.abs(nanoseconds);
    if (absolute >= 1e9) {
      return trimNumber(nanoseconds / 1e9) + " s";
    }
    if (absolute >= 1e6) {
      return trimNumber(nanoseconds / 1e6) + " ms";
    }
    if (absolute >= 1e3) {
      return trimNumber(nanoseconds / 1e3) + " µs";
    }
    return trimNumber(nanoseconds) + " ns";
  }

  function trimNumber(value) {
    var rounded = Math.round(value * 100) / 100;
    return String(rounded);
  }

  function formatMetric(metric, value) {
    if (metric === "ns_op") {
      return humanizeNs(value);
    }
    if (metric === "bytes_op") {
      return trimNumber(value) + " B";
    }
    if (metric === "allocs_op") {
      return trimNumber(value) + " allocs";
    }
    return trimNumber(value) + " " + metric;
  }

  function formatSignedPercent(delta) {
    if (!delta) {
      return "0.0%";
    }
    var sign = delta < 0 ? "−" : "+";
    return sign + Math.abs(delta).toFixed(1) + "%";
  }

  function formatPValue(p) {
    if (!p) {
      return "0";
    }
    if (p < 0.001) {
      return p.toExponential(1);
    }
    return p.toFixed(3);
  }

  /* --------------------------------------------------------------- manifest */

  function loadManifest() {
    return fetch(MANIFEST_PATH, { cache: "no-cache" }).then(function (response) {
      if (!response.ok) {
        throw new Error(MANIFEST_HINT);
      }
      return response.json();
    });
  }

  function applySiteSettings(manifest) {
    var site = manifest.site || {};
    document.title = site.title || manifest.project.name || "Documentation";
    byId("bb-brand-title").textContent = manifest.project.name || site.title || "Documentation";
    byId("bb-brand-emoji").textContent = manifest.project.emoji || "";
    var repo = byId("bb-repo-link");
    if (manifest.project.repo_url) {
      repo.href = manifest.project.repo_url;
      repo.hidden = false;
    }
    if (site.extra_css) {
      var extra = byId("bb-extra-css");
      extra.href = site.extra_css;
      extra.disabled = false;
    }
    if (site.loading_text) {
      LOADING_TEXT = site.loading_text;
      byId("bb-loading").textContent = site.loading_text;
    }
    if (site.not_found_text) {
      NOT_FOUND_TEXT = site.not_found_text;
    }
    if (manifest.favicon) {
      var link = document.createElement("link");
      link.rel = "icon";
      link.href = manifest.favicon;
      document.head.appendChild(link);
    }
  }

  function loadExtraLanguages(site) {
    var languages = (site && site.highlight_langs) || [];
    languages.forEach(function (language) {
      var script = document.createElement("script");
      script.src = VENDOR_LANGUAGE_PATH + language + ".min.js";
      script.async = false;
      /* A missing grammar is not an error: the core bundle already covers the
         common languages, and an unknown one simply falls back to plain text. */
      script.onerror = function () {
        if (window.console && console.debug) {
          console.debug("benchbook: no vendored grammar for", language);
        }
      };
      document.head.appendChild(script);
    });
  }

  /* ---------------------------------------------------------------- sidebar */

  function renderNav() {
    var nav = byId("bb-nav");
    nav.textContent = "";
    (state.manifest.nav || []).forEach(function (section) {
      var isCollapsible = section.collapsible === true;
      var container = isCollapsible ? document.createElement("details") : element("div", "bb-nav-section");
      if (isCollapsible) {
        container.className = "bb-nav-section";
        container.open = section.expanded !== false;
        var summary = document.createElement("summary");
        summary.textContent = section.section;
        container.appendChild(summary);
      } else {
        var heading = element("div", "bb-nav-heading", section.section);
        container.appendChild(heading);
      }
      var list = element("ul", "bb-nav-list");
      (section.pages || []).forEach(function (page) {
        var item = document.createElement("li");
        var link = document.createElement("a");
        link.href = "#/" + page.path;
        link.textContent = page.title;
        link.dataset.path = page.path;
        link.dataset.filter = (page.title + " " + page.path).toLowerCase();
        item.appendChild(link);
        list.appendChild(item);
      });
      container.appendChild(list);
      nav.appendChild(container);
    });
    highlightActive();
  }

  function highlightActive() {
    var links = document.querySelectorAll("#bb-nav a");
    Array.prototype.forEach.call(links, function (link) {
      if (link.dataset.path === state.page) {
        link.setAttribute("aria-current", "page");
      } else {
        link.removeAttribute("aria-current");
      }
    });
  }

  function applyFilter(query) {
    var needle = query.trim().toLowerCase();
    var links = document.querySelectorAll("#bb-nav a");
    Array.prototype.forEach.call(links, function (link) {
      var match = needle === "" || link.dataset.filter.indexOf(needle) !== -1;
      link.parentNode.hidden = !match;
    });
    var sections = document.querySelectorAll("#bb-nav .bb-nav-section");
    Array.prototype.forEach.call(sections, function (section) {
      var visible = section.querySelectorAll("li:not([hidden])").length > 0;
      section.hidden = !visible;
    });
  }

  /* --------------------------------------------------------------- routing */

  function parseHash() {
    var raw = window.location.hash.replace(/^#\/?/, "");
    var parts = raw.split("#");
    return { path: parts[0] || "", anchor: parts[1] || "" };
  }

  function navigate() {
    var route = parseHash();
    var path = route.path || DEFAULT_PAGE;
    if (state.manifest && state.manifest.default_path && !route.path) {
      path = state.manifest.default_path;
    }
    if (path === state.page && route.anchor) {
      scrollToAnchor(route.anchor);
      return;
    }
    state.page = path;
    loadPage(path, route.anchor);
  }

  function loadPage(path, anchor) {
    var loading = byId("bb-loading");
    var article = byId("bb-article");
    var error = byId("bb-error");
    loading.hidden = false;
    loading.textContent = LOADING_TEXT;
    article.hidden = true;
    error.hidden = true;

    fetch(path, { cache: "no-cache" })
      .then(function (response) {
        if (!response.ok) {
          throw new Error("not-found");
        }
        return response.text();
      })
      .then(function (markdown) {
        renderMarkdown(markdown);
        loading.hidden = true;
        article.hidden = false;
        highlightActive();
        if (anchor) {
          scrollToAnchor(anchor);
        } else {
          window.scrollTo({ top: 0 });
        }
      })
      .catch(function (cause) {
        loading.hidden = true;
        error.hidden = false;
        error.textContent = cause && cause.message === "not-found" ? NOT_FOUND_TEXT : String(cause && cause.message);
      });
  }

  function renderMarkdown(markdown) {
    var article = byId("bb-article");
    var html = window.marked ? window.marked.parse(markdown) : markdown;
    article.innerHTML = html;
    addHeadingAnchors();
    wrapTables();
    addCopyButtons();
    highlightCode();
    buildToc();
    mountCompare();
  }

  function addHeadingAnchors() {
    var headings = byId("bb-article").querySelectorAll("h2, h3");
    Array.prototype.forEach.call(headings, function (heading) {
      if (!heading.id) {
        heading.id = slugify(heading.textContent);
      }
      var anchor = document.createElement("a");
      anchor.className = "bb-header-anchor";
      anchor.href = "#/" + state.page + "#" + heading.id;
      anchor.setAttribute("aria-label", "Link to this section");
      anchor.textContent = "#";
      anchor.style.marginLeft = "0.4rem";
      anchor.style.opacity = "0.45";
      heading.appendChild(anchor);
    });
  }

  function wrapTables() {
    var tables = byId("bb-article").querySelectorAll("table");
    Array.prototype.forEach.call(tables, function (table) {
      if (table.parentNode && table.parentNode.classList.contains("bb-table-scroll")) {
        return;
      }
      var wrapper = element("div", "bb-table-scroll");
      table.parentNode.insertBefore(wrapper, table);
      wrapper.appendChild(table);
    });
  }

  function addCopyButtons() {
    var blocks = byId("bb-article").querySelectorAll("pre");
    Array.prototype.forEach.call(blocks, function (block) {
      var wrapper = element("div", "bb-pre-wrap");
      block.parentNode.insertBefore(wrapper, block);
      wrapper.appendChild(block);
      var button = element("button", "bb-copy", "copy");
      button.type = "button";
      button.addEventListener("click", function () {
        var text = block.innerText;
        var done = function () {
          button.textContent = "copied";
          window.setTimeout(function () {
            button.textContent = "copy";
          }, 1500);
        };
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(done, function () {
            button.textContent = "copy failed";
          });
        } else {
          done();
        }
      });
      wrapper.appendChild(button);
    });
  }

  function highlightCode() {
    if (!window.hljs) {
      return;
    }
    var blocks = byId("bb-article").querySelectorAll("pre code");
    Array.prototype.forEach.call(blocks, function (block) {
      var className = block.className || "";
      var marker = className.indexOf(CODE_LANGUAGE_CLASS);
      if (marker === -1) {
        block.classList.add("nohighlight");
        return;
      }
      var language = className.slice(marker + CODE_LANGUAGE_CLASS.length).split(/\s+/)[0];
      if (!window.hljs.getLanguage(language)) {
        block.classList.add("nohighlight");
        return;
      }
      try {
        window.hljs.highlightElement(block);
      } catch (cause) {
        block.classList.add("nohighlight");
      }
    });
  }

  /* ------------------------------------------------------------------- TOC */

  function buildToc() {
    var toc = byId("bb-toc");
    toc.textContent = "";
    state.headings = [];
    var headings = byId("bb-article").querySelectorAll("h2, h3");
    if (headings.length === 0) {
      return;
    }
    var list = document.createElement("ul");
    Array.prototype.forEach.call(headings, function (heading) {
      var level = Number(heading.tagName.slice(1));
      if (level < MIN_HEADING_LEVEL || level > MAX_HEADING_LEVEL) {
        return;
      }
      state.headings.push(heading);
      var item = document.createElement("li");
      var link = document.createElement("a");
      link.href = "#/" + state.page + "#" + heading.id;
      link.textContent = heading.textContent.replace(/#$/, "");
      link.className = level === MAX_HEADING_LEVEL ? "bb-toc-h3" : "bb-toc-h2";
      link.dataset.target = heading.id;
      item.appendChild(link);
      list.appendChild(item);
    });
    toc.appendChild(list);
  }

  function scrollToAnchor(anchor) {
    var target = byId(anchor) || document.getElementById(anchor);
    if (!target) {
      return;
    }
    var offset = target.getBoundingClientRect().top + window.pageYOffset - SCROLL_SPY_OFFSET;
    window.scrollTo({ top: offset });
    updateActiveHeading();
  }

  function updateActiveHeading() {
    if (state.headings.length === 0) {
      return;
    }
    var active = state.headings[0];
    state.headings.forEach(function (heading) {
      if (heading.getBoundingClientRect().top - SCROLL_SPY_OFFSET <= 0) {
        active = heading;
      }
    });
    var links = document.querySelectorAll("#bb-toc a");
    Array.prototype.forEach.call(links, function (link) {
      if (link.dataset.target === active.id) {
        link.classList.add("bb-toc-active");
      } else {
        link.classList.remove("bb-toc-active");
      }
    });
  }

  /* ---------------------------------------------------------------- themes */

  function themeKey() {
    return (state.manifest && state.manifest.site && state.manifest.site.theme_storage_key) || "benchbook-theme";
  }

  function storedTheme() {
    try {
      return window.localStorage.getItem(themeKey());
    } catch (cause) {
      return null;
    }
  }

  function storeTheme(theme) {
    try {
      window.localStorage.setItem(themeKey(), theme);
    } catch (cause) {
      /* Private browsing: the theme simply does not persist. */
    }
  }

  function preferredTheme() {
    var configured = state.manifest && state.manifest.site && state.manifest.site.default_theme;
    var stored = storedTheme();
    if (stored) {
      return stored;
    }
    if (configured) {
      return configured;
    }
    if (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) {
      return "dark";
    }
    return "light";
  }

  function applyTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    var dark = DARK_THEMES[theme] === true;
    byId("bb-theme-light").disabled = dark;
    byId("bb-theme-dark").disabled = !dark;
    var toggle = byId("bb-theme-toggle");
    if (toggle) {
      toggle.setAttribute("aria-label", "Colour theme: " + theme + " (click to change)");
    }
  }

  function cycleTheme() {
    var current = document.documentElement.getAttribute("data-theme") || "light";
    var next = THEME_ORDER[(THEME_ORDER.indexOf(current) + 1) % THEME_ORDER.length];
    applyTheme(next);
    storeTheme(next);
  }

  /* --------------------------------------------------------------- compare */

  function loadRun(file) {
    if (state.compareCache[file]) {
      return Promise.resolve(state.compareCache[file]);
    }
    return fetch(file, { cache: "no-cache" })
      .then(function (response) {
        if (!response.ok) {
          throw new Error("cannot load " + file);
        }
        return response.json();
      })
      .then(function (run) {
        state.compareCache[file] = run;
        return run;
      });
  }

  function mountCompare() {
    var mount = document.getElementById(COMPARE_MOUNT_ID);
    if (!mount) {
      return;
    }
    mount.textContent = LOADING_TEXT;
    fetch("data/index.json", { cache: "no-cache" })
      .then(function (response) {
        if (!response.ok) {
          throw new Error("run `benchbook build` to export the comparison data");
        }
        return response.json();
      })
      .then(function (index) {
        renderCompare(index, mount);
      })
      .catch(function (cause) {
        mount.textContent = String(cause && cause.message);
      });
  }

  function renderCompare(index, mount) {
    var runs = index.runs || [];
    if (runs.length < 1) {
      mount.textContent = "No runs recorded yet.";
      return;
    }
    var options = index.options || {};
    var metrics = Object.keys(index.metrics || {});
    if (metrics.length === 0) {
      metrics = ["ns_op", "bytes_op", "allocs_op"];
    }

    var permalink = readPermalink();
    var controls = element("div", "bb-compare-controls");
    var fromSelect = runSelect("From (older)", runs, permalink.from || defaultFrom(runs));
    var toSelect = runSelect("To (newer)", runs, permalink.to || defaultTo(runs));
    var metricSelect = metricRunSelect(metrics, permalink.metric || metrics[0]);
    controls.appendChild(fromSelect.wrapper);
    controls.appendChild(toSelect.wrapper);
    controls.appendChild(metricSelect.wrapper);

    var output = element("div", "bb-compare-output");
    mount.textContent = "";
    mount.appendChild(controls);
    mount.appendChild(output);

    function refresh() {
      var from = runs.filter(function (run) {
        return run.id === fromSelect.select.value;
      })[0];
      var to = runs.filter(function (run) {
        return run.id === toSelect.select.value;
      })[0];
      writePermalink(from.id, to.id, metricSelect.select.value);
      output.textContent = "Comparing…";
      Promise.all([loadRun(from.file), loadRun(to.file)])
        .then(function (loaded) {
          renderCompareResult(output, loaded[0], loaded[1], metricSelect.select.value, options);
        })
        .catch(function (cause) {
          output.textContent = String(cause && cause.message);
        });
    }

    fromSelect.select.addEventListener("change", refresh);
    toSelect.select.addEventListener("change", refresh);
    metricSelect.select.addEventListener("change", refresh);
    refresh();
  }

  function defaultFrom(runs) {
    return runs.length > 1 ? runs[runs.length - 2].id : runs[0].id;
  }

  function defaultTo(runs) {
    return runs[runs.length - 1].id;
  }

  function runSelect(label, runs, selected) {
    var wrapper = document.createElement("label");
    wrapper.appendChild(document.createTextNode(label));
    var select = document.createElement("select");
    runs.forEach(function (run) {
      var option = document.createElement("option");
      option.value = run.id;
      option.textContent = run.title || run.id;
      if (run.id === selected) {
        option.selected = true;
      }
      select.appendChild(option);
    });
    wrapper.appendChild(select);
    return { wrapper: wrapper, select: select };
  }

  function metricRunSelect(metrics, selected) {
    var wrapper = document.createElement("label");
    wrapper.appendChild(document.createTextNode("Metric"));
    var select = document.createElement("select");
    metrics.forEach(function (metric) {
      var option = document.createElement("option");
      option.value = metric;
      option.textContent = metric;
      if (metric === selected) {
        option.selected = true;
      }
      select.appendChild(option);
    });
    wrapper.appendChild(select);
    return { wrapper: wrapper, select: select };
  }

  function readPermalink() {
    var raw = window.location.hash.indexOf("?") === -1 ? "" : window.location.hash.split("?")[1];
    var params = {};
    raw.split("&").forEach(function (pair) {
      var parts = pair.split("=");
      if (parts.length === 2) {
        params[decodeURIComponent(parts[0])] = decodeURIComponent(parts[1]);
      }
    });
    return {
      from: params[PERMALINK_FROM_KEY],
      to: params[PERMALINK_TO_KEY],
      metric: params[PERMALINK_METRIC_KEY]
    };
  }

  function writePermalink(from, to, metric) {
    var query =
      "?" +
      PERMALINK_FROM_KEY +
      "=" +
      encodeURIComponent(from) +
      "&" +
      PERMALINK_TO_KEY +
      "=" +
      encodeURIComponent(to) +
      "&" +
      PERMALINK_METRIC_KEY +
      "=" +
      encodeURIComponent(metric);
    var base = "#/" + state.page;
    if (window.location.hash.indexOf(query) === -1) {
      window.history.replaceState(null, "", base + query);
    }
  }

  // toCompareOptions translates the snake_case thresholds exported in
  // data/index.json into the camelCase shape compare.js expects.
  function toCompareOptions(raw) {
    var source = raw || {};
    var options = {};
    if (typeof source.alpha === "number") {
      options.alpha = source.alpha;
    }
    if (typeof source.min_effect_pct === "number") {
      options.minEffectPct = source.min_effect_pct;
    }
    if (typeof source.min_samples === "number") {
      options.minSamples = source.min_samples;
    }
    return options;
  }

  function renderCompareResult(output, from, to, metric, options) {
    var compareApi = window.BenchbookCompare;
    if (!compareApi) {
      output.textContent = "compare.js failed to load";
      return;
    }
    var comparison = compareApi.compareRuns(from, to, toCompareOptions(options));
    output.textContent = "";

    var headline = element("h3", null, "Headline");
    output.appendChild(headline);
    var summary = element("p");
    var parts = [];
    comparison.geomeans.forEach(function (geomean) {
      if (geomean.count > 0) {
        parts.push(geomean.metric + " " + formatSignedPercent(geomean.deltaPct) + " over " + geomean.count + " benchmarks");
      }
    });
    summary.textContent = parts.length > 0 ? parts.join("   ·   ") : "no comparable benchmarks";
    output.appendChild(summary);

    if (comparison.incomparable) {
      var banner = element("div", "bb-banner",
        comparison.forced
          ? "Forced view across different hardware: the numbers below are not directly comparable."
          : "These runs come from different hardware fingerprints, so no percentages are computed. Re-run on the same machine to compare.");
      output.appendChild(banner);
    }

    var counts = element("p");
    counts.textContent = Object.keys(comparison.counts)
      .sort()
      .map(function (classification) {
        return classification + " " + comparison.counts[classification];
      })
      .join("   ");
    output.appendChild(counts);

    var table = document.createElement("table");
    var head = document.createElement("tr");
    ["Benchmark", metric, "before", "after", "Δ", "p", "verdict"].forEach(function (title) {
      var cell = document.createElement("th");
      cell.textContent = title;
      head.appendChild(cell);
    });
    table.appendChild(head);

    comparison.results.forEach(function (result) {
      var metricResult = compareApi.metricResult(result, metric);
      if (!metricResult) {
        return;
      }
      var row = document.createElement("tr");
      appendCell(row, result.displayName, "th");
      appendCell(row, result.variant);
      appendCell(row, formatMetric(metric, metricResult.fromMedian));
      appendCell(row, formatMetric(metric, metricResult.toMedian));
      appendCell(row, metricResult.deltaDefined ? formatSignedPercent(metricResult.deltaPct) : "n/a");
      appendCell(row, metricResult.tested ? formatPValue(metricResult.p) : "—");
      var verdict = appendCell(row, metricResult.classification);
      verdict.className = "bb-verdict-" + metricResult.classification;
      table.appendChild(row);
    });
    var wrapper = element("div", "bb-table-scroll");
    wrapper.appendChild(table);
    output.appendChild(wrapper);

    var note = element("p");
    note.className = "bb-status";
    note.textContent =
      "Same procedure as the Go implementation: two-sided Mann-Whitney U with α = " +
      (options.alpha || compareApi.constants.defaultAlpha) +
      " and a minimum effect of " +
      (typeof options.min_effect_pct === "number" ? options.min_effect_pct : compareApi.constants.defaultMinEffectPct) +
      "%. Fewer than 4 samples per side is reported as insufficient.";
    output.appendChild(note);
  }

  function appendCell(row, text, tag) {
    var cell = document.createElement(tag || "td");
    cell.textContent = text == null ? "—" : text;
    row.appendChild(cell);
    return cell;
  }

  /* ------------------------------------------------------------------ boot */

  function bindGlobalHandlers() {
    window.addEventListener("hashchange", navigate);
    window.addEventListener("scroll", updateActiveHeading, { passive: true });
    byId("bb-theme-toggle").addEventListener("click", cycleTheme);
    byId("bb-filter").addEventListener("input", function (event) {
      applyFilter(event.target.value);
    });
    byId("bb-sidebar-toggle").addEventListener("click", function (event) {
      var sidebar = byId("bb-sidebar");
      var open = sidebar.classList.toggle("bb-open");
      event.currentTarget.setAttribute("aria-expanded", open ? "true" : "false");
    });
  }

  function boot() {
    bindGlobalHandlers();
    loadManifest()
      .then(function (manifest) {
        state.manifest = manifest;
        applySiteSettings(manifest);
        loadExtraLanguages(manifest.site);
        applyTheme(preferredTheme());
        renderNav();
        navigate();
      })
      .catch(function (cause) {
        byId("bb-loading").hidden = true;
        var error = byId("bb-error");
        error.hidden = false;
        error.textContent = String(cause && cause.message ? cause.message : cause);
      });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
