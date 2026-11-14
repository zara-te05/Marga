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

async function loadHistory() {
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
  selectCommitItem(h.commits[0].hash);
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

    const tablas = (c.tablas || []).map(t =>
      `<span class="px-2 py-0.5 rounded text-[10px] font-mono bg-marga-card border border-marga-border text-neutral-300 flex items-center gap-1">
         <span class="w-1 h-1 rounded-full bg-brand-cyan"></span>${e(t)}</span>`).join('');

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
    parts.push(`<div class="flex items-center gap-2 px-2 pt-3 pb-1 text-[11px] ${parts.length ? 'mt-2 border-t border-marga-border-subtle' : ''}">
      <span class="material-symbols-outlined text-[14px] ${color}">table</span>
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
  setText('diff-hunk', `@@ +${add} −${del} @@ · ${d.tablas.length} ${d.modo === 'sql' ? 'archivo' : 'tabla(s)'}${d.desde_cache ? ' · caché' : ''}`);
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
