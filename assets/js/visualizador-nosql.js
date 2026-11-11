/**
 * Marga Interactive NoSQL Schema Graph Engine
 * Handles zoom, pan, collection filter, minimap toggle and JSON Schema export
 */

let canvasZoom = 1.0;

function updateZoomTransform() {
  const plane = document.getElementById('nosqlPlane');
  const display = document.getElementById('zoomLevelDisplay');
  if (plane) {
    plane.style.transform = `scale(${canvasZoom})`;
  }
  if (display) {
    display.textContent = `${Math.round(canvasZoom * 100)}%`;
  }
}

function zoomIn() {
  canvasZoom = Math.min(canvasZoom + 0.1, 1.5);
  updateZoomTransform();
}

function zoomOut() {
  canvasZoom = Math.max(canvasZoom - 0.1, 0.6);
  updateZoomTransform();
}

function resetZoomCenter() {
  canvasZoom = 1.0;
  updateZoomTransform();
  const wrapper = document.getElementById('nosqlCanvasWrapper');
  if (wrapper) {
    wrapper.scrollTo({ left: 0, top: 0, behavior: 'smooth' });
  }
  if (typeof showToast === 'function') {
    showToast('Lienzo centrado al 100%');
  }
}

function toggleMinimap() {
  const map = document.getElementById('canvasMinimap');
  if (map) {
    map.classList.toggle('hidden');
  }
}

function filterCollections(type) {
  // Update button states
  const pills = document.querySelectorAll('.filter-pill');
  pills.forEach(p => {
    p.className = 'filter-pill px-3 py-1 rounded-md text-xs font-medium text-neutral-400 hover:text-neutral-200 hover:bg-marga-card transition-all';
  });

  const activeBtn = document.getElementById('btn-' + type);
  if (activeBtn) {
    activeBtn.className = 'filter-pill px-3 py-1 rounded-md text-xs font-medium bg-brand-violet text-neutral-950 shadow-sm transition-all';
  }

  // Toggle cards visibility
  const cards = document.querySelectorAll('[id^="card-"]');
  cards.forEach(card => {
    if (type === 'all') {
      card.style.opacity = '1';
      card.style.filter = 'none';
    } else {
      const cardType = card.getAttribute('data-type');
      if (cardType === type) {
        card.style.opacity = '1';
        card.style.filter = 'none';
      } else {
        card.style.opacity = '0.2';
        card.style.filter = 'grayscale(80%)';
      }
    }
  });

  // Toggle SVG connections brightness
  const svgLayer = document.getElementById('connectionsSvg');
  if (svgLayer) {
    if (type !== 'all' && type !== 'subdocs') {
      svgLayer.style.opacity = '0.15';
    } else {
      svgLayer.style.opacity = '1';
    }
  }
}

function exportSchemaModal() {
  const modal = document.getElementById('exportModal');
  if (modal) {
    modal.classList.remove('hidden');
  }
}

function closeSchemaModal() {
  const modal = document.getElementById('exportModal');
  if (modal) {
    modal.classList.add('hidden');
  }
}

function copySchemaJson() {
  const jsonSchema = {
    "$jsonSchema": {
      "bsonType": "object",
      "required": ["_id", "email", "auth", "created_at"],
      "properties": {
        "_id": { "bsonType": "objectId" },
        "email": { "bsonType": "string", "pattern": "^.+@.+\\..+$" },
        "auth": {
          "bsonType": "object",
          "required": ["password_hash", "status"],
          "properties": {
            "password_hash": { "bsonType": "string" },
            "status": { "enum": ["ACTIVE", "SUSPENDED", "PENDING_VERIFICATION"] }
          }
        },
        "created_at": { "bsonType": "date" }
      }
    }
  };

  navigator.clipboard?.writeText(JSON.stringify(jsonSchema, null, 2)).then(() => {
    if (typeof showToast === 'function') {
      showToast('JSON Schema copiado al portapapeles con éxito', 'success');
    }
  });
  closeSchemaModal();
}

document.addEventListener('DOMContentLoaded', () => {
  updateZoomTransform();
});
