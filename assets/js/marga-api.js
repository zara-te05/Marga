/**
 * Marga API Client
 * - Cliente fetch para el backend Go (/api/...), con sesión por cookie HttpOnly
 * - Guardia de sesión: si no hay sesión válida redirige a login.html
 * - Pinta el avatar del usuario y su menú (cerrar sesión)
 * - Utilidades: escapeHTML, tiempo relativo, repositorio activo
 *
 * Si abres las páginas con doble clic (file://) no hay backend: MargaAPI.online = false
 * y las páginas siguen mostrando su contenido de demostración.
 */
(function () {
  const online = location.protocol === 'http:' || location.protocol === 'https:';
  const isLogin = /login\.html$/.test(location.pathname);

  class ApiError extends Error {
    constructor(status, message) { super(message); this.status = status; }
  }

  async function request(method, path, body) {
    const opts = {
      method,
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' }
    };
    if (body !== undefined) opts.body = JSON.stringify(body);

    let res;
    try {
      res = await fetch('/api' + path, opts);
    } catch (e) {
      throw new ApiError(0, 'No hay conexión con el servidor de Marga');
    }
    let data = null;
    try { data = await res.json(); } catch (_) { /* respuesta sin cuerpo */ }

    if (res.status === 401 && !isLogin && path !== '/auth/login') {
      goToLogin();
      throw new ApiError(401, (data && data.error) || 'Sesión expirada');
    }
    if (!res.ok) throw new ApiError(res.status, (data && data.error) || ('Error HTTP ' + res.status));
    return data;
  }

  function goToLogin() {
    const next = encodeURIComponent(location.pathname + location.search);
    location.replace('login.html?next=' + next);
  }

  // ------------------------------------------------------------ utilidades
  function escapeHTML(v) {
    return String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  function timeAgo(date) {
    const d = new Date(date);
    const s = Math.round((Date.now() - d.getTime()) / 1000);
    if (s < 60) return 'hace un momento';
    const m = Math.round(s / 60); if (m < 60) return `hace ${m}m`;
    const h = Math.round(m / 60); if (h < 24) return `hace ${h}h`;
    const dd = Math.round(h / 24); if (dd < 2) return 'ayer';
    if (dd < 30) return `hace ${dd} días`;
    return d.toLocaleDateString('es-MX', { day: '2-digit', month: 'short', year: 'numeric' });
  }

  function initials(name) {
    const p = String(name || '?').replace(/[^\p{L}\p{N}\s-]/gu, ' ').split(/[\s-]+/).filter(Boolean);
    return ((p[0] || '?')[0] + (p[1] ? p[1][0] : (p[0] || '')[1] || '')).toUpperCase();
  }

  const MOTORES = { postgresql: 'PostgreSQL', mysql: 'MySQL', sqlserver: 'SQL Server', mongodb: 'MongoDB', sqlite: 'SQLite' };

  // Repositorio activo: ?repo=ID en la URL o el último usado.
  function currentRepoId() {
    const fromUrl = new URLSearchParams(location.search).get('repo');
    if (fromUrl) return Number(fromUrl);
    try { return Number(localStorage.getItem('marga_repo')) || null; } catch (_) { return null; }
  }
  function setCurrentRepo(id) {
    try { localStorage.setItem('marga_repo', String(id)); } catch (_) { }
  }

  // ------------------------------------------------------------ usuario en cabecera
  function paintUser(u) {
    const el = document.getElementById('user-initials');
    if (!el) return;
    el.textContent = initials(u.nombre);
    const chip = el.parentElement;
    chip.title = `${u.nombre} · ${u.email}`;
    chip.style.position = 'relative';
    chip.addEventListener('click', (e) => {
      e.stopPropagation();
      let menu = document.getElementById('user-menu');
      if (menu) { menu.remove(); return; }
      menu = document.createElement('div');
      menu.id = 'user-menu';
      menu.className = 'absolute right-0 top-10 z-[80] w-56 rounded-xl bg-marga-surface border border-marga-border shadow-2xl p-1.5 text-left cursor-default';
      menu.innerHTML = `
        <div class="px-2.5 py-2 border-b border-marga-border-subtle mb-1">
          <div class="text-xs font-semibold text-white">${escapeHTML(u.nombre)}</div>
          <div class="text-[11px] font-mono text-neutral-400 truncate">${escapeHTML(u.email)}</div>
          <div class="text-[10px] font-mono text-brand-cyan mt-1">${escapeHTML(u.rol)}${u.equipo ? ' · ' + escapeHTML(u.equipo) : ''}</div>
        </div>
        <button type="button" id="logout-btn" class="flex items-center gap-2 w-full px-2.5 py-1.5 rounded text-xs text-neutral-400 hover:bg-neutral-500/20 transition-colors">
          <span class="material-symbols-outlined text-[15px]">logout</span><span>Cerrar sesión</span>
        </button>`;
      chip.appendChild(menu);
      menu.querySelector('#logout-btn').addEventListener('click', async (ev) => {
        ev.stopPropagation();
        try { await request('POST', '/auth/logout', {}); } catch (_) { }
        location.replace('login.html');
      });
    });
    document.addEventListener('click', () => document.getElementById('user-menu')?.remove());
  }

  const API = {
    online,
    ApiError,
    me: null,
    get: (p) => request('GET', p),
    post: (p, b) => request('POST', p, b ?? {}),
    patch: (p, b) => request('PATCH', p, b ?? {}),
    del: (p) => request('DELETE', p),
    escapeHTML, timeAgo, initials, MOTORES,
    currentRepoId, setCurrentRepo,
    ready: Promise.resolve(null)
  };

  if (online && !isLogin) {
    API.ready = request('GET', '/auth/me')
      .then(u => {
        API.me = u;
        if (document.readyState === 'loading') {
          document.addEventListener('DOMContentLoaded', () => paintUser(u));
        } else {
          paintUser(u);
        }
        return u;
      })
      .catch(err => {
        if (err.status !== 401 && typeof showToast === 'function') showToast(err.message, 'error');
        return null;
      });
  }

  window.MargaAPI = API;
})();
