/**
 * Entornos del proyecto (entornos.html)
 * Lista lo que reportó la CLI de cada base: aplicadas, pendientes y drift.
 */
(function () {
  const API = window.MargaAPI;
  const $ = (id) => document.getElementById(id);
  const e = (v) => API.escapeHTML(v);
  let repo = null;
  let entornos = [];
  let elegido = null;

  if (!API.online) return;

  document.addEventListener('marga:repo', async (ev) => {
    repo = ev.detail;
    try {
      entornos = await API.get(`/repos/${repo.id}/entornos`);
      $('e-rama').textContent = 'Comparado con la rama main';
      if (!entornos.length) {
        $('e-vacio').classList.remove('hidden');
        $('e-contenido').classList.add('hidden');
        return;
      }
      const pedido = new URLSearchParams(location.search).get('entorno');
      pintarLista();
      seleccionar(entornos.some(x => x.nombre === pedido) ? pedido : entornos[0].nombre);
    } catch (err) {
      showToast(err.message, 'error');
    }
  });

  function estado(x) {
    if (x.drift.length) return { txt: `${x.drift.length} cambio${x.drift.length === 1 ? '' : 's'} fuera de Marga`, icon: 'warning', fuerte: true };
    if (x.pendientes) return { txt: `${x.pendientes} pendiente${x.pendientes === 1 ? '' : 's'}`, icon: 'schedule', fuerte: true };
    return { txt: 'Al día', icon: 'check_circle', fuerte: false };
  }

  function pintarLista() {
    $('e-lista').innerHTML = entornos.map(x => {
      const st = estado(x);
      return `
      <button type="button" data-n="${e(x.nombre)}" class="e-item text-left rounded-lg border border-marga-border bg-marga-surface px-4 py-3 hover:border-neutral-600 transition-colors">
        <div class="flex items-center justify-between gap-2">
          <span class="font-mono text-sm text-white truncate">${e(x.nombre)}</span>
          <span class="text-[10px] uppercase tracking-wider text-neutral-500 shrink-0">${x.compartido ? 'compartido' : 'personal'}</span>
        </div>
        <div class="flex items-center gap-1.5 mt-2 text-xs ${st.fuerte ? 'text-white' : 'text-neutral-400'}">
          <span class="material-symbols-outlined text-[15px]">${st.icon}</span>${e(st.txt)}
        </div>
        <div class="text-[11px] text-neutral-500 mt-1">${x.aplicadas} aplicadas · reportado ${e(API.timeAgo(x.reportado_en))}${x.reportado_por ? ' por ' + e(x.reportado_por) : ''}</div>
      </button>`;
    }).join('');
    document.querySelectorAll('.e-item').forEach(b => b.addEventListener('click', () => seleccionar(b.dataset.n)));
  }

  async function seleccionar(nombre) {
    elegido = nombre;
    document.querySelectorAll('.e-item').forEach(b => {
      const on = b.dataset.n === nombre;
      b.classList.toggle('border-neutral-400', on);
      b.classList.toggle('border-marga-border', !on);
    });
    const url = new URL(location.href);
    url.searchParams.set('entorno', nombre);
    history.replaceState(null, '', url);
    $('e-detalle').innerHTML = '<div class="text-sm text-neutral-500">Cargando…</div>';
    try {
      const d = await API.get(`/repos/${repo.id}/entornos/${encodeURIComponent(nombre)}`);
      if (elegido === nombre) pintarDetalle(d);
    } catch (err) {
      showToast(err.message, 'error');
    }
  }

  const tarjeta = (titulo, extra, cuerpo) => `
    <section class="rounded-lg border border-marga-border bg-marga-surface overflow-hidden">
      <div class="flex items-center justify-between px-4 h-11 border-b border-marga-border">
        <h2 class="text-sm font-medium text-white">${titulo}</h2>
        <span class="text-xs text-neutral-500">${extra}</span>
      </div>
      ${cuerpo}
    </section>`;

  function pintarDetalle(d) {
    const comando = `marga up --env ${d.compartido ? d.nombre : d.nombre.split('@')[0]}`;
    const tablas = d.esquema && d.esquema.tablas ? d.esquema.tablas.length : 0;

    const cabecera = `
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <div class="font-mono text-lg text-white">${e(d.nombre)}</div>
          <div class="text-xs text-neutral-500 mt-1">${e(API.MOTORES[d.motor] || d.motor)} · ${d.compartido ? 'compartido por el equipo' : 'base personal'} · ${tablas} tablas · reportado ${e(new Date(d.reportado_en).toLocaleString('es-MX'))}${d.reportado_por ? ' por ' + e(d.reportado_por) : ''}</div>
        </div>
        <a href="esquema.html?repo=${repo.id}&entorno=${encodeURIComponent(d.nombre)}" class="flex items-center gap-1.5 h-9 px-3 rounded-md border border-marga-border text-sm text-neutral-200 hover:border-neutral-500 transition-colors shrink-0">
          <span class="material-symbols-outlined text-[17px]">account_tree</span>Ver esquema
        </a>
      </div>`;

    const drift = d.drift.length ? tarjeta('Cambios hechos fuera de Marga', `${d.drift.length}`, `
      <ul class="px-4 py-3 font-mono text-xs text-neutral-200 flex flex-col gap-1.5">${d.drift.map(x => `<li>${e(x)}</li>`).join('')}</ul>
      <p class="px-4 pb-3 text-xs text-neutral-500">Alguien modificó esta base directamente. Si los cambios son correctos: <span class="font-mono text-neutral-300">marga drift --aceptar</span>. Mientras tanto, <span class="font-mono text-neutral-300">marga up</span> no aplicará nada.</p>`) : '';

    const pendientes = tarjeta('Pendientes', d.lista_pendientes.length ? `${d.lista_pendientes.length} de main` : 'al día con main',
      d.lista_pendientes.length ? `
        <ul class="divide-y divide-marga-border">${d.lista_pendientes.map(c => `
          <li><a href="commits.html?repo=${repo.id}" class="flex items-center gap-3 px-4 py-2.5 text-sm hover:bg-marga-card">
            <span class="font-mono text-xs text-neutral-400 shrink-0">${e(c.hash_corto)}</span>
            <span class="text-neutral-100 truncate">${e(c.mensaje.split('\n')[0])}</span>
            <span class="ml-auto text-xs text-neutral-500 shrink-0">${e(c.autor || '—')}</span>
          </a></li>`).join('')}</ul>
        <div class="px-4 py-2.5 border-t border-marga-border text-xs text-neutral-500">Para aplicarlas: <span class="font-mono text-neutral-300">${e(comando)}</span></div>`
        : '<div class="px-4 py-4 text-sm text-neutral-400">Este entorno tiene aplicadas todas las migraciones de main.</div>');

    const aplicadas = tarjeta('Aplicadas', `${d.lista_aplicadas.length}`, d.lista_aplicadas.length ? `
      <div class="overflow-x-auto"><table class="w-full text-sm">
        <thead><tr class="text-left text-xs text-neutral-500 border-b border-marga-border">
          <th class="px-4 py-2 font-medium">Migración</th><th class="px-4 py-2 font-medium">Aplicada</th>
          <th class="px-4 py-2 font-medium">Por</th><th class="px-4 py-2 font-medium text-right">Duración</th></tr></thead>
        <tbody>${d.lista_aplicadas.slice().reverse().map(a => `
          <tr class="border-b border-marga-border last:border-0">
            <td class="px-4 py-2"><span class="font-mono text-[13px] text-neutral-100">${e(a.version + '_' + a.nombre)}</span>
              ${a.commit ? '' : '<span class="ml-2 text-[10px] uppercase tracking-wider text-neutral-500">sin enviar</span>'}</td>
            <td class="px-4 py-2 text-neutral-400 whitespace-nowrap" title="${e(new Date(a.aplicada_en).toLocaleString('es-MX'))}">${e(API.timeAgo(a.aplicada_en))}</td>
            <td class="px-4 py-2 text-neutral-400">${e(a.aplicada_por)}</td>
            <td class="px-4 py-2 text-neutral-400 text-right font-mono text-xs">${a.duracion_ms}ms</td>
          </tr>`).join('')}</tbody></table></div>`
      : '<div class="px-4 py-4 text-sm text-neutral-400">Aún no se ha aplicado ninguna migración.</div>');

    $('e-detalle').innerHTML = cabecera + drift + pendientes + aplicadas;
  }
})();
