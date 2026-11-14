/**
 * Marga Layout
 * Dibuja la cabecera según dónde está el usuario:
 *  - <body data-scope="workspace">  fuera de un proyecto: Marga / Proyectos
 *  - <body data-scope="project" data-page="resumen|commits|esquema">
 *        dentro de un proyecto: Marga / Proyectos / <Proyecto ▾>  + pestañas del proyecto
 * Requiere <header id="marga-header"> y marga-api.js cargado antes.
 */
(function () {
  const API = window.MargaAPI;
  const header = document.getElementById('marga-header');
  if (!header || !API) return;

  const e = API.escapeHTML;
  const scope = document.body.dataset.scope || 'workspace';
  const page = document.body.dataset.page || '';
  const repoId = scope === 'project' ? API.currentRepoId() : null;

  // Dentro de un proyecto sin proyecto elegido: volver a la lista.
  if (scope === 'project' && API.online && !repoId) {
    location.replace('proyectos.html');
    return;
  }

  const q = repoId ? `?repo=${repoId}` : '';
  const tabs = [
    { id: 'resumen', label: 'Resumen', icon: 'space_dashboard', href: 'code.html' + q },
    { id: 'commits', label: 'Commits', icon: 'commit', href: 'commits.html' + q },
    { id: 'entornos', label: 'Entornos', icon: 'dns', href: 'entornos.html' + q },
    { id: 'esquema', label: 'Esquema', icon: 'account_tree', href: 'esquema.html' + q },
    { id: 'ajustes', label: 'Ajustes', icon: 'settings', href: `proyectos.html?editar=${repoId}` }
  ];

  const sep = '<span class="text-neutral-700 select-none">/</span>';
  const brand = `
    <a href="proyectos.html" class="shrink-0 text-[17px] font-semibold text-white tracking-tight hover:opacity-80 transition-opacity">Marga</a>`;

  const crumbs = scope === 'project'
    ? `${sep}
       <a href="proyectos.html" class="text-sm text-neutral-400 hover:text-white transition-colors">Proyectos</a>
       ${sep}
       <div class="relative min-w-0">
         <button type="button" id="project-switcher" class="flex items-center gap-1 max-w-[260px] px-2 py-1 -mx-2 rounded-md text-sm font-semibold text-white hover:bg-marga-card transition-colors">
           <span id="project-name" class="truncate">${API.online ? 'Cargando…' : 'Proyecto demo'}</span>
           <span class="material-symbols-outlined text-[16px] text-neutral-500">unfold_more</span>
         </button>
         <div id="project-menu" class="hidden absolute left-0 top-9 z-[70] w-72 rounded-lg bg-marga-surface border border-marga-border shadow-2xl p-1"></div>
       </div>
       <span id="project-env" class="hidden sm:inline px-2 py-0.5 rounded border border-marga-border text-[10px] font-mono text-neutral-400"></span>`
    : page === 'cuenta'
      ? `${sep}<span class="text-sm font-medium text-white">Cuenta</span>`
      : `${sep}<span class="text-sm font-medium text-white">Proyectos</span>`;

  const nav = scope === 'project' ? `
    <nav class="h-10 px-5 lg:px-8 flex items-end gap-1 text-[13px] border-b border-marga-border overflow-x-auto">
      ${tabs.map(t => `
        <a href="${t.href}" data-tab="${t.id}" class="flex items-center gap-1.5 px-3 pb-2.5 pt-1 border-b-2 whitespace-nowrap transition-colors ${t.id === page
          ? 'border-white text-white font-medium'
          : 'border-transparent text-neutral-400 hover:text-neutral-100'}">
          <span class="material-symbols-outlined text-[16px]">${t.icon}</span>${t.label}
        </a>`).join('')}
    </nav>` : '';

  header.className = 'sticky top-0 z-50 shrink-0 bg-marga-bg/95 backdrop-blur select-none';
  header.innerHTML = `
    <div class="h-16 px-5 lg:px-8 flex items-center justify-between gap-4 ${scope === 'project' ? '' : 'border-b border-marga-border'}">
      <div class="flex items-center gap-3 min-w-0">${brand}${crumbs}</div>
      <div class="flex items-center gap-3 shrink-0">
        <button type="button" onclick="openCommandPalette()" class="hidden md:flex items-center gap-2 h-8 w-60 px-3 rounded-md bg-marga-surface border border-marga-border text-xs text-neutral-500 hover:border-neutral-600 transition-colors">
          <span class="material-symbols-outlined text-[16px]">search</span>
          <span class="flex-1 text-left">Ir a…</span>
          <kbd class="font-mono text-[10px] text-neutral-500">Ctrl K</kbd>
        </button>
        <div class="w-8 h-8 rounded-full bg-marga-card border border-marga-border flex items-center justify-center cursor-pointer hover:border-neutral-500 transition-colors">
          <span class="text-[11px] font-semibold text-neutral-200" id="user-initials">··</span>
        </div>
      </div>
    </div>
    ${nav}`;

  // Paleta de comandos (Ctrl K) con rutas acordes al contexto.
  if (typeof navigationRoutes !== 'undefined') {
    navigationRoutes.length = 0;
    navigationRoutes.push({ name: 'Todos los proyectos', desc: 'Lista de proyectos del espacio de trabajo', path: 'proyectos.html', icon: 'folder' });
    navigationRoutes.push({ name: 'Cuenta y tokens', desc: 'Tus datos y tokens de acceso para la CLI', path: 'cuenta.html', icon: 'key' });
    if (scope === 'project') {
      tabs.slice(0, 4).forEach(t => navigationRoutes.push({ name: t.label, desc: 'Proyecto actual', path: t.href, icon: t.icon }));
    }
  }

  // ---------------------------------------------------------------- datos del proyecto
  if (scope !== 'project' || !API.online) return;

  let reposCache = null;
  const loadRepos = async () => reposCache || (reposCache = await API.get('/repos'));

  API.ready.then(async (u) => {
    if (!u) return;
    try {
      const repo = await API.get('/repos/' + repoId);
      API.currentRepo = repo;
      API.setCurrentRepo(repo.id);
      document.getElementById('project-name').textContent = repo.nombre;
      const env = document.getElementById('project-env');
      env.textContent = repo.entorno;
      if (repo.permiso !== 'admin') header.querySelector('[data-tab="ajustes"]')?.remove();
      const label = { resumen: 'Resumen', commits: 'Commits', entornos: 'Entornos', esquema: 'Esquema' }[page] || '';
      document.title = `${label ? label + ' · ' : ''}${repo.nombre} — Marga`;
      document.dispatchEvent(new CustomEvent('marga:repo', { detail: repo }));

      // Accesos directos a otros proyectos en la paleta.
      (await loadRepos()).filter(r => r.id !== repo.id).forEach(r =>
        navigationRoutes.push({ name: r.nombre, desc: 'Abrir proyecto', path: `code.html?repo=${r.id}`, icon: 'database' }));
    } catch (err) {
      if (err.status === 404) { location.replace('proyectos.html'); return; }
      showToast(err.message, 'error');
    }
  });

  // Selector de proyecto: misma pestaña, otro proyecto.
  const btn = document.getElementById('project-switcher');
  const menu = document.getElementById('project-menu');
  btn.addEventListener('click', async (ev) => {
    ev.stopPropagation();
    if (!menu.classList.contains('hidden')) { menu.classList.add('hidden'); return; }
    menu.innerHTML = '<div class="px-3 py-2 text-xs text-neutral-500">Cargando…</div>';
    menu.classList.remove('hidden');
    try {
      const repos = await loadRepos();
      menu.innerHTML = `<div class="px-3 pt-2 pb-1 text-[10px] uppercase tracking-wider text-neutral-500">Cambiar de proyecto</div>` +
        repos.map(r => {
          const target = location.pathname.split('/').pop();
          return `<a href="${target}?repo=${r.id}" class="flex items-center justify-between gap-2 px-3 py-2 rounded-md text-sm hover:bg-marga-card ${r.id === repoId ? 'text-white' : 'text-neutral-300'}">
            <span class="truncate">${e(r.nombre)}</span>
            ${r.id === repoId ? '<span class="material-symbols-outlined text-[16px]">check</span>' : `<span class="text-[10px] font-mono text-neutral-500">${e(r.entorno)}</span>`}
          </a>`;
        }).join('') +
        `<div class="h-px bg-marga-border my-1"></div>
         <a href="proyectos.html" class="flex items-center gap-2 px-3 py-2 rounded-md text-sm text-neutral-400 hover:bg-marga-card hover:text-white">
           <span class="material-symbols-outlined text-[16px]">arrow_back</span>Todos los proyectos</a>`;
    } catch (err) {
      menu.innerHTML = `<div class="px-3 py-2 text-xs text-neutral-400">${e(err.message)}</div>`;
    }
  });
  document.addEventListener('click', () => menu.classList.add('hidden'));
})();
