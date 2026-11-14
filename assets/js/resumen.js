/**
 * Resumen del proyecto (code.html)
 * Muestra el estado del esquema en la rama elegida, el último commit,
 * las ramas y las personas con acceso.
 */
(function () {
  const API = window.MargaAPI;
  const $ = (id) => document.getElementById(id);
  const e = (v) => API.escapeHTML(v);
  let repo = null;
  let tablas = [];
  let actual = null; // tabla seleccionada

  if (!API.online) {
    $('offline-note').classList.remove('hidden');
    $('r-last').classList.add('hidden');
    return;
  }

  // marga-layout.js emite el repositorio cuando lo carga.
  document.addEventListener('marga:repo', async (ev) => {
    repo = ev.detail;
    try {
      pintarProyecto();
      const [branches, personas, entornos] = await Promise.all([
        API.get(`/repos/${repo.id}/branches`),
        API.get(`/repos/${repo.id}/colaboradores`),
        API.get(`/repos/${repo.id}/entornos`).catch(() => [])
      ]);
      pintarEntornos(entornos);
      pintarRamas(branches);
      pintarPersonas(personas);
      const pedida = new URLSearchParams(location.search).get('branch');
      const rama = branches.some(b => b.nombre === pedida) ? pedida : (branches[0]?.nombre || 'main');
      const sel = $('r-branch');
      sel.innerHTML = branches.map(b =>
        `<option class="bg-marga-card" value="${e(b.nombre)}"${b.nombre === rama ? ' selected' : ''}>${e(b.nombre)}</option>`).join('');
      sel.onchange = () => cargarRama(sel.value, branches);
      await cargarRama(rama, branches);
    } catch (err) {
      showToast(err.message, 'error');
    }
  });

  function pintarProyecto() {
    $('r-nombre').textContent = repo.nombre;
    $('r-desc').textContent = repo.descripcion || '';
    const badge = (icon, txt) => `<span class="inline-flex items-center gap-1.5 h-6 px-2 rounded border border-marga-border text-xs text-neutral-300">
      <span class="material-symbols-outlined text-[14px] text-neutral-500">${icon}</span>${e(txt)}</span>`;
    $('r-badges').innerHTML = [
      badge('database', API.MOTORES[repo.motor] || repo.motor),
      badge('layers', repo.entorno),
      badge(repo.visibilidad === 'publico' ? 'public' : 'lock', repo.visibilidad === 'publico' ? 'Público' : 'Privado'),
      badge('badge', 'Tu acceso: ' + repo.permiso)
    ].join('');
    $('r-cli').textContent = [
      `marga login --servidor ${location.origin}`,
      `marga init --repo ${repo.id} --servidor ${location.origin}`,
      'marga pull',
      'marga env add dev "postgres://usuario:clave@localhost:5432/bd?sslmode=disable"',
      'marga up'
    ].join('\n');
    $('r-cli-copiar').onclick = () =>
      navigator.clipboard.writeText($('r-cli').textContent).then(() => showToast('Comandos copiados', 'success'));
  }

  async function cargarRama(rama, branches) {
    $('r-ver-commits').href = `commits.html?repo=${repo.id}&branch=${encodeURIComponent(rama)}`;
    const b = branches.find(x => x.nombre === rama);
    const last = $('r-last');
    if (b && b.ultimo_commit) {
      const c = b.ultimo_commit;
      last.href = `commits.html?repo=${repo.id}&branch=${encodeURIComponent(rama)}`;
      last.innerHTML = `
        <span class="w-7 h-7 shrink-0 rounded-full bg-marga-card border border-marga-border flex items-center justify-center text-[10px] font-semibold text-neutral-300">${e(API.initials(c.autor || '?'))}</span>
        <span class="font-medium text-white shrink-0">${e(c.autor || '—')}</span>
        <span class="text-neutral-400 truncate">${e(c.mensaje.split('\n')[0])}</span>
        <span class="ml-auto flex items-center gap-3 shrink-0 text-xs">
          <span class="font-mono text-neutral-300">${e(c.hash_corto)}</span>
          <span class="text-neutral-500">${e(API.timeAgo(c.fecha))}</span>
        </span>`;
    } else {
      last.removeAttribute('href');
      last.innerHTML = `<span class="text-neutral-500">La rama <span class="font-mono text-neutral-300">${e(rama)}</span> todavía no tiene commits.</span>`;
    }

    const esq = await API.get(`/repos/${repo.id}/esquema?ref=${encodeURIComponent(rama)}`);
    if (esq.modo === 'sql') {
      // Sin snapshots por tabla: mostramos el SQL acumulado como un único archivo.
      tablas = esq.sql ? [{ tabla: 'migraciones.sql', ddl: esq.sql, commit: null }] : [];
    } else {
      tablas = esq.tablas;
    }
    $('r-tablas-count').textContent = esq.modo === 'sql' ? 'SQL acumulado' : `${tablas.length} tabla${tablas.length === 1 ? '' : 's'}`;
    const lista = $('r-tablas');
    if (!tablas.length) {
      lista.innerHTML = '<li class="px-4 py-3 text-neutral-500">Sin tablas todavía.</li>';
      mostrar(null);
      return;
    }
    lista.innerHTML = tablas.map((t, i) => `
      <li><button type="button" data-i="${i}" class="r-tabla w-full flex items-center gap-2 px-4 py-2 text-left text-neutral-300 hover:bg-marga-card hover:text-white transition-colors">
        <span class="material-symbols-outlined text-[16px] text-neutral-500">${esq.modo === 'sql' ? 'description' : 'table'}</span>
        <span class="font-mono text-[13px] truncate">${e(t.tabla)}</span>
      </button></li>`).join('');
    lista.querySelectorAll('.r-tabla').forEach(btn => btn.addEventListener('click', () => mostrar(tablas[btn.dataset.i])));
    const previa = actual && tablas.find(t => t.tabla === actual.tabla);
    mostrar(previa || tablas[0]);
  }

  function mostrar(t) {
    actual = t;
    document.querySelectorAll('.r-tabla').forEach(b => {
      const on = t && tablas[b.dataset.i] === t;
      b.classList.toggle('bg-marga-card', on);
      b.classList.toggle('text-white', on);
    });
    $('r-ddl-titulo').textContent = t ? t.tabla : '';
    $('r-ddl').textContent = t ? t.ddl : '';
    $('r-ddl-pie').innerHTML = t && t.commit
      ? `Último cambio: <span class="text-neutral-300">${e(t.commit.mensaje.split('\n')[0])}</span> · <span class="font-mono">${e(t.commit.hash_corto)}</span> · ${e(t.commit.autor || '—')} · ${e(API.timeAgo(t.commit.fecha))}`
      : '';
  }

  $('r-copiar').addEventListener('click', () => {
    if (!actual) return;
    navigator.clipboard.writeText(actual.ddl).then(() => showToast('DDL copiado', 'success'));
  });

  function pintarEntornos(lista) {
    $('r-entornos-link').href = `entornos.html?repo=${repo.id}`;
    if (!lista.length) {
      $('r-entornos').innerHTML = '<li class="px-4 py-2.5 text-xs text-neutral-500">Ninguno ha reportado aún. Ejecuta <span class="font-mono text-neutral-300">marga status</span>.</li>';
      return;
    }
    $('r-entornos').innerHTML = lista.map(x => {
      const estado = x.drift.length ? `${x.drift.length} fuera de Marga`
        : x.pendientes ? `${x.pendientes} pendiente${x.pendientes === 1 ? '' : 's'}` : 'al día';
      return `<li><a href="entornos.html?repo=${repo.id}&entorno=${encodeURIComponent(x.nombre)}" class="flex items-center justify-between gap-3 px-4 py-2 hover:bg-marga-card transition-colors">
        <span class="font-mono text-[13px] text-neutral-200 truncate">${e(x.nombre)}</span>
        <span class="text-xs shrink-0 ${estado === 'al día' ? 'text-neutral-500' : 'text-white'}">${e(estado)}</span>
      </a></li>`;
    }).join('');
  }

  function pintarRamas(branches) {
    $('r-ramas-count').textContent = branches.length;
    $('r-ramas').innerHTML = branches.map(b => `
      <li><a href="commits.html?repo=${repo.id}&branch=${encodeURIComponent(b.nombre)}" class="flex items-center justify-between gap-3 px-4 py-2 hover:bg-marga-card transition-colors">
        <span class="font-mono text-[13px] text-neutral-200 truncate">${e(b.nombre)}</span>
        <span class="text-xs text-neutral-500 shrink-0">${b.ultimo_commit ? e(API.timeAgo(b.ultimo_commit.fecha)) : 'vacía'}</span>
      </a></li>`).join('');
  }

  function pintarPersonas(colabs) {
    const filas = [{ nombre: repo.propietario, detalle: 'propietario' }]
      .concat(colabs.map(c => ({ nombre: c.nombre, detalle: c.permiso })));
    $('r-personas-count').textContent = filas.length;
    $('r-personas').innerHTML = filas.map(p => `
      <li class="flex items-center gap-3 px-4 py-2">
        <span class="w-6 h-6 rounded-full bg-marga-card border border-marga-border flex items-center justify-center text-[10px] font-semibold text-neutral-300">${e(API.initials(p.nombre))}</span>
        <span class="text-neutral-200 truncate">${e(p.nombre)}</span>
        <span class="ml-auto text-xs text-neutral-500">${e(p.detalle)}</span>
      </li>`).join('');
  }
})();
