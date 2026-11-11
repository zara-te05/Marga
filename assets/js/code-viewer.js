/**
 * Marga Code & Schemas DDL Explorer
 * Handles tab switching, tree/graph toggle, file selection, and migration scaffolding
 */

function switchRepoTab(tabId) {
  const tabs = ['files', 'migrations', 'graph', 'prs'];
  tabs.forEach(t => {
    const el = document.getElementById('tab-' + t);
    if (el) {
      if (t === tabId) {
        el.className = 'tab-btn flex items-center gap-2 px-3 py-2 rounded-lg bg-brand-violet text-neutral-950 text-xs font-semibold shadow-sm transition-colors';
      } else {
        el.className = 'tab-btn flex items-center gap-2 px-3 py-2 rounded-lg bg-transparent hover:bg-marga-card text-neutral-400 hover:text-neutral-200 text-xs transition-colors';
      }
    }
  });

  if (tabId === 'graph') {
    window.location.href = 'visualizador-sql.html';
  } else if (tabId === 'migrations') {
    window.location.href = 'commits.html';
  }
}

function setViewMode(mode) {
  const treeView = document.getElementById('repo-tree-view');
  const graphView = document.getElementById('repo-graph-view');
  const btnTree = document.getElementById('btn-view-tree');
  const btnGraph = document.getElementById('btn-view-graph');

  if (mode === 'graph') {
    window.location.href = 'visualizador-sql.html';
  } else {
    if (treeView) treeView.classList.remove('hidden');
    if (btnTree) btnTree.className = 'px-2.5 py-1 rounded text-xs font-medium bg-brand-violet text-neutral-950 transition-all flex items-center gap-1.5';
    if (btnGraph) btnGraph.className = 'px-2.5 py-1 rounded text-xs font-medium text-neutral-400 hover:text-neutral-200 transition-all flex items-center gap-1.5';
  }
}

function promptNewMigration() {
  const name = prompt("Nombre descriptivo para la migración DDL (snake_case):", "create_audit_retention_policy");
  if (name) {
    if (typeof showToast === 'function') {
      showToast(`Migración 'migrations/${Date.now()}_${name}.sql' generada con éxito`, 'success');
    } else {
      alert("Migración 'migrations/" + Date.now() + "_" + name + ".sql' generada.");
    }
  }
}

function copyFileContent() {
  const ddl = `-- Marga Schema DDL
CREATE TABLE public.transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id UUID NOT NULL REFERENCES public.wallets(id) ON DELETE CASCADE,
    amount NUMERIC(18, 4) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED')),
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`;
  navigator.clipboard.writeText(ddl).then(() => {
    if (typeof showToast === 'function') {
      showToast('DDL de tabla copiado al portapapeles', 'success');
    }
  });
}
