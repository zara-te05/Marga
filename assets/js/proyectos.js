/**
 * Marga Projects Management Engine
 * Handles project creation/editing drawer, deletion guards, filtering, and query inspector modal.
 *
 * Modo en vivo: si la página se sirve desde el backend (MargaAPI.online) las tarjetas se
 * generan con /api/repos y crear / editar / eliminar llaman a la API. Sin backend se
 * conserva el contenido de demostración.
 */

let currentTargetProject = "FinTech Core Engine";
let currentTargetId = null;          // id del repositorio en edición (modo en vivo)
const liveRepos = new Map();         // id -> repositorio de la API

// ------------------------------------------------------------------ Modo en vivo

const ENV_BADGE = {
  'Producción': 'bg-white/5 text-brand-cyan border-white/10',
  'Staging': 'bg-white/5 text-brand-purple border-white/10',
  'Desarrollo': 'bg-white/5 text-brand-sky border-white/10',
  'Sandbox': 'bg-neutral-500/10 text-neutral-300 border-neutral-500/30'
};

function isLive() {
  return window.MargaAPI && MargaAPI.online && MargaAPI.me;
}

async function loadProjects() {
  const repos = await MargaAPI.get('/repos');
  liveRepos.clear();
  repos.forEach(r => liveRepos.set(r.id, r));

  const grid = document.getElementById('projects-grid');
  if (grid) {
    grid.innerHTML = repos.length
      ? repos.map(renderProjectCard).join('')
      : `<div class="col-span-full p-10 rounded-2xl border border-dashed border-marga-border text-center flex flex-col items-center gap-3">
           <span class="material-symbols-outlined text-[32px] text-brand-purple">add_box</span>
           <p class="text-sm text-neutral-300">Aún no tienes proyectos.</p>
           <button type="button" onclick="openCreateDrawer()" class="px-4 py-2 rounded-lg bg-brand-violet hover:bg-neutral-300 text-neutral-950 text-xs font-semibold">Crear el primero</button>
         </div>`;
  }

  const setText = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v; };
  setText('stat-repos', repos.length);
  setText('stat-commits', repos.reduce((n, r) => n + r.total_commits, 0));
  setText('stat-branches', repos.reduce((n, r) => n + r.total_branches, 0));
  setText('stat-shared', repos.filter(r => r.propietario_id !== MargaAPI.me.id).length);

  // Desde la pestaña "Ajustes" de un proyecto: proyectos.html?editar=ID
  const editar = Number(new URLSearchParams(location.search).get('editar'));
  if (editar && liveRepos.has(editar)) {
    history.replaceState(null, '', 'proyectos.html');
    openEditDrawerById(editar);
  }
}

function renderProjectCard(r) {
  const e = MargaAPI.escapeHTML;
  const last = r.ultimo_commit;
  const canAdmin = r.permiso === 'admin';
  const motor = MargaAPI.MOTORES[r.motor] || r.motor;
  const badge = ENV_BADGE[r.entorno] || ENV_BADGE.Sandbox;

  return `
  <article class="project-card group relative flex flex-col justify-between p-6 rounded-2xl bg-marga-surface/90 border border-marga-border hover:border-white/20 hover:bg-marga-card transition-all shadow-lg hover:shadow-xl" data-env="${e(r.entorno)}" data-name="${e(r.nombre)}" data-slug="${e(r.slug)}" data-id="${r.id}">
    <div class="flex flex-col gap-4">
      <div class="flex items-start justify-between gap-3">
        <div class="flex items-center gap-3 min-w-0">
          <div class="w-10 h-10 rounded-xl bg-white/10 border border-white/10 flex items-center justify-center text-brand-purple shrink-0">
            <span class="material-symbols-outlined text-[22px]">database</span>
          </div>
          <div class="min-w-0">
            <h2 class="text-base font-semibold text-neutral-100 group-hover:text-brand-purple transition-colors truncate">${e(r.nombre)}</h2>
            <span class="font-mono text-xs text-neutral-400">${e(r.slug)}</span>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span class="shrink-0 px-2.5 py-1 rounded-full font-mono text-[10px] border ${badge}">${e(r.entorno)}</span>
          ${canAdmin ? `<button class="p-1 rounded-lg text-neutral-400 hover:text-white hover:bg-marga-surface transition-colors" onclick="toggleContextMenu('menu-r${r.id}')" type="button">
            <span class="material-symbols-outlined text-[18px]">more_vert</span>
          </button>` : ''}
        </div>
      </div>

      <p class="text-xs text-neutral-400 line-clamp-2 leading-relaxed">${e(r.descripcion) || '<span class="italic text-neutral-500">Sin descripción</span>'}</p>

      <div class="flex flex-col gap-1.5 pt-1">
        <span class="font-mono text-[10px] text-neutral-400 uppercase tracking-wider">${r.total_commits} commits · ${r.total_branches} ramas</span>
        <div class="flex flex-wrap gap-1.5">
          <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-marga-bg border border-marga-border-subtle font-mono text-xs text-neutral-300">
            <span class="w-1.5 h-1.5 rounded-full bg-brand-cyan"></span>${e(motor)}
          </span>
          <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-marga-bg border border-marga-border-subtle font-mono text-xs text-neutral-300">
            <span class="material-symbols-outlined text-[13px] text-neutral-400">${r.visibilidad === 'publico' ? 'public' : 'lock'}</span>${r.visibilidad === 'publico' ? 'público' : 'privado'}
          </span>
          <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-marga-bg border border-marga-border-subtle font-mono text-xs text-neutral-300">
            <span class="w-1.5 h-1.5 rounded-full bg-brand-purple"></span>${e(r.permiso)}
          </span>
        </div>
      </div>

      <div class="flex items-center justify-between gap-2 p-2.5 rounded-xl bg-marga-bg border border-marga-border-subtle font-mono text-xs">
        <div class="flex items-center gap-1.5 text-neutral-200 min-w-0">
          <span class="material-symbols-outlined text-[14px] text-brand-purple">call_split</span>
          <span class="text-white font-medium">main</span>
          <span class="text-neutral-400">${last ? '#' + e(last.hash_corto) : 'sin commits'}</span>
        </div>
        <span class="text-neutral-400 truncate">${last ? `${e(MargaAPI.timeAgo(last.fecha))} · ${e(last.autor || '—')}` : `por ${e(r.propietario)}`}</span>
      </div>
    </div>

    <div class="flex items-center justify-between gap-2 pt-4 mt-2 border-t border-marga-border-subtle">
      ${canAdmin ? `<button class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-marga-surface hover:bg-marga-border text-neutral-200 text-xs transition-colors border border-marga-border" onclick="openEditDrawerById(${r.id})" type="button">
        <span class="material-symbols-outlined text-[16px] text-neutral-400">tune</span>
        <span>Configurar</span>
      </button>` : `<span class="text-[11px] font-mono text-neutral-500">propietario: ${e(r.propietario)}</span>`}
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg bg-white/10 hover:bg-brand-violet text-brand-purple hover:text-neutral-950 text-xs font-semibold border border-white/10 transition-all" onclick="quickLaunch(${r.id})" type="button">
        <span>Abrir</span>
        <span class="material-symbols-outlined text-[16px]">arrow_forward</span>
      </button>
    </div>

    ${canAdmin ? `<div class="hidden absolute top-12 right-4 z-20 w-44 rounded-xl bg-marga-surface border border-marga-border p-1.5 shadow-2xl flex flex-col gap-0.5" id="menu-r${r.id}">
      <button class="flex items-center gap-2 w-full px-2.5 py-1.5 rounded text-left text-xs text-neutral-200 hover:bg-marga-card transition-colors" onclick="openEditDrawerById(${r.id})" type="button">
        <span class="material-symbols-outlined text-[15px] text-brand-sky">edit</span><span>Editar ajustes</span>
      </button>
      <div class="h-px bg-marga-border my-1"></div>
      <button class="flex items-center gap-2 w-full px-2.5 py-1.5 rounded text-left text-xs text-neutral-400 hover:bg-neutral-500/20 transition-colors" onclick="confirmDeleteById(${r.id})" type="button">
        <span class="material-symbols-outlined text-[15px] text-neutral-400">delete</span><span>Eliminar</span>
      </button>
    </div>` : ''}
  </article>`;
}

function openEditDrawerById(id) {
  const r = liveRepos.get(id);
  if (!r) return;
  openEditDrawer(r.nombre, r.slug, r.descripcion, r.entorno, id);
  setField('form-motor', r.motor);
  setField('form-vis', r.visibilidad);
}

function setField(id, v) { const el = document.getElementById(id); if (el) el.value = v; }

function confirmDeleteById(id) {
  const r = liveRepos.get(id);
  if (!r) return;
  currentTargetId = id;
  confirmDelete(r.nombre);
}

document.addEventListener('DOMContentLoaded', () => {
  if (!window.MargaAPI || !MargaAPI.online) return;
  MargaAPI.ready.then(u => {
    if (u) loadProjects().catch(err => showToast(err.message, 'error'));
  });
});

// ------------------------------------------------------------------ Drawer

function openCreateDrawer() {
  currentTargetId = null;
  document.getElementById('drawer-title').innerText = "Crear Nuevo Proyecto";
  document.getElementById('drawer-subtitle').innerText = "Configura los datos base de tu nuevo espacio";
  document.getElementById('drawer-icon').innerText = "add_box";
  document.getElementById('form-name').value = "";
  document.getElementById('form-slug').value = "";
  document.getElementById('form-desc').value = "";
  setField('form-env', 'Desarrollo');
  setField('form-motor', 'postgresql');
  setField('form-vis', 'privado');
  document.getElementById('drawer-save-text').innerText = "Crear proyecto";

  switchDrawerTab('general', null);

  document.getElementById('drawer-backdrop').classList.remove('hidden');
  document.getElementById('project-drawer').classList.remove('translate-x-full');
}

function openEditDrawer(name, slug, desc, env, id) {
  currentTargetProject = name;
  currentTargetId = id ?? null;
  document.getElementById('drawer-title').innerText = "Editar Proyecto: " + name;
  document.getElementById('drawer-subtitle').innerText = "Modifica la configuración de ramas y cluster";
  document.getElementById('drawer-icon').innerText = "tune";
  document.getElementById('form-name').value = name;
  document.getElementById('form-slug').value = slug;
  document.getElementById('form-desc').value = desc;
  document.getElementById('form-env').value = env;
  document.getElementById('drawer-save-text').innerText = "Guardar Cambios";

  switchDrawerTab('general', null);

  document.getElementById('drawer-backdrop').classList.remove('hidden');
  document.getElementById('project-drawer').classList.remove('translate-x-full');
}

function closeAllDrawers() {
  const backdrop = document.getElementById('drawer-backdrop');
  const drawer = document.getElementById('project-drawer');
  if (backdrop) backdrop.classList.add('hidden');
  if (drawer) drawer.classList.add('translate-x-full');
  closeAllMenus();
}

function syncSlug(val) {
  // El slug es inmutable una vez creado el proyecto.
  if (currentTargetId) return;
  const slug = val.toLowerCase().trim()
    .normalize('NFD').replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
  const slugInput = document.getElementById('form-slug');
  if (slugInput) slugInput.value = slug;
}

function switchDrawerTab(tabName, clickedBtn) {
  document.getElementById('tab-general')?.classList.add('hidden');
  document.getElementById('tab-databases')?.classList.add('hidden');
  document.getElementById('tab-danger')?.classList.add('hidden');
  document.getElementById('tab-' + tabName)?.classList.remove('hidden');

  if (clickedBtn) {
    const parent = clickedBtn.parentElement;
    parent.querySelectorAll('button').forEach(b => {
      b.className = "py-2.5 px-3 font-medium text-xs text-neutral-400 hover:text-neutral-200 rounded-t-lg transition-colors";
    });
    clickedBtn.className = "py-2.5 px-3 font-semibold text-xs text-brand-purple bg-marga-card rounded-t-lg border-b-2 border-brand-violet";
  }
}

async function saveProjectChanges() {
  const name = document.getElementById('form-name').value.trim();
  if (!name) {
    showToast('Por favor introduce un nombre para el proyecto', 'error');
    return;
  }

  if (isLive()) {
    const body = {
      nombre: name,
      descripcion: document.getElementById('form-desc').value.trim(),
      entorno: document.getElementById('form-env').value,
      motor: document.getElementById('form-motor')?.value,
      visibilidad: document.getElementById('form-vis')?.value
    };
    try {
      if (currentTargetId) {
        await MargaAPI.patch('/repos/' + currentTargetId, body);
      } else {
        body.slug = document.getElementById('form-slug').value.trim();
        await MargaAPI.post('/repos', body);
      }
      showToast(`Proyecto '${name}' guardado correctamente`, 'success');
      closeAllDrawers();
      await loadProjects();
    } catch (err) {
      showToast(err.message, 'error');
    }
    return;
  }

  showToast(`Proyecto '${name}' guardado correctamente`, 'success');
  closeAllDrawers();
}

function confirmDelete(projectName) {
  currentTargetProject = projectName;
  document.getElementById('delete-target-name').innerText = projectName;
  document.getElementById('delete-confirm-input').value = "";
  validateDeleteInput("");
  document.getElementById('delete-modal').classList.remove('hidden');
  closeAllMenus();
}

function triggerDeleteFromDrawer() {
  confirmDelete(currentTargetProject);
}

function closeDeleteModal() {
  document.getElementById('delete-modal').classList.add('hidden');
}

function validateDeleteInput(val) {
  const btn = document.getElementById('delete-execute-btn');
  if (val.trim().toUpperCase() === "ELIMINAR") {
    btn.disabled = false;
    btn.className = "px-4 py-2 rounded-lg bg-neutral-100 hover:bg-neutral-200 text-neutral-950 font-medium text-xs transition-all cursor-pointer shadow-md";
  } else {
    btn.disabled = true;
    btn.className = "px-4 py-2 rounded-lg bg-neutral-900/40 text-neutral-300/60 font-medium text-xs cursor-not-allowed transition-all";
  }
}

async function executeProjectDeletion() {
  if (isLive() && currentTargetId) {
    try {
      await MargaAPI.del('/repos/' + currentTargetId);
      showToast(`Proyecto '${currentTargetProject}' eliminado`, 'error');
      closeDeleteModal();
      closeAllDrawers();
      currentTargetId = null;
      await loadProjects();
    } catch (err) {
      showToast(err.message, 'error');
    }
    return;
  }
  showToast(`Proyecto '${currentTargetProject}' eliminado de tu workspace`, 'error');
  closeDeleteModal();
  closeAllDrawers();
}

function toggleContextMenu(menuId) {
  const menu = document.getElementById(menuId);
  const isHidden = menu.classList.contains('hidden');
  closeAllMenus();
  if (isHidden) {
    menu.classList.remove('hidden');
  }
}

function closeAllMenus() {
  const menus = document.querySelectorAll('[id^="menu-"]');
  menus.forEach(m => m.classList.add('hidden'));
}

function filterProjects(searchVal) {
  const query = searchVal.toLowerCase();
  const cards = document.querySelectorAll('.project-card');
  cards.forEach(card => {
    const name = card.getAttribute('data-name')?.toLowerCase() || '';
    const slug = card.getAttribute('data-slug')?.toLowerCase() || '';
    if (name.includes(query) || slug.includes(query)) {
      card.classList.remove('hidden');
    } else {
      card.classList.add('hidden');
    }
  });
}

function filterByEnvironment(env, btn) {
  const cards = document.querySelectorAll('.project-card');
  cards.forEach(card => {
    const cardEnv = card.getAttribute('data-env');
    if (env === 'all' || cardEnv === env) {
      card.classList.remove('hidden');
    } else {
      card.classList.add('hidden');
    }
  });

  if (btn) {
    const parent = btn.parentElement;
    parent.querySelectorAll('button').forEach(b => {
      b.className = "px-3 py-1 rounded text-xs font-medium text-neutral-400 hover:text-neutral-100";
    });
    btn.className = "px-3 py-1 rounded text-xs font-medium text-neutral-950 bg-brand-violet";
  }
}

// En vivo recibe el id del repositorio y abre su historial; en demo, el explorador.
function quickLaunch(idOrName) {
  if (typeof idOrName === 'number') {
    MargaAPI.setCurrentRepo(idOrName);
    window.location.href = "code.html?repo=" + idOrName;
    return;
  }
  window.location.href = "code.html";
}

function duplicateProject(name) {
  showToast(`Clon del esquema estructural '${name}' creado`, 'success');
  closeAllMenus();
}

function archiveProject(name) {
  showToast(`Proyecto '${name}' archivado`, 'info');
  closeAllMenus();
}

function openModalDetails(hash, title, author, tag) {
  const modal = document.getElementById('inspector-modal');
  if (!modal) return;
  document.getElementById('modal-commit-title').innerText = title;
  document.getElementById('modal-commit-hash').innerText = hash;
  document.getElementById('modal-commit-author').innerText = author;
  document.getElementById('modal-commit-tag').innerText = tag;
  modal.classList.remove('hidden');
}

function closeModal() {
  const modal = document.getElementById('inspector-modal');
  if (modal) modal.classList.add('hidden');
}

function copySqlQuery() {
  navigator.clipboard.writeText("CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trx_user_created ON transactions (user_id, created_at DESC);").then(() => {
    showToast('Script SQL DDL copiado al portapapeles', 'success');
  });
}

function triggerRollbackAlert() {
  showToast('Comando DOWN enviado con éxito. WAL replay programado sin downtime.', 'info');
  closeModal();
}

// Global click outside listener
document.addEventListener('click', function (e) {
  if (!e.target.closest('.project-card') && !e.target.closest('[id^="menu-"]')) {
    closeAllMenus();
  }
});
