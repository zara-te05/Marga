package cli

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zara-te05/Marga/internal/introspeccion"
	"github.com/zara-te05/Marga/internal/proyecto"
	"github.com/zara-te05/Marga/internal/remoto"
)

var syncRama string

// sincro cruza las migraciones locales con el historial de una rama del servidor.
type sincro struct {
	p          *proyecto.Proyecto
	cli        *remoto.Cliente
	rama       string
	hist       remoto.Historial
	locales    []proyecto.Migracion
	mapa       map[string]string // id local -> hash del servidor (.marga/remoto/)
	enviadas   map[string]remoto.Commit
	porEnviar  []proyecto.Migracion
	faltan     []remoto.Commit   // en el servidor pero no en local (orden cronológico)
	tieneLocal map[string]string // hash -> id local
	// renombrar: migraciones locales cuyo SQL está en el servidor con otro número
	// (porque se fusionaron ramas). `pull` les pone el nombre del servidor.
	renombrar map[string]remoto.Commit
	// ramaNueva: la rama de git todavía no existe en el servidor (la crea `push`).
	ramaNueva bool
}

// analizar cruza lo local con la rama del servidor. Por defecto es la rama de git
// actual; si todavía no existe en el servidor y crear=true (push), se crea desde
// la rama de la que salió en git (o la principal).
func analizar(c *cobra.Command, crear bool) (*sincro, error) {
	p, err := proyectoActual()
	if err != nil {
		return nil, err
	}
	if p.Config.Servidor == "" || p.Config.RepoID == 0 {
		return nil, errors.New("este proyecto no está vinculado a un servidor. Usa `marga link --repo ID`")
	}
	cli, _, err := remoto.Conectado(p.Config.Servidor)
	if err != nil {
		return nil, err
	}
	s := &sincro{p: p, cli: cli, rama: syncRama, enviadas: map[string]remoto.Commit{}, tieneLocal: map[string]string{},
		renombrar: map[string]remoto.Commit{}}
	if s.rama == "" {
		s.rama = ramaDeTrabajo(p)
	}
	if s.hist, err = cli.Historial(p.Config.RepoID, s.rama); err != nil {
		var re *remoto.Error
		if syncRama != "" || !errors.As(err, &re) || re.Status != 404 {
			return nil, err
		}
		ramas, errR := cli.Ramas(p.Config.RepoID)
		if errR != nil {
			return nil, err
		}
		nombres := map[string]bool{}
		for _, r := range ramas {
			nombres[r.Nombre] = true
		}
		if nombres[s.rama] {
			return nil, err
		}
		if !crear {
			s.ramaNueva, s.hist = true, remoto.Historial{}
		} else {
			desde := origenEnServidor(p, s.rama, nombres)
			if err := cli.CrearRama(p.Config.RepoID, s.rama, desde); err != nil {
				return nil, fmt.Errorf("la rama %s no existía en el servidor y no se pudo crear: %w", s.rama, err)
			}
			linea(c, "%s Rama %s creada en el servidor desde %s", sAcento.Render("✓"), sNegri.Render(s.rama), desde)
			if s.hist, err = cli.Historial(p.Config.RepoID, s.rama); err != nil {
				return nil, err
			}
		}
	}
	if s.locales, err = p.Migraciones(); err != nil {
		return nil, err
	}
	if s.mapa, err = p.Remoto(); err != nil {
		return nil, err
	}

	porHash := map[string]remoto.Commit{}
	porMensaje := map[string]remoto.Commit{}
	for _, c := range s.hist.Commits {
		porHash[c.Hash] = c
		porMensaje[c.PrimeraLinea()] = c
	}
	usados := map[string]bool{}
	for _, l := range s.locales {
		if h, ok := s.mapa[l.ID()]; ok {
			if c, ok := porHash[h]; ok {
				s.enviadas[l.ID()], usados[h], s.tieneLocal[h] = c, true, l.ID()
				continue
			}
		}
		if c, ok := porMensaje[l.ID()]; ok && !usados[c.Hash] && (c.Checksum == "" || c.Checksum == l.Checksum) {
			s.enviadas[l.ID()], usados[c.Hash], s.tieneLocal[c.Hash] = c, true, l.ID()
			s.mapa[l.ID()] = c.Hash
			continue
		}
		s.porEnviar = append(s.porEnviar, l)
	}
	// Tercera vía: el mismo SQL (checksum) en el servidor con otro nombre.
	var siguen []proyecto.Migracion
	for _, l := range s.porEnviar {
		encontrado := false
		for i := len(s.hist.Commits) - 1; i >= 0; i-- {
			c := s.hist.Commits[i]
			if !usados[c.Hash] && c.Checksum == l.Checksum {
				usados[c.Hash], s.tieneLocal[c.Hash] = true, l.ID()
				s.renombrar[l.ID()] = c
				encontrado = true
				break
			}
		}
		if !encontrado {
			siguen = append(siguen, l)
		}
	}
	s.porEnviar = siguen
	for i := len(s.hist.Commits) - 1; i >= 0; i-- { // el historial viene del más nuevo al más viejo
		if c := s.hist.Commits[i]; !usados[c.Hash] {
			s.faltan = append(s.faltan, c)
		}
	}
	return s, nil
}

// ---------------------------------------------------------------- push

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Envía al servidor las migraciones locales que aún no tiene",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		s, err := analizar(c, true)
		if err != nil {
			return err
		}
		// ¿Juntaste otra rama en git? Entonces se fusiona también en el servidor.
		fusiono, err := s.fusionarLoDeGit(c)
		if err != nil {
			return err
		}
		if fusiono {
			defer linea(c, "%s", sTenue.Render("Si se renombraron archivos de migrations/, haz commit de ellos en git."))
			if s, err = analizar(c, true); err != nil {
				return err
			}
			if len(s.faltan) > 0 || len(s.renombrar) > 0 {
				if err := s.bajar(c); err != nil {
					return err
				}
				if s, err = analizar(c, true); err != nil {
					return err
				}
			}
		}
		if len(s.faltan) > 0 || len(s.renombrar) > 0 {
			return fmt.Errorf("el servidor tiene cambios en %s que no tienes. Ejecuta `marga pull` primero", s.rama)
		}
		// Avisar si alguien editó una migración que ya se había enviado.
		for id, cm := range s.enviadas {
			for _, l := range s.locales {
				if l.ID() != id {
					continue
				}
				if det, err := s.cli.Commit(s.p.Config.RepoID, cm.Hash); err == nil && proyecto.Checksum(det.SQLUp) != l.Checksum {
					linea(c, "%s %s cambió después de enviarse; el servidor conserva la versión original", sAviso.Render("!"), id)
				}
			}
		}
		if len(s.porEnviar) == 0 {
			linea(c, "%s Nada que enviar: %s está al día con el servidor", sAcento.Render("✓"), s.rama)
			return s.p.GuardarRemoto(s.mapa)
		}
		head := ""
		if s.hist.Head != nil {
			head = *s.hist.Head
		}
		linea(c, "Enviando %s a %s…", plural(len(s.porEnviar), "migración", "migraciones"), sNegri.Render(s.rama))
		var sinEsquema []string
		for _, l := range s.porEnviar {
			snaps, ok := snapshotsDe(s.p, s.locales, l.ID())
			if !ok {
				sinEsquema = append(sinEsquema, l.ID())
			}
			if proyecto.EsVacio(l.Up) {
				return fmt.Errorf("%s: el archivo .up.sql está vacío", l.ID())
			}
			padre := head
			cm, err := s.cli.CrearCommit(s.p.Config.RepoID, remoto.NuevoCommit{
				Branch: s.rama, Mensaje: l.ID(), SQLUp: l.Up, SQLDown: l.Down, PadreEsperado: &padre, Snapshots: snaps,
			})
			if err != nil {
				_ = s.p.GuardarRemoto(s.mapa)
				return fmt.Errorf("%s: %w", l.ID(), err)
			}
			head = cm.Hash
			s.mapa[l.ID()] = cm.Hash
			if err := s.p.GuardarRemoto(s.mapa); err != nil {
				return err
			}
			linea(c, "  %s %s %s", sAcento.Render("↑"), relleno(l.ID(), 40), sTenue.Render(cm.HashCorto))
		}
		if len(sinEsquema) > 0 {
			linea(c, "%s Sin esquema: %s. Aplícalas con `marga up` antes de `push` para que la web vea sus tablas.",
				sAviso.Render("!"), strings.Join(sinEsquema, ", "))
		}
		linea(c, "%s Listo. Haz commit de .marga/remoto/ en tu repositorio", sAcento.Render("✓"))
		return nil
	},
}

// ---------------------------------------------------------------- pull

var reIDMigracion = regexp.MustCompile(`^(\d{4,})_([a-z0-9_]+)$`)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Descarga del servidor las migraciones que no tienes",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		s, err := analizar(c, false)
		if err != nil {
			return err
		}
		if s.ramaNueva {
			linea(c, "%s La rama %s todavía no existe en el servidor: no hay nada que bajar. `marga push` la crea.", sAcento.Render("✓"), sNegri.Render(s.rama))
			return nil
		}
		return s.bajar(c)
	},
}

// bajar trae del servidor lo que falta en local: renombra las migraciones que el
// servidor renumeró al fusionar ramas y escribe las que no tienes.
func (s *sincro) bajar(c *cobra.Command) error {
	var err error
	// Primero: migraciones que el servidor renumeró al fusionar ramas.
	for id, cm := range s.renombrar {
		m := reIDMigracion.FindStringSubmatch(cm.PrimeraLinea())
		var local proyecto.Migracion
		for _, l := range s.locales {
			if l.ID() == id {
				local = l
			}
		}
		if m == nil || id == cm.PrimeraLinea() {
			s.mapa[id] = cm.Hash
			continue
		}
		if err := s.p.Renombrar(local, m[1], m[2]); err != nil {
			return fmt.Errorf("no se pudo renombrar %s: %w", id, err)
		}
		delete(s.mapa, id)
		s.mapa[cm.PrimeraLinea()] = cm.Hash
		linea(c, "  %s %s ahora es %s %s", sAcento.Render("↻"), id, sNegri.Render(cm.PrimeraLinea()), sTenue.Render("(renumerada al fusionar ramas)"))
	}
	if len(s.renombrar) > 0 {
		if err := s.p.GuardarRemoto(s.mapa); err != nil {
			return err
		}
		if s.locales, err = s.p.Migraciones(); err != nil {
			return err
		}
	}
	if len(s.faltan) == 0 {
		linea(c, "%s Ya tienes todo lo de %s", sAcento.Render("✓"), s.rama)
		return s.p.GuardarRemoto(s.mapa)
	}
	// El servidor manda: si una migración local SIN enviar usa la misma versión
	// que una del servidor, se renumera la local (nunca la del servidor).
	maxVersion := 0
	locales := map[string]proyecto.Migracion{}
	for _, l := range s.locales {
		locales[l.Version] = l
		maxVersion = max(maxVersion, versionNum(l.Version))
	}
	for _, cm := range s.faltan {
		if m := reIDMigracion.FindStringSubmatch(cm.PrimeraLinea()); m != nil {
			maxVersion = max(maxVersion, versionNum(m[1]))
		}
	}
	for _, cm := range s.faltan {
		m := reIDMigracion.FindStringSubmatch(cm.PrimeraLinea())
		if m == nil {
			continue
		}
		l, ocupada := locales[m[1]]
		if !ocupada || l.Nombre == m[2] {
			continue
		}
		if _, enviada := s.enviadas[l.ID()]; enviada {
			continue // dos commits del servidor con la misma versión: se resuelve abajo
		}
		maxVersion++
		nueva := fmt.Sprintf("%04d", maxVersion)
		if err := s.p.RenombrarVersion(l, nueva); err != nil {
			return err
		}
		delete(locales, l.Version)
		l.Version = nueva
		locales[nueva] = l
		linea(c, "%s Tu migración sin enviar %s_%s choca con %s del servidor: ahora es %s",
			sAviso.Render("!"), m[1], l.Nombre, cm.PrimeraLinea(), sNegri.Render(l.ID()))
		linea(c, "  Si ya la aplicaste en tu base local, revierte antes (marga down) y vuelve a aplicar.")
	}

	versiones := map[string]string{}
	for v, l := range locales {
		versiones[v] = l.Nombre
	}
	linea(c, "Descargando %s de %s…", plural(len(s.faltan), "migración", "migraciones"), sNegri.Render(s.rama))
	for _, cm := range s.faltan {
		det, err := s.cli.Commit(s.p.Config.RepoID, cm.Hash)
		if err != nil {
			return err
		}
		var version, nombre string
		if m := reIDMigracion.FindStringSubmatch(det.PrimeraLinea()); m != nil {
			nombre = m[2]
			if _, ocupada := versiones[m[1]]; !ocupada {
				version = m[1]
			}
		} else if nombre = proyecto.Slug(det.PrimeraLinea()); nombre == "" {
			nombre = "commit_" + det.HashCorto
		}
		if version == "" {
			maxVersion++
			version = fmt.Sprintf("%04d", maxVersion)
		}
		if _, _, err := s.p.EscribirMigracion(version, nombre, proyecto.Normalizar(det.SQLUp), proyecto.Normalizar(det.SQLDown)); err != nil {
			return err
		}
		versiones[version] = nombre
		s.mapa[version+"_"+nombre] = det.Hash
		if err := s.p.GuardarRemoto(s.mapa); err != nil {
			return err
		}
		linea(c, "  %s %s %s", sAcento.Render("↓"), relleno(version+"_"+nombre, 40), sTenue.Render(det.HashCorto+" · "+det.NombreAutor()))
	}
	linea(c, "%s Listo. Aplica lo nuevo con: marga up", sAcento.Render("✓"))
	return nil
}

// ---------------------------------------------------------------- log

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Muestra el historial de la rama en el servidor",
	Args:  cobra.NoArgs,
	RunE: func(c *cobra.Command, args []string) error {
		s, err := analizar(c, false)
		if err != nil {
			return err
		}
		head := "vacía"
		if s.ramaNueva {
			head = "aún no existe en el servidor (marga push la crea)"
		}
		if s.hist.Head != nil {
			head = "HEAD " + (*s.hist.Head)[:7]
		}
		linea(c, "%s · %s · %s", sNegri.Render(s.rama), plural(s.hist.Total, "commit", "commits"), sTenue.Render(head))
		for _, cm := range s.hist.Commits {
			marca := sAcento.Render("✓")
			if _, ok := s.tieneLocal[cm.Hash]; !ok {
				marca = sAviso.Render("↓")
			}
			msg := cm.PrimeraLinea()
			if len([]rune(msg)) > 48 {
				msg = string([]rune(msg)[:47]) + "…"
			}
			linea(c, "  %s %s %s %s %s", marca, sAcento.Render(cm.HashCorto), relleno(msg, 48),
				relleno(sTenue.Render(cm.NombreAutor()), 12), sTenue.Render(hace(cm.Fecha)))
		}
		if len(s.porEnviar) > 0 {
			ids := make([]string, 0, len(s.porEnviar))
			for _, l := range s.porEnviar {
				ids = append(ids, l.ID())
			}
			linea(c, "%s Sin enviar: %s  (usa marga push)", sAviso.Render("↑"), strings.Join(ids, ", "))
		}
		if len(s.faltan) > 0 {
			linea(c, "%s = está en el servidor pero no en tu carpeta (usa marga pull)", sAviso.Render("↓"))
		}
		return nil
	},
}

var pushForzar bool

// fusionarLoDeGit: si hay migraciones locales sin enviar que ya existen (mismo SQL)
// en otra rama del servidor, es que esa rama se juntó en git. En lugar de subirlas
// como commits nuevos, se fusiona esa rama en el servidor: un solo merge (el de git).
func (s *sincro) fusionarLoDeGit(c *cobra.Command) (bool, error) {
	if syncRama != "" || len(s.porEnviar) == 0 || gitDe(s.p) == nil {
		return false, nil
	}
	faltan := map[string]bool{}
	for _, l := range s.porEnviar {
		faltan[l.Checksum] = true
	}
	ramas, err := s.cli.Ramas(s.p.Config.RepoID)
	if err != nil {
		return false, nil
	}
	historiales := map[string]map[string]bool{}
	for _, r := range ramas {
		if r.Nombre == s.rama || r.UltimoCommit == nil {
			continue
		}
		h, err := s.cli.Historial(s.p.Config.RepoID, r.Nombre)
		if err != nil {
			continue
		}
		cs := map[string]bool{}
		for _, cm := range h.Commits {
			cs[cm.Checksum] = true
		}
		historiales[r.Nombre] = cs
	}
	fusiono := false
	for len(faltan) > 0 {
		// La rama que más migraciones pendientes contiene (en empate, por nombre).
		mejor, n := "", 0
		for nombre, cs := range historiales {
			k := 0
			for ch := range faltan {
				if cs[ch] {
					k++
				}
			}
			if k > n || (k == n && k > 0 && nombre < mejor) {
				mejor, n = nombre, k
			}
		}
		if n == 0 {
			break
		}
		sim, err := s.cli.Fusionar(s.p.Config.RepoID, mejor, s.rama, true, pushForzar)
		if err != nil {
			return fusiono, fmt.Errorf("ya juntaste %s en git, pero no se pudo fusionar en el servidor: %w", mejor, err)
		}
		if sim.Tipo == "conflicto" {
			linea(c, "%s Juntaste %s en git, pero en el servidor %s y %s cambiaron las mismas tablas:", sAviso.Render("!"), sNegri.Render(mejor), mejor, s.rama)
			for _, cf := range sim.Conflictos {
				linea(c, "    %s", sNegri.Render(cf.Tabla))
			}
			return fusiono, errors.New("revisa que esas migraciones funcionen juntas (marga verificar); si sí, repite con: marga push --forzar")
		}
		res, err := s.cli.Fusionar(s.p.Config.RepoID, mejor, s.rama, false, pushForzar)
		if err != nil {
			return fusiono, err
		}
		if res.Tipo != "al_dia" {
			fusiono = true
			tipo := map[string]string{"fast_forward": "fast-forward", "copia": "copiando sus migraciones encima"}[res.Tipo]
			linea(c, "%s %s se fusionó en %s en el servidor (%s), como en git", sAcento.Render("✓"), sNegri.Render(mejor), sNegri.Render(s.rama), tipo)
			for _, r := range res.Renumeradas {
				linea(c, "    %s %s → %s", sAcento.Render("↻"), r.Antes, sNegri.Render(r.Despues))
			}
		}
		for ch := range historiales[mejor] {
			delete(faltan, ch)
		}
		delete(historiales, mejor)
	}
	return fusiono, nil
}

func init() {
	pushCmd.Flags().BoolVar(&pushForzar, "forzar", false, "fusiona en el servidor aunque las ramas toquen las mismas tablas")
	for _, cm := range []*cobra.Command{pushCmd, pullCmd, logCmd} {
		cm.Flags().StringVarP(&syncRama, "rama", "b", "", "rama del servidor (por defecto la rama de git actual)")
	}
}

// snapshotsDe calcula qué tablas cambió una migración comparando el esquema
// guardado tras aplicarla con el de la migración anterior. ok=false si falta alguno.
func snapshotsDe(p *proyecto.Proyecto, locales []proyecto.Migracion, id string) ([]remoto.NuevoSnapshot, bool) {
	idx := -1
	for i, l := range locales {
		if l.ID() == id {
			idx = i
		}
	}
	if idx < 0 {
		return nil, false
	}
	// Ambos esquemas (antes y después) deben venir del MISMO entorno.
	var despues, antes introspeccion.Esquema
	encontrado := false
	for _, env := range p.EntornosConSnapshots() {
		despues, antes = introspeccion.Esquema{}, introspeccion.Esquema{}
		if ok, err := p.LeerSnapshot(env, id, &despues); !ok || err != nil {
			continue
		}
		if idx > 0 {
			if ok, err := p.LeerSnapshot(env, locales[idx-1].ID(), &antes); !ok || err != nil {
				continue
			}
		}
		encontrado = true
		break
	}
	if !encontrado {
		return nil, false
	}
	nombres := map[string]bool{}
	for _, t := range antes.Tablas {
		nombres[t.Nombre] = true
	}
	for _, t := range despues.Tablas {
		nombres[t.Nombre] = true
	}
	var out []remoto.NuevoSnapshot
	for n := range nombres {
		var ddlA, ddlD string
		if t := antes.Buscar(n); t != nil {
			ddlA = t.DDL()
		}
		if t := despues.Buscar(n); t != nil {
			ddlD = t.DDL()
		}
		switch {
		case ddlA == ddlD:
		case ddlD == "":
			out = append(out, remoto.NuevoSnapshot{Tabla: n, Eliminada: true})
		default:
			out = append(out, remoto.NuevoSnapshot{Tabla: n, DDL: ddlD})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tabla < out[j].Tabla })
	return out, true
}

// origenEnServidor elige desde dónde crear en el servidor una rama nueva de git:
// la rama de la que se salió en git si existe en el servidor, si no la principal.
func origenEnServidor(p *proyecto.Proyecto, rama string, enServidor map[string]bool) string {
	if repo := gitDe(p); repo != nil {
		if o := repo.Origen(rama); o != "" && enServidor[o] {
			return o
		}
		if local, err := p.Local(); err == nil {
			if pr, _ := ramaPrincipal(p, repo, local); enServidor[pr] {
				return pr
			}
		}
	}
	if enServidor[p.Config.Rama] {
		return p.Config.Rama
	}
	return "main"
}
