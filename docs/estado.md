# Estado de Marga

_Actualizado: 16 de noviembre de 2026_

Marga es control de versiones para el esquema de la base de datos, pensado para trabajar
junto a git: cada cambio al esquema es una migración (`up` / `down`) que se comparte con el
equipo, se revisa, se aplica por entorno y sigue a las ramas de git.

Tiene dos piezas:

- **`marga`**: la CLI (Go + Cobra). Es lo que usa cada desarrollador.
- **`marga-server`**: la API REST y la web, que guardan el historial compartido en PostgreSQL.

---

## Lo que ya está hecho

### Migraciones (núcleo)

- [x] `marga init`, `marga env add / ls / rm / probar`, `marga login / logout / whoami`.
- [x] `marga new` (migraciones a mano) y `marga commit` (Marga compara tu base contra el
      último snapshot y escribe el `up` y el `down` sola).
- [x] `marga up`, `down`, `status`, `up --dry-run`, con bloqueo para que dos personas
      no apliquen a la vez (advisory lock).
- [x] Checksums de cada migración aplicada; detecta archivos editados después de aplicarse.
      Normaliza CRLF/LF para que Windows y Linux den el mismo checksum.
- [x] `marga drift`: cambios hechos a mano en la base, fuera de Marga.
- [x] `marga esquema [objeto]`: el esquema real, o el DDL de un objeto.
- [x] `marga revisar`: análisis de riesgo (DROP, cambios de tipo, NOT NULL sin default,
      bloqueos largos, etc.).
- [x] `marga verificar`: aplica todas las migraciones desde cero en una base temporal y
      comprueba que se revierten limpio.

### Objetos del esquema (fase 2)

- [x] Tablas, columnas, índices, llaves primarias y foráneas, CHECK.
- [x] Tipos ENUM, funciones, procedimientos, vistas, vistas materializadas y triggers en
      `commit`, `drift` y `esquema`.
- [x] Orden por dependencias (pg_depend, incluso a nivel de columna) al crear y borrar.
- [x] Snapshots versión 2; los de la versión 1 se actualizan solos.
- [x] Fusión por objeto al juntar ramas.

### Facilidad de uso (fase A)

- [x] `marga init` guiado: en una terminal y sin opciones pregunta nombre, URL de la base (y
      la prueba), si usarás servidor (inicia sesión, crea o vincula el proyecto) y ofrece
      `adoptar` si la base ya tiene tablas. Con opciones, en CI o con `MARGA_NO_INTERACTIVO`
      se comporta como antes.
- [x] Modo local oficial: `marga init --local`; `push` y `pull` explican que el proyecto no
      usa servidor y terminan sin error; `log`, `branch`, `checkout` y `merge` dicen cómo vincularlo.
- [x] Todos los errores terminan con `prueba: …` (paquete `internal/modelos/sugerencia`). Casos de
      las pruebas manuales: 404 de algo que no es Marga, servidor apagado, servidor viejo, sesión
      vencida, slug repetido (dice qué `--repo` usar), proyecto sin entornos, y un `marga` viejo
      antes en el PATH.
- [x] Instalación de un paso: Scoop, winget y Homebrew en `.goreleaser.yaml`, y scripts
      `scripts/instalar.ps1` / `scripts/instalar.sh`. `marga version` avisa si hay una versión
      nueva (y cómo actualizar) y si otro `marga` del PATH tapa a este.
- [x] [Inicio rápido](inicio-rapido.md): de cero a la primera migración en 5 minutos.

### Empezar con una base que ya existe

- [x] `marga adoptar`: convierte el esquema actual en la migración `0001` (incluye esquemas,
      extensiones y secuencias sueltas), la prueba en una base temporal (ida y vuelta) y la
      marca como aplicada sin tocar los datos.
- [x] `status`, `drift` y `commit` sugieren `marga adoptar` cuando ven una base con tablas y
      un proyecto sin migraciones.

### Trabajo en equipo y ramas

- [x] `marga push / pull / log`: el historial compartido en el servidor.
- [x] Marga sigue la rama de git; una base por rama (`tienda__feature_x`, copiada de la de main).
- [x] `marga ramas` (tamaño, último uso, borrar), `marga url --escribir .env`.
- [x] `marga renumerar` cuando dos ramas crean la misma versión; `pull` renombra y `up`
      actualiza el historial sin volver a ejecutar.
- [x] Un solo merge: `git merge` + `marga push` fusiona también en el servidor.
- [x] Entornos protegidos: en `prod`, solo se aplican migraciones aprobadas por otra persona.
- [x] Tokens de acceso (`MARGA_TOKEN`) y URLs desde variables de entorno para CI.

### SQL y terminal

- [x] Objetos repetibles (`sql/`) y datos de prueba (`seeds/`, `marga seed`).
- [x] `marga sql`: consola interactiva con resaltado en vivo mientras escribes.
- [x] `marga ui`: panel en Bubble Tea con temas personalizables, colores de sintaxis y
      búsqueda de migraciones con `/`.
- [x] `marga` sin argumentos muestra la ayuda (se quitó la consola propia antigua).

### Servidor y web

- [x] API REST, cuentas, sesiones, equipos, colaboradores, auditoría, límite de intentos,
      restablecimiento de contraseña por correo.
- [x] Web embebida en el binario (Tailwind compilado): proyectos, commits, diffs, revisiones.
- [x] Despliegue con Docker Compose, HTTPS (Caddy) y respaldos diarios (`deploy/`).

### Arquitectura y pruebas

- [x] Carpetas por capas: `internal/modelos` (lógica pura), `datos` (bases, archivos, red),
      `servicios`, `controladores` (CLI y API), `vistas` (terminal y web).
- [x] Interfaz `Motor` (`internal/datos/motor`): lo que cambia de un motor de base de datos
      a otro. PostgreSQL es la primera implementación (`motor/postgres`); el modelo del
      esquema (`modelos/esquema`) no depende de ningún motor.
- [x] Pruebas en `tests/`: `unit` (sin base), `integracion` y `e2e` (necesitan PostgreSQL,
      `MARGA_TEST_DB_URL`). `make probar`, `probar-integracion`, `probar-e2e`, `probar-todo`.
- [x] CI en GitHub Actions con las tres etapas separadas; releases con GoReleaser.

---

## Lo que falta

### Más motores de base de datos (plan en marcha)

1. ~~`marga adoptar` para bases PostgreSQL existentes~~ ✔
2. ~~Separar el modelo del esquema y crear la interfaz `Motor`~~ ✔
3. [ ] **MySQL / MariaDB**: implementar `Motor` en `internal/datos/motor/mysql`
       (leer `information_schema`, generar DDL, `GET_LOCK` para el bloqueo).
       Ojo: MySQL no hace DDL dentro de transacciones (`DDLEnTransaccion() = false`), así
       que una migración que falla a la mitad no se revierte sola; hay que avisarlo.
       Las bases por rama tendrán que copiarse con dump/restore (no hay `TEMPLATE`).
4. [ ] **SQL Server** y **SQLite**.
5. [ ] **MongoDB** (al final): no tiene esquema fijo; habría que versionar validadores e
       índices, no tablas.

Hoy `marga env probar` ya conecta con MySQL y SQL Server, pero aplicar migraciones, drift,
commit y adoptar solo funcionan en PostgreSQL.

### Facilidad de uso en proyectos reales

- [ ] Publicar el primer release con Scoop, winget y Homebrew: la configuración está en
      `.goreleaser.yaml`; falta crear los repos `scoop-marga` y `homebrew-tap`, el fork de
      `winget-pkgs` y el secreto `MARGA_PUBLICAR_TOKEN` (ver el comentario al inicio del archivo),
      y elegir la licencia (hoy dice `Proprietary`).

### Esquema

- [ ] Particiones, políticas RLS, permisos (`GRANT`), comentarios (`COMMENT ON`) y
      dominios (`CREATE DOMAIN`) en `commit` / `drift` / `adoptar`.
- [ ] Detectar renombres de columnas y tablas: hoy `commit` los ve como borrar + crear
      (avisa y sugiere `RENAME`, pero hay que editarlo a mano).
- [ ] Quitar o renombrar valores de un ENUM (hoy `commit` solo agrega valores).
- [ ] Que `commit` escriba solo `CREATE INDEX CONCURRENTLY` en tablas grandes (hoy se hace a
      mano con `-- marga:sin-transaccion`; `revisar` ya lo recomienda).
- [ ] Migraciones de datos largas por lotes.

### Ramas y entornos

- [ ] `marga checkout` solo existe en proyectos sin git (con git, Marga sigue la rama de git).
- [ ] Bases por rama solo en PostgreSQL y en entornos personales.

### Editor

- [ ] El editor estilo NeoVim dentro de `marga ui` (hoy hay editor básico y consola `marga sql`).

### Calidad

- [ ] Más pruebas e2e de la web (hoy las e2e cubren la CLI contra un servidor real).
- [ ] Correr las pruebas en Windows y macOS dentro del CI (hoy se prueban en Linux; para
      Windows y macOS solo se compila).

---

## Cómo probar lo último

```powershell
go build -o $HOME\go\bin\marga.exe .\cmd\marga
mkdir C:\temp\adoptar-prueba; cd C:\temp\adoptar-prueba
marga init --crear "Adoptar prueba"
marga env add dev "postgres://postgres:postgres@localhost:5432/biblioteca?sslmode=disable"
marga adoptar --simular
marga adoptar
marga drift
marga verificar
```

Pruebas automáticas: `make probar` (unitarias) y `make probar-todo` (necesita PostgreSQL).
