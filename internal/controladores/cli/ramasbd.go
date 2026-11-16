package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/datos/gitlocal"
	"github.com/zara-te05/Marga/internal/datos/migrador"
	"github.com/zara-te05/Marga/internal/datos/proyecto"
	"github.com/zara-te05/Marga/internal/datos/ramabd"
	"github.com/zara-te05/Marga/internal/modelos/sugerencia"
)

// Una base de datos por rama: en los entornos personales (dev, local) cada rama de
// git usa su propia base. La rama principal (main) usa la base de la cadena de
// conexión; las demás, una copia: tienda → tienda__feature_facturas.

var ramaBDFlags struct {
	vacia   bool
	desde   string
	env     string
	forzar  bool
	simular bool
}

// gitDe devuelve el repositorio git del proyecto (nil si no usa git).
func gitDe(p *proyecto.Proyecto) *gitlocal.Repo {
	r, err := gitlocal.Buscar(p.Raiz)
	if err != nil {
		return nil
	}
	return r
}

// ramaDeTrabajo es la rama que usan push, pull, log, branch y merge: la de git si
// el proyecto usa git (Marga sigue a git), o la de `marga checkout` si no.
func ramaDeTrabajo(p *proyecto.Proyecto) string {
	if r := gitDe(p); r != nil {
		if rama, separado, err := r.RamaActual(); err == nil && !separado {
			return rama
		}
	}
	if p.Config.Rama == "" {
		return "main"
	}
	return p.Config.Rama
}

// sigueRamas: solo los entornos personales de PostgreSQL tienen una base por rama.
// staging y prod (compartidos) nunca se ramifican.
func sigueRamas(nombre string, e proyecto.Entorno) bool {
	return !e.Compartido && !esProduccion(nombre) && (e.Motor == "" || e.Motor == "postgresql")
}

// ramaPrincipal: la rama dueña de la base de la cadena de conexión. Se decide una vez
// y se guarda en .marga/local.json (se puede editar ahí): la rama del proyecto
// (config.json) si existe en git, si no main o master, si no la rama actual
// (un repositorio recién creado todavía no tiene ramas).
func ramaPrincipal(p *proyecto.Proyecto, repo *gitlocal.Repo, local *proyecto.Local) (string, bool) {
	if local.RamaPrincipal != "" {
		return local.RamaPrincipal, false
	}
	pr := ""
	for _, r := range []string{p.Config.Rama, "main", "master"} {
		if r != "" && repo.Existe(r) {
			pr = r
			break
		}
	}
	if pr == "" {
		if actual, sep, err := repo.RamaActual(); err == nil && !sep {
			pr = actual
		} else {
			pr = "main"
		}
	}
	local.RamaPrincipal = pr
	return pr, true
}

func claveRama(entorno, base string) string { return entorno + "@" + base }

// resolverRama hace que un entorno personal apunte a la base de la rama de git
// actual, creándola si todavía no existe.
func resolverRama(ctx context.Context, c *cobra.Command, p *proyecto.Proyecto, nombre string, e proyecto.Entorno, locales []proyecto.Migracion) (proyecto.Entorno, error) {
	if !sigueRamas(nombre, e) {
		return e, nil
	}
	repo := gitDe(p)
	if repo == nil {
		return e, nil
	}
	rama, separado, err := repo.RamaActual()
	if err != nil {
		return e, nil
	}
	if separado {
		linea(c, "%s", sTenue.Render("git no está en ninguna rama (HEAD separado): se usa la base principal de "+nombre+"."))
		return e, nil
	}
	local, err := p.Local()
	if err != nil {
		return e, err
	}
	principal, cambio := ramaPrincipal(p, repo, local)
	urlP, err := e.URLReal()
	if err != nil {
		return e, err
	}
	baseP, err := ramabd.BaseDeURL(urlP)
	if err != nil {
		return e, fmt.Errorf("%s: %w", nombre, err)
	}
	if rama == principal {
		if cambio {
			_ = p.GuardarLocal(local)
		}
		return e.ConRama(e.URL, rama, baseP, ""), nil
	}

	admin, err := ramabd.AbrirAdmin(ctx, urlP)
	if err != nil {
		return e, fmt.Errorf("%s: %w", nombre, err)
	}
	defer admin.Close()
	br := local.Base(nombre, rama)
	if br != nil {
		ok, err := admin.Existe(ctx, br.Base)
		if err != nil {
			return e, err
		}
		if !ok {
			linea(c, "%s La base %s de la rama %s ya no existe en el servidor: se vuelve a crear.", sAviso.Render("!"), br.Base, rama)
			br = nil
		}
	}
	if br == nil {
		br, err = crearBaseRama(ctx, c, p, repo, admin, local, nombre, urlP, baseP, rama, principal, locales)
		if err != nil {
			return e, err
		}
	}
	br.UltimoUso = time.Now().UTC()
	local.PonerBase(nombre, rama, br)
	if err := p.GuardarLocal(local); err != nil {
		return e, err
	}
	u, err := ramabd.ConBase(urlP, br.Base)
	if err != nil {
		return e, err
	}
	return e.ConRama(u, rama, br.Base, claveRama(nombre, br.Base)), nil
}

// crearBaseRama crea la base de `rama`: copia (con datos) de la base de su rama de
// origen, o vacía si se pidió --vacia o si la copia traería migraciones que esta
// rama no conoce (la rama nació de un main más viejo).
func crearBaseRama(ctx context.Context, c *cobra.Command, p *proyecto.Proyecto, repo *gitlocal.Repo, admin *ramabd.Admin,
	local *proyecto.Local, nombre, urlP, baseP, rama, principal string, locales []proyecto.Migracion) (*proyecto.BaseRama, error) {

	nuevo := ramabd.Nombre(baseP, rama)
	for _, r := range local.Ramas(nombre) {
		if r != rama && local.Base(nombre, r).Base == nuevo {
			nuevo = ramabd.NombreConHash(baseP, rama) // dos ramas con el mismo sufijo (feature/a-b y feature/a_b)
		}
	}
	if existe, err := admin.Existe(ctx, nuevo); err != nil {
		return nil, err
	} else if existe {
		marca, err := admin.Marca(ctx, nuevo)
		if err != nil {
			return nil, err
		}
		if marca == rama {
			// La creó Marga antes (p. ej. se borró .marga/local.json): se vuelve a usar.
			linea(c, "%s Rama %s: se usa la base %s que ya existía.", sAcento.Render("⎇"), sNegri.Render(rama), sNegri.Render(nuevo))
			return &proyecto.BaseRama{Base: nuevo, CreadaEn: time.Now().UTC()}, nil
		}
		nuevo = ramabd.NombreConHash(baseP, rama)
		if existe, err := admin.Existe(ctx, nuevo); err != nil || existe {
			return nil, sugerencia.Nuevof("revisa si esa base es tuya; si no la usas, bórrala a mano (DROP DATABASE) y repite", "ya existe una base llamada %s que no es de esta rama; Marga no la toca", nuevo)
		}
	}

	// ¿De qué base se copia?
	origen := ramaBDFlags.desde
	if origen == "" {
		if o := repo.Origen(rama); o != "" && o != rama && (o == principal || local.Base(nombre, o) != nil) {
			origen = o
		} else {
			origen = principal
		}
	}
	baseO, claveO := baseP, nombre
	if origen != principal {
		bo := local.Base(nombre, origen)
		if bo == nil {
			return nil, sugerencia.Nuevof("marga ramas para ver qué ramas tienen base", "la rama %s no tiene base propia en %s (hay: %s)", origen, nombre, strings.Join(append([]string{principal}, local.Ramas(nombre)...), ", "))
		}
		if ok, _ := admin.Existe(ctx, bo.Base); !ok {
			linea(c, "%s La base de %s ya no existe: se copia la de %s.", sAviso.Render("!"), origen, principal)
			origen = principal
		} else {
			baseO, claveO = bo.Base, claveRama(nombre, bo.Base)
		}
	}

	vacia := ramaBDFlags.vacia
	if !vacia {
		urlO, err := ramabd.ConBase(urlP, baseO)
		if err != nil {
			return nil, err
		}
		ajenas, err := migracionesAjenas(ctx, urlO, locales)
		if err != nil {
			return nil, fmt.Errorf("no se pudo revisar la base %s antes de copiarla: %w", baseO, err)
		}
		if len(ajenas) > 0 {
			linea(c, "%s La base de %s tiene %s que esta rama no conoce (%s):", sAviso.Render("!"), origen,
				plural(len(ajenas), "migración", "migraciones"), resumirLista(ajenas, 3))
			linea(c, "    %s", sTenue.Render("la rama nació de una versión más vieja de "+origen+". Se crea la base vacía y se aplican las migraciones de la rama."))
			vacia = true
		}
	}

	inicio := time.Now()
	clave := claveRama(nombre, nuevo)
	if vacia {
		if err := admin.CrearVacia(ctx, nuevo, rama); err != nil {
			return nil, err
		}
		_ = p.BorrarSnapshots(clave)
		linea(c, "%s Rama %s: base nueva %s (vacía; %s la deja al día) %s", sAcento.Render("⎇"), sNegri.Render(rama), sNegri.Render(nuevo),
			"marga up", sTenue.Render(fmt.Sprintf("%dms", time.Since(inicio).Milliseconds())))
		if seeds, _ := p.Seeds(); len(seeds) > 0 {
			linea(c, "    %s", sTenue.Render("Queda sin datos: después de marga up, llénala con marga seed."))
		}
		return &proyecto.BaseRama{Base: nuevo, Vacia: true, CreadaEn: time.Now().UTC()}, nil
	}
	if err := admin.Copiar(ctx, baseO, nuevo, rama); err != nil {
		var oc *ramabd.ErrOcupada
		if errors.As(err, &oc) {
			lista := make([]string, 0, len(oc.Conexiones))
			for _, cx := range oc.Conexiones {
				lista = append(lista, "    - "+cx.String())
			}
			return nil, sugerencia.Nuevof("cierra esas conexiones (pgAdmin, tu app…) y repite, o crea la base vacía con las migraciones de la rama: marga up --vacia",
				"no se pudo crear la base de la rama %s: PostgreSQL no deja copiar %s mientras alguien esté conectado a ella:\n%s",
				rama, baseO, strings.Join(lista, "\n"))
		}
		return nil, err
	}
	if err := p.CopiarSnapshots(claveO, clave); err != nil {
		linea(c, "%s no se pudieron copiar los snapshots de %s: %v", sAviso.Render("!"), baseO, err)
	}
	linea(c, "%s Rama %s: base nueva %s, copia de %s (%s) %s", sAcento.Render("⎇"), sNegri.Render(rama), sNegri.Render(nuevo),
		baseO, origen, sTenue.Render(fmt.Sprintf("%dms", time.Since(inicio).Milliseconds())))
	return &proyecto.BaseRama{Base: nuevo, Desde: origen, CreadaEn: time.Now().UTC()}, nil
}

// migracionesAjenas lista las migraciones aplicadas en una base que no están (o
// son distintas) en los archivos locales: copiar esa base daría un historial roto.
func migracionesAjenas(ctx context.Context, url string, locales []proyecto.Migracion) ([]string, error) {
	m, err := migrador.Abrir(ctx, proyecto.Entorno{URL: url, Motor: "postgresql"})
	if err != nil {
		return nil, err
	}
	defer m.Close()
	hist, err := m.Historial(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range migrador.Comparar(locales, hist) {
		if f.Situacion == migrador.Huerfana || f.Situacion == migrador.Modificada {
			out = append(out, f.Registro.Version+"_"+f.Registro.Nombre)
		}
	}
	return out, nil
}

func resumirLista(xs []string, n int) string {
	if len(xs) <= n {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:n], ", ") + fmt.Sprintf(" y %d más", len(xs)-n)
}

// ---------------------------------------------------------------- marga ramas

// entornosConRamas devuelve los entornos personales (o el pedido con --env).
func entornosConRamas(p *proyecto.Proyecto) ([]string, map[string]proyecto.Entorno, error) {
	m, err := p.Entornos()
	if err != nil {
		return nil, nil, err
	}
	if ramaBDFlags.env != "" {
		e, ok := m[ramaBDFlags.env]
		if !ok {
			return nil, nil, sugerencia.Nuevof("marga env ls para ver los que hay", "el entorno %q no existe", ramaBDFlags.env)
		}
		if !sigueRamas(ramaBDFlags.env, e) {
			return nil, nil, sugerencia.Nuevof("usa un entorno personal: --env dev", "%s es compartido: los entornos compartidos (staging, prod…) no tienen una base por rama", ramaBDFlags.env)
		}
		return []string{ramaBDFlags.env}, m, nil
	}
	var out []string
	for _, n := range proyecto.NombresEntornos(m) {
		if sigueRamas(n, m[n]) {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, nil, sugerencia.Nuevo("no hay entornos personales (dev, local): solo esos tienen una base por rama", "marga env add dev "+proyecto.EjemploURL)
	}
	return out, m, nil
}

type ctxRamas struct {
	p         *proyecto.Proyecto
	repo      *gitlocal.Repo
	local     *proyecto.Local
	principal string
	actual    string
}

func abrirRamas(necesitaGit bool) (*ctxRamas, error) {
	p, err := proyectoActual()
	if err != nil {
		return nil, err
	}
	repo := gitDe(p)
	if repo == nil && necesitaGit {
		return nil, sugerencia.Nuevo("este proyecto no usa git: las bases por rama siguen a las ramas de git", "git init (y haz un primer commit) para activarlas")
	}
	local, err := p.Local()
	if err != nil {
		return nil, err
	}
	r := &ctxRamas{p: p, repo: repo, local: local}
	if repo != nil {
		var cambio bool
		r.principal, cambio = ramaPrincipal(p, repo, local)
		if cambio {
			_ = p.GuardarLocal(local)
		}
		if rama, sep, err := repo.RamaActual(); err == nil && !sep {
			r.actual = rama
		}
	}
	return r, nil
}

// admin abre la conexión de mantenimiento del entorno y devuelve su base principal.
func adminDe(ctx context.Context, e proyecto.Entorno) (*ramabd.Admin, string, string, error) {
	u, err := e.URLReal()
	if err != nil {
		return nil, "", "", err
	}
	baseP, err := ramabd.BaseDeURL(u)
	if err != nil {
		return nil, "", "", err
	}
	a, err := ramabd.AbrirAdmin(ctx, u)
	return a, baseP, u, err
}

var ramasCmd = &cobra.Command{
	Use:   "ramas",
	Short: "Lista las bases de datos de cada rama de git (entornos personales)",
	Long: `En los entornos personales (dev, local) cada rama de git usa su propia base:
la rama principal usa la de la cadena de conexión y las demás una copia,
<base>__<rama>, que Marga crea la primera vez que la usas (marga up, status…).

  marga ramas                       lista las bases: tamaño y último uso
  marga ramas borrar RAMA           borra la base de una rama
  marga ramas reiniciar             vuelve a crear la base de la rama actual
  marga ramas limpiar               borra las bases de ramas que ya no existen en git

Marga marca cada base que crea (COMMENT ON DATABASE) y solo borra bases con esa marca.`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := abrirRamas(false)
		if err != nil {
			return err
		}
		nombres, ents, err := entornosConRamas(r.p)
		if err != nil {
			return err
		}
		if r.repo == nil {
			linea(c, "Este proyecto no usa git, así que cada entorno usa una sola base. Con git, cada rama tendría la suya.")
			return nil
		}
		for i, n := range nombres {
			if i > 0 {
				linea(c, "")
			}
			e := ents[n]
			admin, baseP, _, err := adminDe(ctx, e)
			if err != nil {
				linea(c, "%s %s: %v", sAviso.Render("!"), n, err)
			}
			if admin != nil {
				defer admin.Close()
			}
			tam := func(base string) (string, bool) {
				if admin == nil {
					return "?", true
				}
				ok, err := admin.Existe(ctx, base)
				if err != nil {
					return "?", true
				}
				if !ok {
					return "", false
				}
				t, err := admin.Tamano(ctx, base)
				if err != nil {
					return "?", true
				}
				return ramabd.TamanoLegible(t), true
			}
			linea(c, "Entorno %s · %s", sNegri.Render(n), sTenue.Render(e.URLOculta()))
			fila := func(rama, base, tamano, uso, nota string) {
				marca := "  "
				nr := rama
				if rama == r.actual {
					marca, nr = sAcento.Render("* "), sNegri.Render(rama)
				}
				linea(c, "  %s%s %s %s %s %s", marca, relleno(nr, 24), relleno(base, 36), relleno(sTenue.Render(tamano), 9), relleno(sTenue.Render(uso), 16), nota)
			}
			t, _ := tam(baseP)
			fila(r.principal, baseP, t, "", sTenue.Render("principal"))
			for _, rama := range r.local.Ramas(n) {
				b := r.local.Base(n, rama)
				t, existe := tam(b.Base)
				var notas []string
				if !existe {
					notas = append(notas, sAviso.Render("la base ya no existe"))
				}
				if !r.repo.Existe(rama) {
					notas = append(notas, sAviso.Render("la rama ya no existe en git"))
				}
				if b.Vacia {
					notas = append(notas, sTenue.Render("creada vacía"))
				} else if b.Desde != "" {
					notas = append(notas, sTenue.Render("copia de "+b.Desde))
				}
				fila(rama, b.Base, t, hace(b.UltimoUso), strings.Join(notas, sTenue.Render(" · ")))
			}
			if r.actual != "" && r.actual != r.principal && r.local.Base(n, r.actual) == nil {
				linea(c, "  %s", sTenue.Render("La rama "+r.actual+" aún no tiene base: se crea al usarla (marga up)."))
			}
		}
		if huerfanas := r.ramasSinGit(nombres); len(huerfanas) > 0 {
			linea(c, "\n%s Hay %s de ramas que ya no existen en git. Bórralas con: marga ramas limpiar", sAviso.Render("!"),
				plural(len(huerfanas), "base", "bases"))
		}
		return nil
	},
}

// ramasSinGit: pares entorno/rama cuya rama ya no existe en git.
func (r *ctxRamas) ramasSinGit(nombres []string) [][2]string {
	var out [][2]string
	for _, n := range nombres {
		for _, rama := range r.local.Ramas(n) {
			if !r.repo.Existe(rama) {
				out = append(out, [2]string{n, rama})
			}
		}
	}
	return out
}

// borrarBaseRama borra la base de una rama en un entorno y la quita de local.json.
func borrarBaseRama(ctx context.Context, c *cobra.Command, r *ctxRamas, nombre string, e proyecto.Entorno, rama string) error {
	b := r.local.Base(nombre, rama)
	if b == nil {
		if rama == r.principal {
			return sugerencia.Nuevof("marga ramas para ver las bases que sí se pueden borrar", "%s es la rama principal: usa la base de la cadena de conexión de %s y Marga no la borra", rama, nombre)
		}
		return sugerencia.Nuevof("marga ramas para ver qué ramas tienen base", "la rama %s no tiene base propia en %s", rama, nombre)
	}
	admin, baseP, _, err := adminDe(ctx, e)
	if err != nil {
		return err
	}
	defer admin.Close()
	if err := admin.Borrar(ctx, b.Base, baseP, ramaBDFlags.forzar); err != nil {
		var oc *ramabd.ErrOcupada
		if errors.As(err, &oc) {
			lista := make([]string, 0, len(oc.Conexiones))
			for _, cx := range oc.Conexiones {
				lista = append(lista, "    - "+cx.String())
			}
			return sugerencia.Nuevof("ciérralas, o repite con --forzar para que Marga las cierre", "no se borró %s porque hay conexiones abiertas:\n%s", b.Base, strings.Join(lista, "\n"))
		}
		return err
	}
	r.local.QuitarBase(nombre, rama)
	_ = r.p.BorrarSnapshots(claveRama(nombre, b.Base))
	if err := r.p.GuardarLocal(r.local); err != nil {
		return err
	}
	linea(c, "%s Base %s de la rama %s borrada (%s)", sAcento.Render("✓"), sNegri.Render(b.Base), rama, nombre)
	return nil
}

var ramasBorrarCmd = &cobra.Command{
	Use:   "borrar RAMA",
	Short: "Borra la base de datos de una rama",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := abrirRamas(false)
		if err != nil {
			return err
		}
		nombres, ents, err := entornosConRamas(r.p)
		if err != nil {
			return err
		}
		hecho := false
		for _, n := range nombres {
			if r.local.Base(n, args[0]) == nil && len(nombres) > 1 {
				continue
			}
			if err := borrarBaseRama(ctx, c, r, n, ents[n], args[0]); err != nil {
				return err
			}
			hecho = true
		}
		if !hecho {
			return sugerencia.Nuevof("marga ramas para ver qué ramas tienen base", "la rama %s no tiene base propia en %s", args[0], strings.Join(nombres, ", "))
		}
		if args[0] == r.actual {
			linea(c, "%s", sTenue.Render("Es tu rama actual: la próxima vez que la uses (marga up) se vuelve a crear."))
		}
		return nil
	},
}

var ramasReiniciarCmd = &cobra.Command{
	Use:   "reiniciar",
	Short: "Vuelve a crear la base de la rama actual (copia fresca, o vacía con --vacia)",
	Example: `  marga ramas reiniciar              copia otra vez la base de la rama de origen
  marga ramas reiniciar --vacia      base vacía + todas las migraciones de la rama
  marga ramas reiniciar --desde main`,
	Args: cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := abrirRamas(true)
		if err != nil {
			return err
		}
		if r.actual == "" {
			return sugerencia.Nuevo("git no está en ninguna rama (HEAD separado)", "git checkout <rama> y repite")
		}
		if r.actual == r.principal {
			return sugerencia.Nuevof("cámbiate a una rama de trabajo (git checkout -b mi-rama) y repite", "estás en %s, la rama principal: su base es la de la cadena de conexión y Marga no la reinicia", r.actual)
		}
		nombres, ents, err := entornosConRamas(r.p)
		if err != nil {
			return err
		}
		locales, err := r.p.Migraciones()
		if err != nil {
			return err
		}
		for _, n := range nombres {
			if r.local.Base(n, r.actual) != nil {
				if err := borrarBaseRama(ctx, c, r, n, ents[n], r.actual); err != nil {
					return err
				}
			}
			e, err := resolverRama(ctx, c, r.p, n, ents[n], locales)
			if err != nil {
				return err
			}
			if e.RamaGit == r.actual {
				linea(c, "Siguiente: marga up --env %s", n)
			}
			// resolverRama releyó local.json; se recarga para el siguiente entorno.
			if r.local, err = r.p.Local(); err != nil {
				return err
			}
		}
		return nil
	},
}

var ramasLimpiarCmd = &cobra.Command{
	Use:   "limpiar",
	Short: "Borra las bases de las ramas que ya no existen en git",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx := context.Background()
		r, err := abrirRamas(true)
		if err != nil {
			return err
		}
		nombres, ents, err := entornosConRamas(r.p)
		if err != nil {
			return err
		}
		sobran := r.ramasSinGit(nombres)
		if len(sobran) == 0 {
			linea(c, "%s Nada que limpiar: todas las bases son de ramas que existen en git", sAcento.Render("✓"))
			return nil
		}
		if ramaBDFlags.simular {
			linea(c, "Se borrarían %s:", plural(len(sobran), "base", "bases"))
			for _, s := range sobran {
				linea(c, "  %s %s", relleno(s[1], 28), sTenue.Render(r.local.Base(s[0], s[1]).Base+" ("+s[0]+")"))
			}
			linea(c, "%s", sTenue.Render("(simulación: no se borró nada)"))
			return nil
		}
		var fallos []string
		for _, s := range sobran {
			if err := borrarBaseRama(ctx, c, r, s[0], ents[s[0]], s[1]); err != nil {
				linea(c, "  %s %s: %v", sError.Render("✗"), s[1], err)
				fallos = append(fallos, s[1])
			}
		}
		if len(fallos) > 0 {
			return sugerencia.Nuevof("cierra las conexiones a esas bases y repite, o usa --forzar", "no se pudieron borrar: %s", strings.Join(fallos, ", "))
		}
		return nil
	},
}

func init() {
	ramasCmd.PersistentFlags().StringVarP(&ramaBDFlags.env, "env", "e", "", "entorno personal (por defecto todos: dev, local…)")
	for _, cm := range []*cobra.Command{ramasBorrarCmd, ramasReiniciarCmd, ramasLimpiarCmd} {
		cm.Flags().BoolVar(&ramaBDFlags.forzar, "forzar", false, "cierra las conexiones abiertas a la base para poder borrarla")
	}
	ramasReiniciarCmd.Flags().BoolVar(&ramaBDFlags.vacia, "vacia", false, "crea la base vacía y aplica todas las migraciones desde cero")
	ramasReiniciarCmd.Flags().StringVar(&ramaBDFlags.desde, "desde", "", "rama cuya base se copia (por defecto la rama de origen)")
	ramasLimpiarCmd.Flags().BoolVar(&ramaBDFlags.simular, "simular", false, "muestra qué se borraría sin borrar nada")
	ramasCmd.AddCommand(ramasBorrarCmd, ramasReiniciarCmd, ramasLimpiarCmd)
}
