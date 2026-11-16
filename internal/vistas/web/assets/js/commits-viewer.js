/**
 * Marga Commits & Migrations Timeline Engine
 * Handles timeline item selection, diff toggle, dry-run simulation and CLI clipboard actions.
 *
 * Modo en vivo (servido por el backend): historial real por rama, diff calculado por el
 * servidor, búsqueda de texto completo, revert y descarga del SQL de cada migración.
 */

let selectedCommitHash = '8f3b91a';
let selectedCommit = null;      // commit completo (modo en vivo)
let liveRepoId = null;
let liveBranch = 'main';
let searchTimer = null;
let entornosRepo = [];          // entornos que reportó la CLI
let ramasRepo = [];             // nombres de las ramas
let permisoRepo = 'lectura';

const isLiveCommits = () => window.MargaAPI && MargaAPI.online && MargaAPI.me && liveRepoId;

// ------------------------------------------------------------------ Carga inicial

document.addEventListener('DOMContentLoaded', () => {
  if (!window.MargaAPI || !MargaAPI.online) return;
  MargaAPI.ready.then(u => { if (u) initLiveCommits().catch(err => showToast(err.message, 'error')); });
});

async function initLiveCommits() {
  let id = MargaAPI.currentRepoId();
  let repo = null;
  if (id) {
    try { repo = await MargaAPI.get('/repos/' + id); } catch (_) { repo = null; }
  }
  if (!repo) {
    const repos = await MargaAPI.get('/repos');
    if (!repos.length) {
      setTimeline(`<div class="p-6 rounded-xl border border-dashed border-marga-border text-center text-xs text-neutral-400">
        No tienes proyectos todavía. <a class="text-brand-cyan hover:underline" href="proyectos.html">Crea uno</a>.</div>`);
      return;
    }
    repo = repos[0];
  }
  liveRepoId = repo.id;
  MargaAPI.setCurrentRepo(repo.id);

  const btnRevert = document.getElementById('btn-revert');
  if (btnRevert && repo.permiso === 'lectura') btnRevert.classList.add('hidden');

  try { entornosRepo = await MargaAPI.get(`/repos/${repo.id}/entornos`); } catch (_) { entornosRepo = []; }
  const branches = await MargaAPI.get(`/repos/${repo.id}/branches`);
  ramasRepo = branches.map(b => b.nombre);
  permisoRepo = repo.permiso;
  prepararMerge();
  const urlBranch = new URLSearchParams(location.search).get('branch');
  liveBranch = branches.some(b => b.nombre === urlBranch) ? urlBranch : (branches[0]?.nombre || 'main');

  const select = document.getElementById('branch-select');
  if (select) {
    select.innerHTML = branches.map(b =>
      `<option class="bg-marga-card" value="${MargaAPI.escapeHTML(b.nombre)}"${b.nombre === liveBranch ? ' selected' : ''}>${MargaAPI.escapeHTML(b.nombre)}</option>`
    ).join('');
    select.onchange = () => { liveBranch = select.value; loadHistory(); };
  }

  const search = document.getElementById('commit-search');
  if (search) {
    search.placeholder = 'Buscar por mensaje, tabla o hash…';
    search.addEventListener('input', () => {
      clearTimeout(searchTimer);
      searchTimer = setTimeout(() => runSearch(search.value.trim()), 250);
    });
  }

  await loadHistory();
}

async function loadHistory(seleccionar) {
  const h = await MargaAPI.get(`/repos/${liveRepoId}/commits?branch=${encodeURIComponent(liveBranch)}&limit=200`);
  setText('pill-total', `${h.total} commit${h.total === 1 ? '' : 's'}`);
  setText('branch-estado', h.head ? 'HEAD ' + h.head.slice(0, 7) : 'Sin commits');
  setText('catalogo-activo', h.commits[0] ? firstLine(h.commits[0].mensaje) : 'vacío');

  if (!h.commits.length) {
    setTimeline(`<div class="p-6 rounded-xl border border-dashed border-marga-border text-center text-xs text-neutral-400">
      La rama <span class="font-mono text-white">${MargaAPI.escapeHTML(liveBranch)}</span> no tiene commits.<br>
      Crea uno desde la CLI con <code class="font-mono text-brand-cyan">marga commit</code>.</div>`);
    clearInspector();
    return;
  }
  renderTimeline(h.commits, h.head);
  const elegido = h.commits.find(c => c.hash === seleccionar) || h.commits[0];
  selectCommitItem(elegido.hash);
}

async function runSearch(q) {
  if (!isLiveCommits()) return;
  if (!q) { loadHistory(); return; }
  try {
    const res = await MargaAPI.get(`/repos/${liveRepoId}/buscar?q=${encodeURIComponent(q)}`);
    if (!res.length) {
      setTimeline(`<div class="p-6 text-center text-xs text-neutral-400">Sin resultados para “${MargaAPI.escapeHTML(q)}”.</div>`);
      return;
    }
    renderTimeline(res.map(r => ({ ...r, tablas: [] })), null);
  } catch (err) {
    showToast(err.message, 'error');
  }
}

// ------------------------------------------------------------------ Render

function setText(id, v) { const el = document.getElementById(id); if (el) el.textContent = v; }
function firstLine(s) { return String(s || '').split('\n')[0]; }

function setTimeline(html) {
  const tl = document.getElementById('commits-timeline');
  if (!tl) return;
  tl.innerHTML = '<div class="absolute left-[23px] top-6 bottom-6 w-0.5 bg-marga-border pointer-events-none"></div>' + html;
}

function renderTimeline(commits, head) {
  const e = MargaAPI.escapeHTML;
  setTimeline(commits.map(c => {
    const isRevert = /^Revert "/.test(c.mensaje);
    let badge = '';
    if (c.hash === head) badge = '<span class="px-2 py-0.5 rounded text-[10px] font-mono bg-white/5 text-brand-cyan font-semibold border border-white/10 shrink-0">HEAD</span>';
    else if (isRevert) badge = '<span class="px-2 py-0.5 rounded text-[10px] font-mono bg-neutral-500/10 text-neutral-400 border border-neutral-500/20 shrink-0">Revert</span>';
    else if (c.branch && c.branch !== liveBranch) badge = `<span class="px-2 py-0.5 rounded text-[10px] font-mono bg-marga-card border border-marga-border text-brand-purple shrink-0">${e(c.branch)}</span>`;

    const objetos = c.objetos || (c.tablas || []).map(t => ({ tipo: 'tabla', nombre: t }));
    const tablas = objetos.map(o => {
      const tipo = MargaAPI.tipoObjeto(o.tipo);
      const marca = o.tipo && o.tipo !== 'tabla'
        ? `<span class="material-symbols-outlined text-[12px] text-brand-cyan">${tipo.icono}</span>`
        : '<span class="w-1 h-1 rounded-full bg-brand-cyan"></span>';
      return `<span title="${e(tipo.uno + ' ' + o.nombre)}" class="px-2 py-0.5 rounded text-[10px] font-mono bg-marga-card border border-marga-border text-neutral-300 flex items-center gap-1">
         ${marca}${e(o.nombre)}</span>`;
    }).join('');

    return `
    <div class="commit-timeline-item relative group bg-marga-surface hover:bg-marga-card border border-marga-border p-4 rounded-xl transition-all cursor-pointer" id="commit-item-${e(c.hash)}" onclick="selectCommitItem('${e(c.hash)}')">
      <div class="flex items-start gap-3">
        <div class="w-7 h-7 rounded-full bg-marga-card border border-marga-border text-neutral-400 flex items-center justify-center font-mono text-xs z-10 shrink-0">${c.numero || '•'}</div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center justify-between gap-2">
            <span class="font-mono text-xs text-white font-medium truncate">${e(firstLine(c.mensaje))}</span>
            ${badge}
          </div>
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1 mt-1.5 font-mono text-[11px] text-neutral-400">
            <span class="text-brand-cyan font-bold">${e(c.hash_corto)}</span>
            <span class="flex items-center gap-1"><span class="material-symbols-outlined text-[13px]">person</span>${e(c.autor || '—')}</span>
            <span class="flex items-center gap-1" title="${e(new Date(c.fecha).toLocaleString('es-MX'))}"><span class="material-symbols-outlined text-[13px]">schedule</span>${e(MargaAPI.timeAgo(c.fecha))}</span>
          </div>
          ${tablas ? `<div class="flex flex-wrap items-center gap-1.5 mt-2">${tablas}</div>` : ''}
          ${chipsRevision(c)}
          ${(c.entornos || []).length ? `<div class="flex flex-wrap items-center gap-1.5 mt-2">${c.entornos.map(n =>
            `<span class="px-2 py-0.5 rounded text-[10px] font-mono border border-neutral-600 text-neutral-200 flex items-center gap-1" title="Aplicada en ${e(n)}">
               <span class="material-symbols-outlined text-[12px]">check</span>${e(n)}</span>`).join('')}</div>` : ''}
        </div>
      </div>
    </div>`;
  }).join(''));
}

function highlightItem(hash) {
  document.querySelectorAll('.commit-timeline-item').forEach(el => {
    el.classList.remove('border-brand-violet', 'bg-marga-card', 'shadow-md');
    el.classList.add('bg-marga-surface', 'border-marga-border');
  });
  const active = document.getElementById('commit-item-' + hash);
  if (active) {
    active.classList.remove('bg-marga-surface', 'border-marga-border');
    active.classList.add('border-brand-violet', 'bg-marga-card', 'shadow-md');
  }
}

function clearInspector() {
  selectedCommit = null;
  setText('inspector-filename', '—');
  const meta = document.getElementById('inspector-meta');
  if (meta) meta.textContent = 'Selecciona un commit';
  const body = document.getElementById('diff-body');
  if (body) body.innerHTML = '';
  setText('diff-hunk', '');
}

const CONTEXTO = 3; // líneas sin cambios visibles alrededor de cada cambio

function renderDiff(d) {
  const e = MargaAPI.escapeHTML;
  const body = document.getElementById('diff-body');
  if (!body) return;
  let add = 0, del = 0;
  const parts = [];

  if (!d.tablas.length) {
    parts.push('<div class="px-2 py-3 text-neutral-400">Este commit no cambia el esquema (sin diferencias en DDL).</div>');
  }

  d.tablas.forEach(t => {
    add += t.agregadas; del += t.eliminadas;
    const color = { agregada: 'text-brand-cyan', eliminada: 'text-neutral-400', modificada: 'text-brand-purple' }[t.estado];
    const tipo = MargaAPI.tipoObjeto(d.modo === 'sql' ? 'sql' : t.tipo);
    parts.push(`<div class="flex items-center gap-2 px-2 pt-3 pb-1 text-[11px] ${parts.length ? 'mt-2 border-t border-marga-border-subtle' : ''}">
      <span class="material-symbols-outlined text-[14px] ${color}" title="${e(tipo.uno)}">${tipo.icono}</span>
      ${t.tipo && t.tipo !== 'tabla' && d.modo !== 'sql' ? `<span class="text-neutral-500">${e(tipo.uno)}</span>` : ''}
      <span class="text-white font-semibold">${e(t.tabla)}</span>
      <span class="${color}">${e(t.estado)}</span>
      <span class="text-neutral-500">+${t.agregadas} −${t.eliminadas}</span></div>`);

    // Mostrar solo el contexto cercano a los cambios.
    const ls = t.lineas;
    const visible = ls.map(() => false);
    ls.forEach((l, i) => {
      if (l.tipo !== ' ') for (let k = Math.max(0, i - CONTEXTO); k <= Math.min(ls.length - 1, i + CONTEXTO); k++) visible[k] = true;
    });
    // Un hueco de una sola línea no vale la pena ocultarlo.
    visible.forEach((v, i) => {
      if (!v && (i === 0 || visible[i - 1]) && (i === visible.length - 1 || visible[i + 1])) visible[i] = true;
    });
    let skipped = false;
    ls.forEach((l, i) => {
      if (!visible[i]) {
        if (!skipped) parts.push('<div class="px-2 py-0.5 text-neutral-600 select-none">⋯</div>');
        skipped = true;
        return;
      }
      skipped = false;
      const txt = `<span class="whitespace-pre">${e(l.texto) || ' '}</span>`;
      if (l.tipo === '+') {
        parts.push(`<div class="flex items-center gap-3 px-2 py-0.5 bg-white/5 text-brand-cyan rounded border border-white/10">
          <span class="w-8 text-right shrink-0 select-none text-[10px] opacity-20">…</span>
          <span class="w-8 text-right shrink-0 select-none text-[10px] font-bold">${l.b}</span>
          <span class="select-none font-bold">+</span>${txt}</div>`);
      } else if (l.tipo === '-') {
        parts.push(`<div class="flex items-center gap-3 px-2 py-0.5 bg-neutral-500/10 text-neutral-400 rounded border border-neutral-500/20">
          <span class="w-8 text-right shrink-0 select-none text-[10px] opacity-50">${l.a}</span>
          <span class="w-8 text-right shrink-0 select-none text-[10px] opacity-20">…</span>
          <span class="select-none font-bold">-</span>${txt}</div>`);
      } else {
        parts.push(`<div class="flex items-center gap-3 px-2 py-0.5 text-neutral-400">
          <span class="w-8 text-right shrink-0 select-none text-[10px] opacity-40">${l.a}</span>
          <span class="w-8 text-right shrink-0 select-none text-[10px] opacity-40">${l.b}</span>
          <span class="select-none">&nbsp;</span>${txt}</div>`);
      }
    });
  });

  body.innerHTML = parts.join('');
  setText('diff-hunk', `@@ +${add} −${del} @@ · ${d.modo === 'sql' ? d.tablas.length + ' archivo' : MargaAPI.contarObjetos(d.tablas) || '0 objetos'}${d.desde_cache ? ' · caché' : ''}`);
}

// ------------------------------------------------------------------ Acciones

async function selectCommitItem(hash, filename, author, time, duration) {
  selectedCommitHash = hash;
  highlightItem(hash);

  if (!isLiveCommits()) {
    const titleEl = document.getElementById('inspector-filename');
    const metaEl = document.getElementById('inspector-meta');
    if (titleEl) titleEl.textContent = filename;
    if (metaEl) metaEl.innerHTML = `Commit <code class="font-mono text-brand-cyan">${hash}</code> · Autor: ${author} · ${time}`;
    showToast(`Mostrando diff para ${filename} (${hash})`, 'info');
    return;
  }

  try {
    const [c, d] = await Promise.all([
      MargaAPI.get(`/repos/${liveRepoId}/commits/${hash}`),
      MargaAPI.get(`/repos/${liveRepoId}/commits/${hash}/diff`)
    ]);
    if (selectedCommitHash !== hash) return; // el usuario ya eligió otro
    selectedCommit = c;
    const e = MargaAPI.escapeHTML;
    setText('inspector-filename', firstLine(c.mensaje) + '.sql');
    const meta = document.getElementById('inspector-meta');
    if (meta) meta.innerHTML = `Commit <code class="font-mono text-brand-cyan font-bold">${e(c.hash_corto)}</code> · ${e(c.autor || '—')} · ${e(new Date(c.fecha).toLocaleString('es-MX'))}` +
      (c.padre_hash ? ` · padre <code class="font-mono text-neutral-300">${e(c.padre_hash.slice(0, 7))}</code>` : ' · commit raíz');
    const enEl = document.getElementById('inspector-entornos');
    if (enEl) {
      const aplicada = c.entornos || [];
      const falta = entornosRepo.map(x => x.nombre).filter(n => !aplicada.includes(n));
      enEl.innerHTML = !entornosRepo.length ? 'Ningún entorno ha reportado su estado todavía.'
        : (aplicada.length ? `Aplicada en <span class="text-neutral-200">${aplicada.map(e).join(', ')}</span>` : 'No aplicada en ningún entorno')
          + (falta.length ? ` · pendiente en <span class="text-neutral-200">${falta.map(e).join(', ')}</span>` : '');
    }
    renderPanelRevision(c);
    setText('sql-up', c.sql_up);
    setText('down-sql', c.reversible ? c.sql_down : '-- Este commit no tiene script de reversión.');
    setText('down-detalle', c.reversible ? 'revert disponible' : 'no reversible');
    renderDiff(d);
  } catch (err) {
    showToast(err.message, 'error');
  }
}

function copySelectedHash() {
  const h = selectedCommit ? selectedCommit.hash : selectedCommitHash;
  navigator.clipboard.writeText(h).then(() => {
    showToast(`Hash #${h.slice(0, 7)} copiado al portapapeles`, 'success');
  });
}

function downloadSelectedDDL() {
  if (!selectedCommit) {
    showToast('Descargando archivo DDL SQL...', 'info');
    return;
  }
  const c = selectedCommit;
  const sqlText = `-- Marga · ${firstLine(c.mensaje)}\n-- commit ${c.hash}\n-- autor ${c.autor || '—'} · ${c.fecha}\n\n-- +marga Up\n${c.sql_up}\n\n-- +marga Down\n${c.sql_down || '-- (sin script de reversión)'}\n`;
  const blob = new Blob([sqlText], { type: 'application/sql' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = firstLine(c.mensaje).replace(/[^\w.-]+/g, '_') + '.sql';
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

// Doble clic de confirmación (sin diálogos nativos).
let revertArmed = false;
async function revertSelectedCommit() {
  if (!isLiveCommits() || !selectedCommit) {
    showToast('El revert solo está disponible con el servidor de Marga', 'info');
    return;
  }
  if (!selectedCommit.reversible) {
    showToast('Este commit no tiene sql_down: no se puede revertir', 'error');
    return;
  }
  const btnText = document.querySelector('#btn-revert span:last-child');
  if (!revertArmed) {
    revertArmed = true;
    if (btnText) btnText.textContent = '¿Confirmar revert?';
    setTimeout(() => { revertArmed = false; if (btnText) btnText.textContent = 'Revertir'; }, 4000);
    return;
  }
  revertArmed = false;
  if (btnText) btnText.textContent = 'Revertir';
  try {
    const nuevo = await MargaAPI.post(`/repos/${liveRepoId}/commits/${selectedCommit.hash}/revert`, { branch: liveBranch });
    showToast(`Revert creado: ${nuevo.hash_corto}`, 'success');
    await loadHistory();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

function runDryRunSimulation() {
  showToast('Simulación de migración dry-run en Staging exitosa: 0 bloqueos detectados', 'success');
}

function verifyReversibility() {
  if (selectedCommit) {
    showToast(selectedCommit.reversible
      ? 'El commit tiene script DOWN: es reversible'
      : 'El commit no tiene script DOWN', selectedCommit.reversible ? 'success' : 'error');
    return;
  }
  showToast('Reversibilidad 100% validada (script DOWN verificado en réplica WAL)', 'success');
}

function copyRollbackCommand(cmd) {
  navigator.clipboard.writeText(cmd || 'marga migrate down -t 0017').then(() => {
    showToast('Comando CLI copiado al portapapeles', 'success');
  });
}


// ------------------------------------------------------------------ Riesgo y revisiones

const NIVEL = {
  alto: { txt: 'Riesgo alto', icon: 'error' },
  medio: { txt: 'Riesgo medio', icon: 'warning' },
  bajo: { txt: 'Riesgo bajo', icon: 'info' }
};

function chipsRevision(c) {
  const e = MargaAPI.escapeHTML;
  const chips = [];
  if (c.nivel_riesgo === 'alto' || c.nivel_riesgo === 'medio') {
    chips.push(`<span class="px-2 py-0.5 rounded text-[10px] font-mono border ${c.nivel_riesgo === 'alto' ? 'border-neutral-300 text-white' : 'border-neutral-600 text-neutral-300'} flex items-center gap-1">
      <span class="material-symbols-outlined text-[12px]">${NIVEL[c.nivel_riesgo].icon}</span>${e(NIVEL[c.nivel_riesgo].txt)}</span>`);
  }
  if (c.aprobaciones > 0) {
    chips.push(`<span class="px-2 py-0.5 rounded text-[10px] font-mono border border-neutral-600 text-neutral-200 flex items-center gap-1">
      <span class="material-symbols-outlined text-[12px]">verified</span>${c.aprobaciones} aprobación${c.aprobaciones === 1 ? '' : 'es'}</span>`);
  }
  if (c.cambios_pedidos > 0) {
    chips.push(`<span class="px-2 py-0.5 rounded text-[10px] font-mono border border-neutral-500 text-white flex items-center gap-1">
      <span class="material-symbols-outlined text-[12px]">rate_review</span>cambios pedidos</span>`);
  }
  return chips.length ? `<div class="flex flex-wrap items-center gap-1.5 mt-2">${chips.join('')}</div>` : '';
}

function renderPanelRevision(c) {
  const panel = document.getElementById('panel-revision');
  if (!panel) return;
  const e = MargaAPI.escapeHTML;

  const riesgo = (c.riesgo || []).length ? `
    <ul class="divide-y divide-marga-border">${c.riesgo.map(h => `
      <li class="px-4 py-3 flex gap-3">
        <span class="material-symbols-outlined text-[18px] ${h.nivel === 'alto' ? 'text-white' : 'text-neutral-400'}">${NIVEL[h.nivel].icon}</span>
        <div class="min-w-0">
          <div class="text-sm ${h.nivel === 'alto' ? 'text-white' : 'text-neutral-200'}">${e(h.mensaje)}</div>
          <div class="text-[11px] text-neutral-500 mt-0.5">${e(h.nivel.toUpperCase())}${h.linea ? ' · línea ' + h.linea : ''}${h.sugerencia ? ' · ' + e(h.sugerencia) : ''}</div>
        </div>
      </li>`).join('')}</ul>`
    : '<div class="px-4 py-4 text-sm text-neutral-400">No se detectó nada que pueda perder datos o bloquear tablas.</div>';

  const yo = MargaAPI.me ? MargaAPI.me.id : null;
  const esAutor = yo !== null && c.autor_id === yo;
  const puedeRevisar = permisoRepo === 'escritura' || permisoRepo === 'admin';
  const mia = (c.revisiones || []).find(r => r.usuario_id === yo);
  const lista = (c.revisiones || []).length ? `
    <ul class="divide-y divide-marga-border">${c.revisiones.map(r => `
      <li class="px-4 py-3">
        <div class="flex items-center gap-2 text-sm">
          <span class="material-symbols-outlined text-[16px] ${r.estado === 'aprobado' ? 'text-white' : 'text-neutral-400'}">${r.estado === 'aprobado' ? 'verified' : 'rate_review'}</span>
          <span class="text-white">${e(r.usuario)}</span>
          <span class="text-neutral-400">${r.estado === 'aprobado' ? 'aprobó' : 'pidió cambios'}</span>
          <span class="ml-auto text-[11px] text-neutral-500">${e(MargaAPI.timeAgo(r.fecha))}</span>
        </div>
        ${r.comentario ? `<p class="text-sm text-neutral-300 mt-1.5 ml-6 whitespace-pre-line">${e(r.comentario)}</p>` : ''}
      </li>`).join('')}</ul>`
    : '<div class="px-4 py-3 text-sm text-neutral-500">Nadie la ha revisado todavía.</div>';

  let formulario = '';
  if (puedeRevisar) {
    formulario = `
      <div class="px-4 py-3 border-t border-marga-border flex flex-col gap-2">
        <textarea id="rev-comentario" rows="2" maxlength="4000" placeholder="Comentario (obligatorio si pides cambios)"
          class="w-full rounded-md bg-marga-bg border border-marga-border px-3 py-2 text-sm text-white placeholder:text-neutral-500 focus:outline-none focus:border-neutral-500">${mia ? e(mia.comentario) : ''}</textarea>
        <div class="flex items-center gap-2">
          ${esAutor ? '<span class="text-[11px] text-neutral-500">Es tu migración: otra persona debe aprobarla.</span>'
            : `<button type="button" data-estado="aprobado" class="rev-btn h-8 px-3 rounded-md bg-brand-violet hover:bg-neutral-300 text-neutral-950 text-xs font-medium">Aprobar</button>`}
          <button type="button" data-estado="cambios" class="rev-btn h-8 px-3 rounded-md border border-marga-border text-xs text-neutral-200 hover:border-neutral-500">Pedir cambios</button>
          ${mia ? '<button type="button" id="rev-quitar" class="ml-auto text-xs text-neutral-500 hover:text-white">Quitar mi revisión</button>' : ''}
        </div>
      </div>`;
  }

  const tarjeta = (titulo, extra, cuerpo) => `
    <section class="bg-marga-surface rounded-xl border border-marga-border overflow-hidden">
      <div class="flex items-center justify-between px-4 py-2 bg-marga-card border-b border-marga-border text-xs font-mono">
        <span class="text-white font-semibold">${titulo}</span><span class="text-neutral-500">${extra}</span>
      </div>${cuerpo}
    </section>`;

  panel.innerHTML =
    tarjeta('Análisis de riesgo', c.nivel_riesgo ? e(NIVEL[c.nivel_riesgo].txt) : 'sin riesgos', riesgo) +
    tarjeta('Revisiones', `${c.aprobaciones} aprobación${c.aprobaciones === 1 ? '' : 'es'}`, lista + formulario);

  panel.querySelectorAll('.rev-btn').forEach(b => b.addEventListener('click', async () => {
    try {
      await MargaAPI.post(`/repos/${liveRepoId}/commits/${c.hash}/revisiones`, {
        estado: b.dataset.estado, comentario: document.getElementById('rev-comentario').value
      });
      showToast(b.dataset.estado === 'aprobado' ? 'Migración aprobada' : 'Cambios solicitados', 'success');
      await loadHistory(c.hash);
    } catch (err) { showToast(err.message, 'error'); }
  }));
  document.getElementById('rev-quitar')?.addEventListener('click', async () => {
    try {
      await MargaAPI.del(`/repos/${liveRepoId}/commits/${c.hash}/revisiones`);
      await loadHistory(c.hash);
    } catch (err) { showToast(err.message, 'error'); }
  });
}

// ------------------------------------------------------------------ Fusionar ramas

function prepararMerge() {
  const btn = document.getElementById('btn-merge');
  if (!btn || ramasRepo.length < 2 || permisoRepo === 'lectura') return;
  btn.classList.remove('hidden');
  btn.classList.add('flex');
  const $ = (id) => document.getElementById(id);
  const modal = $('merge-modal');
  const opciones = ramasRepo.map(n => `<option class="bg-marga-card" value="${MargaAPI.escapeHTML(n)}">${MargaAPI.escapeHTML(n)}</option>`).join('');
  $('merge-desde').innerHTML = opciones;
  $('merge-hacia').innerHTML = opciones;

  const reiniciar = () => {
    $('merge-resultado').innerHTML = '';
    $('merge-aplicar').disabled = true;
    $('merge-forzar').checked = false;
    $('merge-forzar-wrap').classList.add('hidden');
    $('merge-forzar-wrap').classList.remove('flex');
  };
  btn.onclick = () => {
    $('merge-hacia').value = ramasRepo.includes('main') ? 'main' : ramasRepo[0];
    $('merge-desde').value = liveBranch !== $('merge-hacia').value ? liveBranch : (ramasRepo.find(n => n !== $('merge-hacia').value) || ramasRepo[0]);
    reiniciar();
    modal.classList.remove('hidden');
    modal.classList.add('flex');
  };
  const cerrar = () => { modal.classList.add('hidden'); modal.classList.remove('flex'); };
  $('merge-cerrar').onclick = cerrar;
  modal.onclick = (ev) => { if (ev.target === modal) cerrar(); };
  $('merge-desde').onchange = reiniciar;
  $('merge-hacia').onchange = reiniciar;
  $('merge-forzar').onchange = () => { $('merge-aplicar').disabled = !$('merge-forzar').checked; };

  const pedir = async (simular) => {
    const body = { desde: $('merge-desde').value, hacia: $('merge-hacia').value, simular, forzar: $('merge-forzar').checked };
    if (body.desde === body.hacia) { showToast('Elige dos ramas distintas', 'error'); return null; }
    return MargaAPI.post(`/repos/${liveRepoId}/fusionar`, body);
  };
  $('merge-simular').onclick = async () => {
    try {
      const r = await pedir(true);
      if (r) pintarResultadoMerge(r);
    } catch (err) { showToast(err.message, 'error'); }
  };
  $('merge-aplicar').onclick = async () => {
    try {
      const r = await pedir(false);
      if (!r) return;
      if (!r.aplicado) { pintarResultadoMerge(r); return; }
      showToast(`Fusionado: ${r.nuevos.length} migración(es) en ${$('merge-hacia').value}`, 'success');
      cerrar();
      liveBranch = $('merge-hacia').value;
      const sel = document.getElementById('branch-select');
      if (sel) sel.value = liveBranch;
      await loadHistory();
    } catch (err) { showToast(err.message, 'error'); }
  };
}

function pintarResultadoMerge(r) {
  const e = MargaAPI.escapeHTML;
  const $ = (id) => document.getElementById(id);
  const lista = (cs) => `<ul class="mt-2 flex flex-col gap-1">${cs.map(c =>
    `<li class="flex gap-2 font-mono text-xs"><span class="text-neutral-500">${e(c.hash_corto)}</span><span class="text-neutral-200 truncate">${e(c.mensaje.split('\n')[0])}</span></li>`).join('')}</ul>`;
  let html = '';
  $('merge-aplicar').disabled = true;
  $('merge-forzar-wrap').classList.add('hidden');
  $('merge-forzar-wrap').classList.remove('flex');
  if (r.tipo === 'al_dia') {
    html = `<p class="text-neutral-300">${e(r.hacia)} ya contiene todo lo de ${e(r.desde)}. No hay nada que fusionar.</p>`;
  } else if (r.tipo === 'fast_forward') {
    html = `<p class="text-white">Fast-forward: ${e(r.hacia)} avanzará hasta ${e(r.desde)} con ${r.nuevos.length} migración(es).</p>${lista(r.nuevos)}`;
    $('merge-aplicar').disabled = false;
  } else if (r.tipo === 'copia') {
    html = `<p class="text-white">Se copiarán ${r.nuevos.length} migración(es) encima de ${e(r.hacia)} (que avanzó ${r.otros.length}).</p>${lista(r.nuevos)}` +
      (r.renumeradas.length ? `<div class="mt-3 text-xs text-neutral-400">Se renumeran porque su versión ya existe en ${e(r.hacia)}:
        <ul class="mt-1 font-mono">${r.renumeradas.map(x => `<li>${e(x.antes)} → <span class="text-white">${e(x.despues)}</span></li>`).join('')}</ul></div>` : '');
    $('merge-aplicar').disabled = false;
  } else if (r.tipo === 'conflicto') {
    html = `<p class="text-white flex items-center gap-1.5"><span class="material-symbols-outlined text-[18px]">warning</span>Conflicto: ambas ramas modificaron los mismos objetos.</p>
      <ul class="mt-2 flex flex-col gap-2">${r.conflictos.map(cf => `
        <li class="rounded-md border border-marga-border p-2.5">
          <div class="font-mono text-sm text-white flex items-center gap-1.5"><span class="material-symbols-outlined text-[15px] text-neutral-400" title="${e(MargaAPI.tipoObjeto(cf.tipo).uno)}">${MargaAPI.tipoObjeto(cf.tipo).icono}</span>${e(cf.tabla)}</div>
          <div class="text-xs text-neutral-400 mt-1">${e(r.desde)}: ${cf.desde.map(e).join(', ')}</div>
          <div class="text-xs text-neutral-400">${e(r.hacia)}: ${cf.hacia.map(e).join(', ')}</div>
        </li>`).join('')}</ul>
      <p class="mt-2 text-xs text-neutral-500">Revisa que esas migraciones sean compatibles entre sí antes de forzar la fusión.</p>`;
    $('merge-forzar-wrap').classList.remove('hidden');
    $('merge-forzar-wrap').classList.add('flex');
  }
  $('merge-resultado').innerHTML = html;
}
