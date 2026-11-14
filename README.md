# Marga — Plataforma de Control de Versiones de Bases de Datos & Migraciones

Arquitectura frontend modular, simétrica y unificada para la plataforma **Marga**.

---

## 📁 Estructura del Proyecto

```
Marga Sun/
├── index.html                   # Punto de entrada principal (redirección a dashboard)
├── server/                      # Backend Go + PostgreSQL (API REST y migraciones)
├── README.md                    # Documentación del proyecto
├── pages/                       # Vistas completas e integradas
│   ├── login.html               # Inicio de sesión / registro
│   ├── cuenta.html              # Tu cuenta y tokens de acceso para la CLI
│   ├── proyectos.html           # Dashboard de proyectos, clusters y métricas
│   ├── code.html                # Resumen del proyecto: tablas, DDL, ramas y personas
│   ├── commits.html             # Historial de commits DDL, AST diffs y reversión
│   ├── entornos.html            # Qué tiene aplicado cada base (dev, staging, prod) y su drift
│   └── esquema.html             # Diagrama ER real, leído de la base de un entorno
├── assets/                      # Recursos compartidos y lógica modular
│   ├── css/
│   │   └── marga-shared.css     # Estilos compartidos, scrollbars finos, canvas grid y glows
│   ├── js/
│   │   ├── marga-api.js         # Cliente REST, sesión y usuario
│   │   ├── marga-common.js      # Cambio de tema (Oscuro/Claro), Command Palette (⌘K) y Toasts
│   │   ├── proyectos.js         # Drawer de proyectos, modales de confirmación y filtros
│   │   ├── marga-layout.js      # Cabecera: fuera de un proyecto / dentro de un proyecto
│   │   ├── resumen.js           # Lógica de la página Resumen
│   │   ├── commits-viewer.js    # Selección de commits, visualizador diff y dry-runs
│   │   ├── entornos.js          # Lógica de la página Entornos
│   │   └── esquema.js           # Diagrama ER: distribución, líneas de FK, zoom e inspector
└── _archive_legacy/             # Copia de seguridad de los archivos sueltos originales
```

---

## 🚀 ¿Cómo Ejecutar?

1. **Directamente en el navegador**: Haz doble clic en `index.html` o en cualquiera de los archivos dentro de `pages/`.
2. **Con un servidor local** (ej. Live Server, http-server o npx serve):
   ```bash
   npx serve .
   ```
   Abrirá la aplicación en `http://localhost:3000` con navegación fluida entre todas las pantallas.

---

## ✨ Mejoras y Correcciones Implementadas

### 1. Correcciones Críticas de CSS y Viewport
- **Eliminación del viewport rígido**: Se eliminaron los atributos `style="width: 1280px; height: ...px; overflow: hidden"` que bloqueaban el scroll natural y recortaban la pantalla en monitores estándar y pantallas grandes.
- **Sintaxis CSS corregida**: Se arreglaron errores de clases Tailwind (`pr- space-md`).
- **Normalización de paleta**: Se unificó `commits.html` (que usaba tokens discordantes) a la paleta estricta oficial de Marga (`marga-bg: #0B0F19`, `marga-surface: #111827`, `marga-card: #151D31`, `brand-violet: #7C3AED`, `brand-cyan: #38BDF8`, etc.).

### 2. Simetría y Navegación de Doble Nivel
- **Cabecera simétrica (Nivel 1)**: Todas las páginas cuentan con la misma cabecera superior con logo vectorial nítido, selector de proyecto activo, chip de cluster, buscador interactivo, botón CLI, selector de tema funcional y avatar de usuario.
- **Subbarra de navegación (Nivel 2)**: Barra de 5 pestañas que conecta de forma inmediata y real todas las áreas de la plataforma, marcando visualmente la pantalla activa.

### 3. Nuevas Capacidades Interactivas
- **Command Palette (`⌘K` / `Ctrl+K`)**: Atajo de teclado universal para navegar entre cualquier pantalla o recurso con búsqueda en vivo.
- **Selector de Tema Claro / Oscuro**: Conmutación suave y persistencia automática en `localStorage`.
- **Notificaciones Toast no intrusivas**: Reemplazo de alertas nativas intrusivas por notificaciones modernas en la esquina inferior.
- **Inspector de Tablas en ERD SQL**: Restauración del panel lateral derecho para inspeccionar columnas, PKs, FKs e índices.
- **Minimapa en NoSQL**: Widget flotante conectado al botón del HUD.

---

## 🔌 Backend y Base de Datos (`server/`)

El front ya está conectado a un servidor en **Go + PostgreSQL** que vive en `server/`
(detalles completos en [`server/README.md`](server/README.md)).

```powershell
cd server
docker compose up -d      # o usa tu PostgreSQL local
copy .env.example .env
go run . -seed            # crea tablas + datos demo
```

Abre **http://localhost:8080** y entra con `demo@marga.dev` / `marga1234`.

- `pages/login.html` — inicio de sesión y registro.
- `assets/js/marga-api.js` — cliente de la API, guardia de sesión y menú de usuario.
- `proyectos.html` — tarjetas, contadores, crear / editar / eliminar proyectos desde la BD.
- `commits.html` — historial real por rama, diff de esquema, búsqueda, revert y descarga del SQL.
- Abiertas con doble clic (`file://`) las páginas siguen en modo demo.
