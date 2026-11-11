/**
 * Marga Interactive SQL Schema Graph Engine
 * Handles zoom, pan, table selection, and dynamic inspector updates
 */

let currentZoom = 1.0;
let selectedTableId = 'transactions';

function adjustZoom(delta) {
  currentZoom = Math.min(Math.max(currentZoom + delta, 0.5), 1.8);
  applyZoom();
}

function resetCanvasLayout() {
  currentZoom = 1.0;
  applyZoom();
  
  const users = document.getElementById('card-users');
  const wallets = document.getElementById('card-wallets');
  const trx = document.getElementById('card-transactions');
  const ledger = document.getElementById('card-ledger');

  if (users && wallets && trx && ledger) {
    users.style.left = '40px';
    users.style.top = '40px';
    wallets.style.left = '480px';
    wallets.style.top = '40px';
    trx.style.left = '920px';
    trx.style.top = '40px';
    ledger.style.left = '480px';
    ledger.style.top = '440px';
  }
  if (typeof showToast === 'function') {
    showToast('Posiciones de tablas restablecidas');
  }
}

function fitScreen() {
  currentZoom = 0.9;
  applyZoom();
  const canvasArea = document.getElementById('erd-canvas-area');
  if (canvasArea) {
    canvasArea.scrollTo({ left: 0, top: 0, behavior: 'smooth' });
  }
}

function applyZoom() {
  const plane = document.getElementById('erd-plane');
  const badge = document.getElementById('zoom-badge');
  if (plane) {
    plane.style.transform = `scale(${currentZoom})`;
  }
  if (badge) {
    badge.textContent = Math.round(currentZoom * 100) + '%';
  }
}

function toggleInspector() {
  const inspector = document.getElementById('schema-inspector');
  if (inspector) {
    inspector.classList.toggle('hidden');
  }
}

function selectErdEntity(id, fullName, type, rows, size, pk, fk, indices) {
  selectedTableId = id;
  
  // Highlight active card
  document.querySelectorAll('.table-card').forEach(card => {
    card.classList.remove('border-brand-violet', 'shadow-2xl', 'shadow-brand-violet/20');
    card.classList.add('border-marga-border');
  });
  const activeCard = document.getElementById('card-' + id);
  if (activeCard) {
    activeCard.classList.remove('border-marga-border');
    activeCard.classList.add('border-brand-violet', 'shadow-2xl', 'shadow-brand-violet/20');
  }

  // Update inspector DOM
  const nameEl = document.getElementById('inspect-full-name');
  const typeEl = document.getElementById('inspect-type');
  const rowsEl = document.getElementById('inspect-rows');
  const sizeEl = document.getElementById('inspect-size');
  const pkEl = document.getElementById('inspect-pk');
  const fkEl = document.getElementById('inspect-fk');
  const idxCountEl = document.getElementById('inspect-idx-count');
  const idxListEl = document.getElementById('inspect-indices');

  if (nameEl) nameEl.textContent = fullName;
  if (typeEl) typeEl.textContent = type;
  if (rowsEl) rowsEl.textContent = rows;
  if (sizeEl) sizeEl.textContent = size;
  if (pkEl) pkEl.textContent = pk;
  if (fkEl) fkEl.textContent = fk;

  if (idxCountEl) idxCountEl.textContent = (indices ? indices.length : 0) + ' Índices';
  if (idxListEl) {
    idxListEl.innerHTML = '';
    if (indices && indices.length > 0) {
      indices.forEach(idx => {
        const div = document.createElement('div');
        div.className = 'p-2 rounded-lg bg-marga-card border border-marga-border flex items-center justify-between text-xs font-mono';
        div.innerHTML = `
          <div class="flex items-center gap-1.5">
            <span class="material-symbols-outlined text-[13px] text-brand-cyan">storage</span>
            <span class="text-slate-200">${idx.split(' ')[0]}</span>
          </div>
          <span class="text-[10px] text-slate-400">${idx.replace(idx.split(' ')[0], '').trim()}</span>
        `;
        idxListEl.appendChild(div);
      });
    }
  }

  // Ensure inspector is visible
  const inspector = document.getElementById('schema-inspector');
  if (inspector && inspector.classList.contains('hidden')) {
    inspector.classList.remove('hidden');
  }
}

function focusSelectedTable() {
  const activeCard = document.getElementById('card-' + selectedTableId);
  if (activeCard) {
    activeCard.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'center' });
  }
}

function exportDiagram(format) {
  if (typeof showToast === 'function') {
    showToast(`Diagrama exportado como ${format.toUpperCase()} con éxito`, 'success');
  } else {
    alert(`Exportando Schema Graph como ${format} vector de alta resolución.`);
  }
}

document.addEventListener('DOMContentLoaded', () => {
  applyZoom();
});
