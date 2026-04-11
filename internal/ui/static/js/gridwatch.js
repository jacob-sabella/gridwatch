/* gridwatch.js — minimal client-side glue.
   - Theme toggle (localStorage)
   - Timezone cookie (sent to server on next request)
   - Server-sent events for live refresh
   - Live ticking header clock (updates every second)
   - "Now" cursor advance on the EPG grid (updates every 30s)
*/

(function () {
  'use strict';

  // ---- Theme ----
  const THEME_KEY = 'gw-theme';
  const themeToggle = document.getElementById('theme-toggle');
  const root = document.documentElement;

  function applyTheme(t) {
    root.setAttribute('data-theme', t);
    try { localStorage.setItem(THEME_KEY, t); } catch (e) { /* ignore */ }
  }
  const saved = (function () { try { return localStorage.getItem(THEME_KEY); } catch (e) { return null; } })();
  if (saved) applyTheme(saved);

  if (themeToggle) {
    themeToggle.addEventListener('click', function () {
      const current = root.getAttribute('data-theme') || 'auto';
      const next = current === 'dark' ? 'light' : current === 'light' ? 'auto' : 'dark';
      applyTheme(next);
    });
  }

  // ---- Timezone cookie ----
  // The server reads gw_tz on every render and uses it to format times in
  // the user's local time. First load may see the default tz; subsequent
  // loads use the detected one.
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (tz) {
      document.cookie = 'gw_tz=' + encodeURIComponent(tz) + '; path=/; max-age=31536000; SameSite=Lax';
    }
  } catch (e) { /* ignore */ }

  // ---- Live header clock ----
  // The server renders the header clock once per response. We overwrite
  // it every second so it stays fresh without SSE churn.
  function formatNow() {
    const now = new Date();
    const date = now.toLocaleDateString(undefined, {
      weekday: 'short', month: 'short', day: 'numeric'
    });
    const time = now.toLocaleTimeString(undefined, {
      hour: 'numeric', minute: '2-digit', hour12: true
    });
    let tz = '';
    try {
      tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    } catch (e) { /* ignore */ }
    return tz ? `${date} · ${time} ${tz}` : `${date} · ${time}`;
  }

  function updateClock() {
    // The header clock spans live in .view-head .muted. There may be
    // multiple (one per re-rendered partial); update all of them.
    const nodes = document.querySelectorAll('.view-head .now-clock');
    nodes.forEach(function (el) { el.textContent = formatNow(); });
  }

  // ---- "Now" cursor advance on the EPG ----
  // The server emits data-window-start-ms + data-slot-ms on the .epg
  // element so we can recompute which column the cursor belongs to
  // without a round-trip.
  function updateNowCursor() {
    const epg = document.querySelector('.epg');
    if (!epg) return;
    const start = parseInt(epg.dataset.windowStartMs || '0', 10);
    const slot = parseInt(epg.dataset.slotMs || '0', 10);
    if (!start || !slot) return;
    const now = Date.now();
    const col = Math.max(0, Math.floor((now - start) / slot));
    epg.style.setProperty('--now-col', String(col));
  }

  // Kick both loops on load and every second / 30s.
  document.addEventListener('DOMContentLoaded', function () {
    updateClock();
    updateNowCursor();
    setInterval(updateClock, 1000);
    setInterval(updateNowCursor, 30 * 1000);
    setTimeout(startSSE, 100);
  });

  // Re-wire when htmx swaps partials in — otherwise new .view-head nodes
  // have a stale timestamp until the next tick.
  document.addEventListener('htmx:afterSwap', function () {
    updateClock();
    updateNowCursor();
  });

  // ---- SSE live refresh ----
  function startSSE() {
    if (typeof EventSource === 'undefined') {
      console.log('[gridwatch] SSE unavailable, falling back to polling');
      setInterval(refreshGrid, 60000);
      return;
    }
    const es = new EventSource('events');
    let lastRev = -1;
    es.addEventListener('revision', function (ev) {
      const rev = parseInt(ev.data, 10);
      if (rev !== lastRev) {
        lastRev = rev;
        refreshGrid();
      }
    });
    es.addEventListener('error', function () {
      // Let the browser auto-reconnect.
    });
  }

  function refreshGrid() {
    if (window.htmx) {
      const view = document.getElementById('view');
      if (view) window.htmx.ajax('GET', 'partial/grid', { target: '#view' });
    }
  }
})();
