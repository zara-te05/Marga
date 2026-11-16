// Regenera web/assets/js/demo-datos.js a partir de un marga-server con datos.
// Uso: node scripts/grabar-demo.mjs TOKEN   (token de /api/auth/login; ajusta REPOS abajo)
// Graba respuestas reales del servidor para el modo demo del front.
const BASE = 'http://127.0.0.1:8080/api';
const TOKEN = process.argv[2];
const REPOS = [7, 1, 2];
const datos = {};
const clave = (m, ruta) => {
  const u = new URL('http://x' + ruta);
  ['limit', 'offset'].forEach(k => u.searchParams.delete(k));
  u.searchParams.sort();
  return m + ' ' + u.pathname + (u.search || '');
};
async function pedir(m, ruta, body) {
  const r = await fetch(BASE + ruta, {
    method: m, headers: { Authorization: 'Bearer ' + TOKEN, 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined
  });
  const j = await r.json().catch(() => null);
  if (!r.ok) { console.error('✗', m, ruta, r.status, j && j.error); return null; }
  return j;
}
async function g(ruta) { const j = await pedir('GET', ruta); if (j !== null) datos[clave('GET', ruta)] = j; return j; }

await g('/health'); await g('/auth/me'); await g('/equipos'); await g('/tokens');
const repos = await pedir('GET', '/repos');
datos['GET /repos'] = repos.filter(r => REPOS.includes(r.id));
for (const id of REPOS) {
  const R = '/repos/' + id;
  await g(R); await g(R + '/colaboradores'); await g(R + '/auditoria');
  const ramas = await g(R + '/branches');
  await g(R + '/entornos');
  for (const b of ramas) {
    const rama = encodeURIComponent(b.nombre);
    const h = await g(R + '/commits?branch=' + rama + '&limit=200');
    await g(R + '/esquema?ref=' + rama);
    await g(R + '/entornos?rama=' + rama);
    for (const c of (h && h.commits) || []) {
      if (datos[clave('GET', R + '/commits/' + c.hash)]) continue;
      await g(R + '/commits/' + c.hash);
      await g(R + '/commits/' + c.hash + '/diff');
    }
    if (b.nombre !== 'main') {
      await g(R + '/diff?from=main&to=' + rama);
      const m = await pedir('POST', R + '/fusionar', { desde: b.nombre, hacia: 'main', simular: true });
      if (m) datos['FUSIONAR ' + R + ' ' + b.nombre + ' main'] = m;
    }
  }
  for (const e of (await pedir('GET', R + '/entornos')) || []) await g(R + '/entornos/' + encodeURIComponent(e.nombre));
}
const txt = '// Datos del modo demo, grabados de un servidor real con datos de ejemplo.\n' +
  '// Se regeneran con: node scripts/grabar-demo.mjs TOKEN  (servidor en :8080)\n' +
  'window.MARGA_DEMO_DATOS = ' + JSON.stringify(datos) + ';\n';
(await import('node:fs')).writeFileSync(process.argv[3] || new URL('../web/assets/js/demo-datos.js', import.meta.url), txt);
console.log(Object.keys(datos).length, 'respuestas,', (txt.length / 1024).toFixed(0), 'KB');
