/* gridwatch.js — minimal client-side glue.
   - Theme toggle (localStorage)
   - Timezone cookie (sent to server on next request)
   - Server-sent events for live refresh
   - "now" cursor advance every 30s (pure CSS variable nudge)
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
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (tz) {
      document.cookie = 'gw_tz=' + encodeURIComponent(tz) + '; path=/; max-age=31536000; SameSite=Lax';
    }
  } catch (e) { /* ignore */ }

  // ---- SSE live refresh ----
  // Strategy: on revision event, trigger an htmx request to refetch the
  // grid partial. Falls back to periodic polling if EventSource is missing.
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

  // Wait until htmx is loaded before starting SSE so refreshGrid works.
  document.addEventListener('DOMContentLoaded', function () {
    // Defer SSE connection one tick so the initial render finishes first.
    setTimeout(startSSE, 100);
  });

  // ---- "Now" cursor advance ----
  // The server renders --now-col based on current time. Every 30 seconds,
  // nudge it one slot to the right if the slot duration is ≤30s — visually
  // this just keeps the cursor from looking frozen between SSE updates.
  // A real update lands when SSE pushes a fresh render.
  // (No-op here; the server-side SSE is the source of truth.)
})();
