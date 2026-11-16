/**
 * Modo demo: simula la API de Marga con datos grabados de un servidor real
 * (assets/js/demo-datos.js), para revisar todas las pantallas sin Go ni PostgreSQL.
 *
 * Se activa solo:
 *   - al abrir las páginas con doble clic (file://)
 *   - con ?demo en la URL (se recuerda en la pestaña; ?demo=0 lo apaga)
 *
 * Todo se lee; nada se guarda. Las acciones que cambiarían datos muestran un aviso,
 * salvo simular una fusión, que devuelve el resultado grabado.
 */
(function () {
  const params = new URLSearchParams(location.search);
  let activo = location.protocol === 'file:';
  try {
    if (params.get('demo') === '0') sessionStorage.removeItem('marga_demo');
    else if (params.has('demo')) sessionStorage.setItem('marga_demo', '1');
    if (sessionStorage.getItem('marga_demo') === '1') activo = true;
  } catch (_) {
    if (params.has('demo') && params.get('demo') !== '0') activo = true;
  }
  if (!activo) { window.MargaDemo = null; return; }

  const AVISO = 'Modo demo: los cambios no se guardan. Abre Marga desde el servidor para editar.';
  const yo = document.currentScript && document.currentScript.src;

  // Carga demo-datos.js con <script> (fetch no funciona en file://).
  let datos = null;
  const cargar = new Promise((resolve, reject) => {
    if (window.MARGA_DEMO_DATOS) return resolve();
    const s = document.createElement('script');
    s.src = yo ? yo.replace(/marga-demo\.js(\?.*)?$/, 'demo-datos.js') : '../assets/js/demo-datos.js';
    s.onload = resolve;
    s.onerror = () => reject(new Error('No encontré assets/js/demo-datos.js'));
    document.head.appendChild(s);
  }).then(() => {
    // Las fechas grabadas se recorren para que "hace 2 h" siga pareciendo reciente.
    let txt = JSON.stringify(window.MARGA_DEMO_DATOS);
    const iso = /"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))"/g;
    let max = 0;
    txt.replace(iso, (_, d) => { max = Math.max(max, Date.parse(d)); return _; });
    const corrimiento = max ? Date.now() - max - 20 * 60 * 1000 : 0;
    txt = txt.replace(iso, (_, d) => '"' + new Date(Date.parse(d) + corrimiento).toISOString() + '"');
    datos = JSON.parse(txt);
  });

  const clave = (m, ruta) => {
    const u = new URL('http://x' + ruta);
    ['limit', 'offset'].forEach(k => u.searchParams.delete(k));
    u.searchParams.sort();
    return m + ' ' + u.pathname + (u.search || '');
  };
  const copia = (v) => JSON.parse(JSON.stringify(v));

  function error(status, msg) {
    const e = new Error(msg);
    e.status = status;
    return e;
  }

  // Todos los commits grabados de un proyecto (de todas sus ramas).
  function commitsDe(repo) {
    const vistos = new Map();
    for (const [k, v] of Object.entries(datos)) {
      if (k.startsWith(`GET /repos/${repo}/commits?`) && v && v.commits) v.commits.forEach(c => vistos.set(c.hash, c));
    }
    return [...vistos.values()];
  }

  // Resuelve un commit por hash, prefijo o nombre de migración, como el servidor.
  function resolverCommit(repo, ref) {
    ref = decodeURIComponent(ref);
    const cs = commitsDe(repo);
    return cs.find(c => c.hash === ref) || cs.find(c => c.hash.startsWith(ref)) ||
      cs.find(c => c.mensaje.split('\n')[0] === ref || c.mensaje.startsWith(ref + '_'));
  }

  function get(ruta) {
    const k = clave('GET', ruta);
    if (k in datos) return copia(datos[k]);
    const u = new URL('http://x' + ruta);
    const p = u.pathname;
    let m;
    if ((m = p.match(/^\/repos\/(\d+)\/commits\/([^/]+)(\/diff)?$/))) {
      const c = resolverCommit(m[1], m[2]);
      const v = c && datos[`GET /repos/${m[1]}/commits/${c.hash}${m[3] || ''}`];
      if (v) return copia(v);
      throw error(404, 'commit no encontrado');
    }
    if ((m = p.match(/^\/repos\/(\d+)\/buscar$/))) {
      const q = (u.searchParams.get('q') || '').toLowerCase();
      return copia(commitsDe(m[1]).filter(c => c.mensaje.toLowerCase().includes(q) || c.hash.startsWith(q) ||
        (c.autor || '').toLowerCase().includes(q)).map(c => ({ ...c, rango: 1 })));
    }
    if ((m = p.match(/^\/repos\/(\d+)\/(commits|esquema|entornos)$/))) {
      // Rama o commit sin grabar: se muestra lo de main.
      const alt = { commits: '?branch=main', esquema: '?ref=main', entornos: '' }[m[2]];
      const v = datos[`GET /repos/${m[1]}/${m[2]}${alt}`];
      if (v) return copia(v);
    }
    if ((m = p.match(/^\/repos\/(\d+)\/diff$/))) {
      const v = datos[clave('GET', `/repos/${m[1]}/diff?from=main&to=${encodeURIComponent(u.searchParams.get('to') || '')}`)];
      if (v) return copia(v);
    }
    if ((m = p.match(/^\/repos\/(\d+)\/(colaboradores|auditoria|branches|entornos)$/))) return [];
    if (/^\/repos\/\d+$/.test(p)) throw error(404, 'repositorio no encontrado');
    throw error(404, 'Esto no está disponible en el modo demo');
  }

  async function responder(metodo, ruta, body) {
    await cargar;
    await new Promise(r => setTimeout(r, 60)); // que se note la carga, como en el real
    if (metodo === 'GET') return get(ruta);
    const me = datos['GET /auth/me'];
    switch (ruta) {
      case '/auth/login':
      case '/auth/register':
      case '/auth/restablecer':
        return { usuario: me, token: 'demo', expira_en: new Date(Date.now() + 864e5).toISOString() };
      case '/auth/logout':
        try { sessionStorage.removeItem('marga_demo'); } catch (_) { }
        return { ok: true };
      case '/auth/recuperar':
        return { ok: true, mensaje: 'Si ese email tiene cuenta, te enviamos un enlace para restablecer la contraseña.' };
    }
    const m = ruta.match(/^\/repos\/(\d+)\/fusionar$/);
    if (m && body && body.simular) {
      const v = datos[`FUSIONAR /repos/${m[1]} ${body.desde} ${body.hacia}`];
      if (v) return copia(v);
      return { tipo: 'al_dia', aplicado: false, nuevos: [], conflictos: [], renumeradas: [] };
    }
    throw error(0, AVISO);
  }

  function aviso() {
    if (document.getElementById('marga-demo-aviso')) return;
    const el = document.createElement('div');
    el.id = 'marga-demo-aviso';
    el.style.cssText = 'position:fixed;bottom:12px;left:12px;z-index:90;display:flex;align-items:center;gap:8px;' +
      'padding:6px 12px;border-radius:999px;background:rgba(17,17,17,.95);border:1px solid #262626;' +
      'font-size:11px;color:#d4d4d4;box-shadow:0 8px 24px rgba(0,0,0,.4);pointer-events:none';
    el.innerHTML = '<span style="width:6px;height:6px;border-radius:999px;background:#a3a3a3"></span>Modo demo · datos de ejemplo, nada se guarda';
    document.body.appendChild(el);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', aviso);
  else aviso();

  window.MargaDemo = { activo: true, responder, cargar };
})();
