/**
 * Diagrama ER real (esquema.html)
 * Dibuja las tablas y llaves foráneas que la CLI leyó de un entorno.
 * Distribución: columnas por "nivel" de dependencia (las tablas referenciadas
 * quedan a la izquierda de las que las referencian).
 */
(function () {
  const API = window.MargaAPI;
  const $ = (id) => document.getElementById(id);
  const e = (v) => API.escapeHTML(v);

  const ANCHO = 260, CAB = 40, FILA = 26, PIE = 10, SEP_X = 110, SEP_Y = 36, MARGEN = 40;

  let repo = null, entornos = [], esquema = null, cajas = {}, seleccion = null;
  let zoom = 1, tx = 0, ty = 0;

  if (!API.online) return;

  // ---------------------------------------------------------------- carga

  document.addEventListener('marga:repo', async (ev) => {
    repo = ev.detail;
    try {
      entornos = await API.get(`/repos/${repo.id}/entornos`);
      if (!entornos.length) {
        vacio(`<p class="text-white font-medium">Aún no hay esquema que mostrar.</p>
          <p>El diagrama se dibuja con el esquema real que reporta la CLI. Ejecuta <span class="font-mono text-neutral-200">marga status</span> con la sesión iniciada.</p>`);
        $('s-entorno').innerHTML = '<option>sin entornos</option>';
        return;
      }
      const pedido = new URLSearchParams(location.search).get('entorno');
      const inicial = entornos.some(x => x.nombre === pedido) ? pedido
        : (entornos.find(x => x.compartido) || entornos[0]).nombre;
      $('s-entorno').innerHTML = entornos.map(x =>
        `<option class="bg-marga-card" value="${e(x.nombre)}"${x.nombre === inicial ? ' selected' : ''}>${e(x.nombre)}</option>`).join('');
      $('s-entorno').onchange = () => cargar($('s-entorno').value);
      await cargar(inicial);
    } catch (err) {
      showToast(err.message, 'error');
    }
  });

  async function cargar(nombre) {
    const url = new URL(location.href);
    url.searchParams.set('entorno', nombre);
    history.replaceState(null, '', url);
    const d = await API.get(`/repos/${repo.id}/entornos/${encodeURIComponent(nombre)}`);
    esquema = d.esquema && d.esquema.tablas ? d.esquema : { tablas: [] };
    $('s-info').textContent = `${esquema.tablas.length} tablas · reportado ${API.timeAgo(d.reportado_en)}`;
    const drift = $('s-drift');
    if (d.drift.length) {
      drift.classList.remove('hidden');
      drift.classList.add('flex');
      drift.href = `entornos.html?repo=${repo.id}&entorno=${encodeURIComponent(nombre)}`;
      drift.querySelector('span:last-child').textContent = `${d.drift.length} cambio${d.drift.length === 1 ? '' : 's'} fuera de Marga`;
    } else {
      drift.classList.add('hidden');
      drift.classList.remove('flex');
    }
    cerrarInspector();
    if (!esquema.tablas.length) {
      vacio(`<p class="text-white font-medium">${e(nombre)} no tiene tablas.</p><p>Aplica migraciones con <span class="font-mono text-neutral-200">marga up</span>.</p>`);
      return;
    }
    $('s-vacio').classList.add('hidden');
    dibujar();
    ajustar();
  }

  function vacio(html) {
    $('s-plano').querySelectorAll('.s-tabla').forEach(n => n.remove());
    $('s-lineas').innerHTML = '';
    const v = $('s-vacio');
    v.querySelector('div').innerHTML = html;
    v.classList.remove('hidden');
  }

  // ---------------------------------------------------------------- distribución

  function niveles(tablas) {
    const por = Object.fromEntries(tablas.map(t => [t.nombre, t]));
    const memo = {};
    const nivel = (n, pila = new Set()) => {
      if (n in memo) return memo[n];
      if (pila.has(n)) return 0; // ciclo
      pila.add(n);
      let max = -1;
      for (const fk of (por[n]?.fks || [])) {
        if (fk.tabla !== n && por[fk.tabla]) max = Math.max(max, nivel(fk.tabla, pila));
      }
      pila.delete(n);
      return (memo[n] = max + 1);
    };
    tablas.forEach(t => nivel(t.nombre));
    return memo;
  }

  const alto = (t) => CAB + t.columnas.length * FILA + PIE;

  function corto(tipo) {
    return tipo.replace('character varying', 'varchar').replace('timestamp with time zone', 'timestamptz')
      .replace('timestamp without time zone', 'timestamp').replace('integer', 'int').replace('boolean', 'bool')
      .replace('double precision', 'double');
  }

  // ---------------------------------------------------------------- dibujo

  function dibujar() {
    const plano = $('s-plano');
    plano.querySelectorAll('.s-tabla').forEach(n => n.remove());
    const nv = niveles(esquema.tablas);
    const columnas = {};
    esquema.tablas.forEach(t => (columnas[nv[t.nombre]] ||= []).push(t));
    cajas = {};
    let anchoTotal = 0, altoTotal = 0;
    Object.keys(columnas).map(Number).sort((a, b) => a - b).forEach(n => {
      let y = MARGEN;
      const x = MARGEN + n * (ANCHO + SEP_X);
      columnas[n].sort((a, b) => a.nombre.localeCompare(b.nombre)).forEach(t => {
        cajas[t.nombre] = { x, y, t };
        y += alto(t) + SEP_Y;
      });
      anchoTotal = Math.max(anchoTotal, x + ANCHO + MARGEN);
      altoTotal = Math.max(altoTotal, y + MARGEN);
    });
    plano.style.width = anchoTotal + 'px';
    plano.style.height = altoTotal + 'px';
    const svg = $('s-lineas');
    svg.setAttribute('width', anchoTotal);
    svg.setAttribute('height', altoTotal);

    for (const { x, y, t } of Object.values(cajas)) {
      const pk = new Set(t.pk), fk = new Set(t.fks.flatMap(f => f.columnas));
      const div = document.createElement('div');
      div.className = 's-tabla absolute rounded-lg border border-marga-border bg-marga-surface shadow-lg cursor-pointer hover:border-neutral-500 transition-colors';
      div.dataset.t = t.nombre;
      div.style.cssText = `left:${x}px;top:${y}px;width:${ANCHO}px;height:${alto(t)}px`;
      div.innerHTML = `
        <div class="flex items-center gap-2 px-3 border-b border-marga-border" style="height:${CAB}px">
          <span class="material-symbols-outlined text-[16px] text-neutral-500">table</span>
          <span class="font-mono text-[13px] font-semibold text-white truncate">${e(t.nombre)}</span>
        </div>
        ${t.columnas.map(c => `
          <div class="flex items-center gap-2 px-3 font-mono text-xs" style="height:${FILA}px">
            <span class="material-symbols-outlined text-[13px] w-4 ${pk.has(c.nombre) ? 'text-white' : fk.has(c.nombre) ? 'text-neutral-400' : 'text-transparent'}">${pk.has(c.nombre) ? 'key' : 'link'}</span>
            <span class="${pk.has(c.nombre) ? 'text-white' : 'text-neutral-200'} truncate">${e(c.nombre)}</span>
            <span class="ml-auto text-neutral-500 truncate">${e(corto(c.tipo))}${c.nulo ? '' : ' <span class="text-neutral-400" title="NOT NULL: no acepta vacíos">!</span>'}</span>
          </div>`).join('')}`;
      div.addEventListener('click', (ev) => { ev.stopPropagation(); seleccionar(t.nombre); });
      plano.appendChild(div);
    }
    lineas();
  }

  function filaY(caja, columna) {
    const i = Math.max(0, caja.t.columnas.findIndex(c => c.nombre === columna));
    return caja.y + CAB + i * FILA + FILA / 2;
  }

  function lineas() {
    const partes = [];
    for (const hijo of Object.values(cajas)) {
      for (const fk of hijo.t.fks) {
        const padre = cajas[fk.tabla];
        if (!padre) continue;
        const y1 = filaY(padre, fk.ref_columnas[0]), y2 = filaY(hijo, fk.columnas[0]);
        let d;
        if (padre.x + ANCHO <= hijo.x) {           // padre a la izquierda (lo normal)
          const x1 = padre.x + ANCHO, x2 = hijo.x, m = Math.max(40, (x2 - x1) / 2);
          d = `M${x1},${y1} C${x1 + m},${y1} ${x2 - m},${y2} ${x2},${y2}`;
        } else {                                    // misma columna o ciclo: por la derecha
          const x1 = padre.x + ANCHO, x2 = hijo.x + ANCHO;
          d = `M${x1},${y1} C${x1 + 70},${y1} ${x2 + 70},${y2} ${x2},${y2}`;
        }
        const activa = seleccion && (seleccion === hijo.t.nombre || seleccion === padre.t.nombre);
        const color = activa ? '#FAFAFA' : '#525252';
        partes.push(`<path d="${d}" fill="none" stroke="${color}" stroke-width="${activa ? 1.8 : 1.2}"/>`);
        const [ex, ey] = d.split(' ').pop().split(',').map(Number);
        partes.push(`<circle cx="${ex}" cy="${ey}" r="3" fill="${color}"/>`);
      }
    }
    $('s-lineas').innerHTML = partes.join('');
  }

  // ---------------------------------------------------------------- inspector

  function ddl(t) {
    const partes = t.columnas.map(c => `    ${c.nombre} ${c.tipo}${c.nulo ? '' : ' NOT NULL'}${c.defecto ? ' DEFAULT ' + c.defecto : ''}`);
    if (t.pk.length) partes.push(`    PRIMARY KEY (${t.pk.join(', ')})`);
    t.fks.forEach(f => partes.push(`    FOREIGN KEY (${f.columnas.join(', ')}) REFERENCES ${f.tabla} (${f.ref_columnas.join(', ')})${f.al_borrar ? ' ON DELETE ' + f.al_borrar : ''}`));
    return `CREATE TABLE ${t.nombre} (\n${partes.join(',\n')}\n);` + t.indices.map(i => `\n${i.definicion};`).join('');
  }

  function seleccionar(nombre) {
    seleccion = nombre;
    document.querySelectorAll('.s-tabla').forEach(n => {
      n.classList.toggle('border-white', n.dataset.t === nombre);
      n.classList.toggle('border-marga-border', n.dataset.t !== nombre);
    });
    lineas();
    const t = cajas[nombre].t;
    const referenciadaPor = esquema.tablas.filter(o => o.fks.some(f => f.tabla === nombre)).map(o => o.nombre);
    const enlace = (n) => `<button type="button" data-ir="${e(n)}" class="i-ir font-mono text-xs text-neutral-200 hover:text-white hover:underline">${e(n)}</button>`;
    $('i-nombre').textContent = nombre;
    $('i-cuerpo').innerHTML = `
      <div class="grid grid-cols-3 gap-2 text-center">
        <div class="rounded-md border border-marga-border py-2"><div class="text-lg text-white font-semibold">${t.columnas.length}</div><div class="text-[11px] text-neutral-500">columnas</div></div>
        <div class="rounded-md border border-marga-border py-2"><div class="text-lg text-white font-semibold">${t.fks.length}</div><div class="text-[11px] text-neutral-500">llaves foráneas</div></div>
        <div class="rounded-md border border-marga-border py-2"><div class="text-lg text-white font-semibold">${t.indices.length}</div><div class="text-[11px] text-neutral-500">índices</div></div>
      </div>
      ${t.fks.length ? `<div><div class="text-xs text-neutral-500 mb-1.5">Referencia a</div><div class="flex flex-wrap gap-x-3 gap-y-1">${[...new Set(t.fks.map(f => f.tabla))].map(enlace).join('')}</div></div>` : ''}
      ${referenciadaPor.length ? `<div><div class="text-xs text-neutral-500 mb-1.5">Referenciada por</div><div class="flex flex-wrap gap-x-3 gap-y-1">${referenciadaPor.map(enlace).join('')}</div></div>` : ''}
      <div>
        <div class="flex items-center justify-between mb-1.5">
          <span class="text-xs text-neutral-500">DDL (reconstruido del catálogo)</span>
          <button type="button" id="i-copiar" class="text-xs text-neutral-400 hover:text-white">Copiar</button>
        </div>
        <pre class="rounded-md bg-marga-bg border border-marga-border p-3 font-mono text-[11px] leading-relaxed text-neutral-200 whitespace-pre overflow-x-auto">${e(ddl(t))}</pre>
      </div>`;
    $('i-copiar').onclick = () => navigator.clipboard.writeText(ddl(t)).then(() => showToast('DDL copiado', 'success'));
    document.querySelectorAll('.i-ir').forEach(b => b.addEventListener('click', () => { seleccionar(b.dataset.ir); centrar(b.dataset.ir); }));
    const insp = $('s-inspector');
    insp.classList.remove('hidden');
    insp.classList.add('flex');
  }

  function cerrarInspector() {
    seleccion = null;
    $('s-inspector').classList.add('hidden');
    $('s-inspector').classList.remove('flex');
    document.querySelectorAll('.s-tabla').forEach(n => { n.classList.remove('border-white'); n.classList.add('border-marga-border'); });
    if (Object.keys(cajas).length) lineas();
  }
  $('i-cerrar').addEventListener('click', cerrarInspector);

  // ---------------------------------------------------------------- zoom y desplazamiento

  function aplicar() {
    $('s-plano').style.transform = `translate(${tx}px, ${ty}px) scale(${zoom})`;
    $('s-zoom').textContent = Math.round(zoom * 100) + '%';
  }

  function zoomEn(nuevo, cx, cy) {
    nuevo = Math.min(2, Math.max(0.25, nuevo));
    const r = $('s-lienzo').getBoundingClientRect();
    cx ??= r.width / 2; cy ??= r.height / 2;
    tx = cx - (cx - tx) * (nuevo / zoom);
    ty = cy - (cy - ty) * (nuevo / zoom);
    zoom = nuevo;
    aplicar();
  }

  function ajustar() {
    const r = $('s-lienzo').getBoundingClientRect();
    const w = parseFloat($('s-plano').style.width) || 1, h = parseFloat($('s-plano').style.height) || 1;
    zoom = Math.min(1, Math.max(0.25, Math.min(r.width / w, r.height / h)));
    tx = Math.max(0, (r.width - w * zoom) / 2);
    ty = Math.max(0, (r.height - h * zoom) / 2);
    aplicar();
  }

  function centrar(nombre) {
    const c = cajas[nombre];
    if (!c) return;
    const r = $('s-lienzo').getBoundingClientRect();
    tx = r.width / 2 - (c.x + ANCHO / 2) * zoom;
    ty = r.height / 3 - c.y * zoom;
    aplicar();
  }

  $('s-mas').addEventListener('click', () => zoomEn(zoom * 1.2));
  $('s-menos').addEventListener('click', () => zoomEn(zoom / 1.2));
  $('s-ajustar').addEventListener('click', ajustar);

  const lienzo = $('s-lienzo');
  lienzo.addEventListener('wheel', (ev) => {
    ev.preventDefault();
    const r = lienzo.getBoundingClientRect();
    if (ev.ctrlKey || ev.metaKey) {
      zoomEn(zoom * (ev.deltaY < 0 ? 1.1 : 1 / 1.1), ev.clientX - r.left, ev.clientY - r.top);
    } else {
      tx -= ev.deltaX; ty -= ev.deltaY;
      aplicar();
    }
  }, { passive: false });

  let arrastre = null;
  lienzo.addEventListener('mousedown', (ev) => {
    if (ev.target.closest('.s-tabla')) return;
    arrastre = { x: ev.clientX - tx, y: ev.clientY - ty, movio: false };
    lienzo.classList.replace('cursor-grab', 'cursor-grabbing');
  });
  window.addEventListener('mousemove', (ev) => {
    if (!arrastre) return;
    tx = ev.clientX - arrastre.x; ty = ev.clientY - arrastre.y;
    arrastre.movio = true;
    aplicar();
  });
  window.addEventListener('mouseup', () => {
    if (arrastre && !arrastre.movio) cerrarInspector();
    arrastre = null;
    lienzo.classList.replace('cursor-grabbing', 'cursor-grab');
  });
})();
