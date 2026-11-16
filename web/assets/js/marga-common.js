/**
 * Marga Common JavaScript Suite
 * - Theme Switcher (Dark / Light with localStorage persistence)
 * - Command Palette (⌘K / Ctrl+K quick navigator)
 * - Non-blocking Toast notification system
 */

(function () {
  // 1. Initialize Theme
  const savedTheme = localStorage.getItem('marga_theme') || 'dark';
  applyTheme(savedTheme === 'dark');

  // 2. Setup listeners when DOM is loaded
  document.addEventListener('DOMContentLoaded', () => {
    setupThemeButtons();
    setupCommandPalette();
    setupSearchTriggers();
  });
})();

/**
 * Applies dark or light theme to document
 */
function applyTheme(isDark) {
  if (isDark) {
    document.documentElement.classList.add('dark');
    localStorage.setItem('marga_theme', 'dark');
  } else {
    document.documentElement.classList.remove('dark');
    localStorage.setItem('marga_theme', 'light');
  }
  updateThemeButtonStyles(isDark);
}

/**
 * Toggles theme triggered by button clicks
 */
function toggleTheme(isDark) {
  applyTheme(isDark);
  showToast(isDark ? 'Modo oscuro activado' : 'Modo claro activado (optimizado para Marga)', 'info');
}

/**
 * Updates visual state of header theme toggle buttons
 */
function updateThemeButtonStyles(isDark) {
  const darkBtn = document.getElementById('theme-dark-btn');
  const lightBtn = document.getElementById('theme-light-btn');
  if (!darkBtn || !lightBtn) return;

  if (isDark) {
    darkBtn.className = 'p-1 rounded bg-brand-violet text-neutral-950 shadow-sm transition-colors';
    lightBtn.className = 'p-1 rounded text-neutral-400 hover:text-neutral-200 hover:bg-marga-card transition-colors';
  } else {
    lightBtn.className = 'p-1 rounded bg-brand-violet text-neutral-950 shadow-sm transition-colors';
    darkBtn.className = 'p-1 rounded text-neutral-400 hover:text-neutral-600 hover:bg-neutral-200 transition-colors';
  }
}

function setupThemeButtons() {
  const isDark = document.documentElement.classList.contains('dark');
  updateThemeButtonStyles(isDark);
}

/**
 * Toast Notification System
 */
function showToast(message, type = 'info') {
  let toastContainer = document.getElementById('marga-toast-container');
  if (!toastContainer) {
    toastContainer = document.createElement('div');
    toastContainer.id = 'marga-toast-container';
    toastContainer.className = 'fixed bottom-5 right-5 z-[100] flex flex-col gap-2 pointer-events-none';
    document.body.appendChild(toastContainer);
  }

  const toast = document.createElement('div');
  toast.className = 'pointer-events-auto px-4 py-2.5 rounded-lg bg-marga-card border border-white/20 text-neutral-200 text-xs font-mono shadow-2xl flex items-center gap-2.5 transform transition-all duration-300 opacity-0 translate-y-3';
  
  let icon = 'info';
  let iconColor = 'text-brand-purple';
  if (type === 'success') {
    icon = 'check_circle';
    iconColor = 'text-brand-cyan';
  } else if (type === 'error') {
    icon = 'error';
    iconColor = 'text-neutral-400';
  }

  toast.innerHTML = `
    <span class="material-symbols-outlined text-[17px] ${iconColor}">${icon}</span>
    <span>${message}</span>
  `;

  toastContainer.appendChild(toast);

  // Trigger animation
  requestAnimationFrame(() => {
    toast.classList.remove('opacity-0', 'translate-y-3');
    toast.classList.add('opacity-100', 'translate-y-0');
  });

  setTimeout(() => {
    toast.classList.remove('opacity-100', 'translate-y-0');
    toast.classList.add('opacity-0', 'translate-y-3');
    setTimeout(() => toast.remove(), 300);
  }, 2800);
}

/**
 * Command Palette (⌘K) Global Navigation
 */
const navigationRoutes = [
  { name: 'Dashboard de Proyectos', desc: 'Resumen de clusters, réplicas y estado de drift', path: 'proyectos.html', icon: 'dataset' },
  { name: 'Código & Esquemas DDL', desc: 'Explorador de archivos, tablas y DDL versionado', path: 'code.html', icon: 'code' },
  { name: 'Commits & Migraciones', desc: 'Historial de migraciones, hashes, AST diff y reversiones', path: 'commits.html', icon: 'conversion_path' },
  { name: 'Cuenta y tokens', desc: 'Tus datos y tokens de acceso para la CLI', path: 'cuenta.html', icon: 'key' }
];

function setupCommandPalette() {
  if (document.getElementById('command-palette-modal')) return;

  const modal = document.createElement('div');
  modal.id = 'command-palette-modal';
  modal.className = 'fixed inset-0 z-[90] hidden flex items-start justify-center pt-24 px-4 bg-black/60 backdrop-blur-sm';
  modal.innerHTML = `
    <div class="w-full max-w-xl bg-marga-surface border border-marga-border rounded-xl shadow-2xl overflow-hidden flex flex-col text-neutral-200">
      <div class="flex items-center px-4 py-3 border-b border-marga-border gap-3">
        <span class="material-symbols-outlined text-[20px] text-brand-purple">search</span>
        <input id="palette-search-input" type="text" class="w-full bg-transparent text-sm font-sans placeholder:text-neutral-500 focus:outline-none text-neutral-100" placeholder="Buscar pantalla, tabla, commit o migración... (Escribe para filtrar)">
        <kbd class="px-1.5 py-0.5 rounded bg-marga-card border border-marga-border text-[10px] font-mono text-neutral-400">ESC</kbd>
      </div>
      <div id="palette-results-list" class="max-h-72 overflow-y-auto p-2 space-y-1">
        <!-- Rendered items -->
      </div>
      <div class="px-4 py-2 border-t border-marga-border-subtle bg-marga-card/50 flex items-center justify-between text-[11px] font-mono text-neutral-400">
        <span>Usa ↑ ↓ para navegar, Enter para seleccionar</span>
        <span>Marga Engine v2.4</span>
      </div>
    </div>
  `;
  document.body.appendChild(modal);

  modal.addEventListener('click', (e) => {
    if (e.target === modal) closeCommandPalette();
  });

  const searchInput = document.getElementById('palette-search-input');
  searchInput.addEventListener('input', (e) => renderPaletteResults(e.target.value));

  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      toggleCommandPalette();
    }
    if (e.key === 'Escape') {
      closeCommandPalette();
    }
  });

  renderPaletteResults('');
}

function renderPaletteResults(filter) {
  const list = document.getElementById('palette-results-list');
  if (!list) return;

  const q = filter.trim().toLowerCase();
  const filtered = navigationRoutes.filter(r => r.name.toLowerCase().includes(q) || r.desc.toLowerCase().includes(q));

  if (filtered.length === 0) {
    list.innerHTML = `<div class="p-4 text-center text-xs text-neutral-500">No se encontraron resultados para "${filter}".</div>`;
    return;
  }

  list.innerHTML = filtered.map((item, idx) => `
    <a href="${item.path}" class="flex items-center justify-between px-3 py-2.5 rounded-lg hover:bg-marga-card transition-colors group cursor-pointer text-left">
      <div class="flex items-center gap-3">
        <div class="w-7 h-7 rounded-md bg-marga-card border border-marga-border flex items-center justify-center text-brand-purple group-hover:border-brand-violet transition-colors">
          <span class="material-symbols-outlined text-[16px]">${item.icon}</span>
        </div>
        <div>
          <div class="text-xs font-semibold text-neutral-200 group-hover:text-white">${item.name}</div>
          <div class="text-[11px] text-neutral-400 font-sans">${item.desc}</div>
        </div>
      </div>
      <span class="material-symbols-outlined text-[14px] text-neutral-500 group-hover:text-brand-purple transition-colors">arrow_forward</span>
    </a>
  `).join('');
}

function openCommandPalette() {
  const modal = document.getElementById('command-palette-modal');
  if (modal) {
    modal.classList.remove('hidden');
    const input = document.getElementById('palette-search-input');
    if (input) {
      input.value = '';
      renderPaletteResults('');
      setTimeout(() => input.focus(), 50);
    }
  }
}

function closeCommandPalette() {
  const modal = document.getElementById('command-palette-modal');
  if (modal) modal.classList.add('hidden');
}

function toggleCommandPalette() {
  const modal = document.getElementById('command-palette-modal');
  if (modal && modal.classList.contains('hidden')) {
    openCommandPalette();
  } else {
    closeCommandPalette();
  }
}

function setupSearchTriggers() {
  // Bind any search input or trigger to open the command palette
  // Los buscadores marcados con data-live-search consultan la API y no abren la paleta.
  const searchInputs = document.querySelectorAll('input[placeholder*="Buscar"]:not([data-live-search])');
  searchInputs.forEach(input => {
    input.addEventListener('focus', () => {
      openCommandPalette();
      input.blur();
    });
  });
}
