/**
 * Marga API Client
 * - Cliente fetch para el backend Go (/api/...), con sesión por cookie HttpOnly
 * - Guardia de sesión: si no hay sesión válida redirige a login.html
 * - Pinta el avatar del usuario y su menú (cerrar sesión)
 * - Utilidades: escapeHTML, tiempo relativo, repositorio activo
 *
 * Sin servidor (doble clic, file://) o con ?demo en la URL, las peticiones las responde
 * marga-demo.js con datos de ejemplo: todas las pantallas funcionan igual, solo lectura.
 */
(function () {
  const demo = window.MargaDemo || null;
  const online = !!demo || location.protocol === 'http:' || location.protocol === 'https:';
  // Páginas públicas: no exigen sesión.
  const isLogin = /(login|restablecer)\.html$/.test(location.pathname);

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
    if (demo) {
      try {
        return await demo.responder(method, path, body);
      } catch (e) {
        throw new ApiError(e.status ?? 0, e.message);
      }
    }

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

  // Objetos del esquema: ícono (Material Symbols), nombre en singular y plural, y orden.
  const TIPOS_OBJETO = {
    tabla:               { icono: 'table',       uno: 'tabla',              varios: 'tablas' },
    tipo:                { icono: 'label',       uno: 'tipo',               varios: 'tipos' },
    funcion:             { icono: 'function',    uno: 'función',            varios: 'funciones' },
    procedimiento:       { icono: 'terminal',    uno: 'procedimiento',      varios: 'procedimientos' },
    vista:               { icono: 'visibility',  uno: 'vista',              varios: 'vistas' },
    vista_materializada: { icono: 'layers',      uno: 'vista materializada', varios: 'vistas materializadas' },
    trigger:             { icono: 'bolt',        uno: 'trigger',            varios: 'triggers' },
    sql:                 { icono: 'description', uno: 'archivo',            varios: 'archivos' },
  };
  const tipoObjeto = (t) => TIPOS_OBJETO[t || 'tabla'] || TIPOS_OBJETO.tabla;

  // "3 tablas · 2 funciones · 1 vista" a partir de una lista de objetos con .tipo
  function contarObjetos(objs) {
    const n = {};
    objs.forEach(o => { const t = o.tipo || 'tabla'; n[t] = (n[t] || 0) + 1; });
    return Object.keys(TIPOS_OBJETO).filter(t => n[t])
      .map(t => `${n[t]} ${n[t] === 1 ? TIPOS_OBJETO[t].uno : TIPOS_OBJETO[t].varios}`).join(' · ');
  }

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
        <a href="cuenta.html" class="flex items-center gap-2 w-full px-2.5 py-1.5 rounded text-xs text-neutral-200 hover:bg-marga-card transition-colors">
          <span class="material-symbols-outlined text-[15px]">key</span><span>Cuenta y tokens</span>
        </a>
        <button type="button" id="logout-btn" class="flex items-center gap-2 w-full px-2.5 py-1.5 rounded text-xs text-neutral-400 hover:bg-neutral-500/20 transition-colors">
          <span class="material-symbols-outlined text-[15px]">logout</span><span>Cerrar sesión</span>
        </button>`;
      chip.appendChild(menu);
      menu.addEventListener('click', (ev) => ev.stopPropagation());
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
    demo: !!demo,
    ApiError,
    me: null,
    get: (p) => request('GET', p),
    post: (p, b) => request('POST', p, b ?? {}),
    patch: (p, b) => request('PATCH', p, b ?? {}),
    del: (p) => request('DELETE', p),
    escapeHTML, timeAgo, initials, MOTORES, TIPOS_OBJETO, tipoObjeto, contarObjetos,
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
