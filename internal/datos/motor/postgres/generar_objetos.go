package postgres

// Funciones, vistas, triggers, ENUM y CHECK.
//
// Lo difícil es el orden: PostgreSQL no deja cambiar el tipo de una columna que
// usa una vista, ni borrar una función que usa un trigger. Por eso, para pasar
// de `a` a `b`:
//
//  1. se quitan los triggers afectados,
//  2. se quitan las vistas dependientes (las más externas primero),
//  3. se cambian funciones, tipos y tablas,
//  4. se recrean las vistas (las más internas primero),
//  5. se recrean los triggers.
//
// El DOWN es el mismo algoritmo de `b` a `a`, así que hace exactamente lo inverso.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/lib/pq"
	"github.com/zara-te05/Marga/internal/modelos/esquema"
)

// plan agrupa las sentencias de los objetos que no son tablas.
type plan struct {
	quitarTriggers, quitarVistas, quitarChecks, quitarFunciones []string
	crearTipos, funcionesAntes, funcionesTablas, ponerChecks    []string
	crearVistas, funcionesVistas, crearTriggers, borrarTipos    []string
	creaFunciones                                               bool
	avisos                                                      []string
}

// reemplazable: CREATE OR REPLACE solo sirve si no cambian los argumentos
// (nombres y DEFAULT incluidos), el tipo de retorno ni el tipo de rutina.
func reemplazable(a, b esquema.Funcion) bool {
	return a.Argumentos == b.Argumentos && a.Retorno == b.Retorno && a.Procedimiento == b.Procedimiento
}

// columnasCambiadas: las columnas de `a` que desaparecen o cambian de tipo en `b`.
// Las vistas que las leen hay que quitarlas antes y recrearlas después.
func columnasCambiadas(a, b esquema.Tabla) []string {
	tipos := map[string]string{}
	for _, c := range b.Columnas {
		tipos[c.Nombre] = c.Tipo
	}
	var out []string
	for _, c := range a.Columnas {
		if t, ok := tipos[c.Nombre]; !ok || t != c.Tipo {
			out = append(out, c.Nombre)
		}
	}
	return out
}

func dropFuncion(f esquema.Funcion) string {
	tipo := "FUNCTION"
	if f.Procedimiento {
		tipo = "PROCEDURE"
	}
	return fmt.Sprintf("DROP %s %s(%s);", tipo, f.Nombre, f.Identidad)
}

func dropVista(v esquema.Vista) string {
	if v.Materializada {
		return "DROP MATERIALIZED VIEW " + v.Nombre + ";"
	}
	return "DROP VIEW " + v.Nombre + ";"
}

func sinSaltoFinal(s string) string { return strings.TrimRight(s, "\n") }

// tocados calcula, en el esquema `a`, qué objetos se borran o se tienen que
// quitar y volver a crear para llegar a `b`. Las claves son las de introspeccion
// (tabla:x, vista:x, funcion:f(int), tipo:x).
func tocados(a, b esquema.Esquema) (map[string]bool, map[string]bool) {
	tocado := map[string]bool{}
	reemplazo := map[string]bool{} // funciones que van con DROP + CREATE
	// Tablas: borradas. Columnas: borradas o con otro tipo. Una vista que use la tabla
	// sin decir qué columnas (línea base de antes de guardar columnas, o count(*))
	// se toca solo si se borra la tabla, salvo que no haya información de columnas.
	conColumnas := map[string]bool{} // tablas de las que alguna vista sí registró columnas
	for _, v := range a.Vistas {
		for _, d := range v.Depende {
			if t, _, ok := strings.Cut(strings.TrimPrefix(d, "columna:"), "."); ok && strings.HasPrefix(d, "columna:") {
				conColumnas[t] = true
			}
		}
	}
	for _, ta := range a.Tablas {
		tb := b.Buscar(ta.Nombre)
		if tb == nil {
			tocado[esquema.ClaveTabla(ta.Nombre)] = true
			for _, c := range ta.Columnas {
				tocado["columna:"+ta.Nombre+"."+c.Nombre] = true
			}
			continue
		}
		cols := columnasCambiadas(ta, *tb)
		for _, c := range cols {
			tocado["columna:"+ta.Nombre+"."+c] = true
		}
		if len(cols) > 0 && !conColumnas[ta.Nombre] {
			tocado[esquema.ClaveTabla(ta.Nombre)] = true // sin detalle: por las dudas
		}
	}
	for _, fa := range a.Funciones {
		fb := b.BuscarFuncion(fa.Clave())
		if fb == nil || !reemplazable(fa, *fb) {
			tocado[fa.Clave()] = true
			if fb != nil {
				reemplazo[fa.Clave()] = true
			}
		}
	}
	for _, ta := range a.Tipos {
		if b.BuscarTipo(ta.Nombre) == nil {
			tocado[ta.Clave()] = true
		}
	}
	// Vistas: cambiaron, desaparecen o dependen (directa o indirectamente) de algo tocado.
	// Funciones que devuelven una vista tocada también se recrean.
	for cambio := true; cambio; {
		cambio = false
		for _, va := range a.Vistas {
			if tocado[va.Clave()] {
				continue
			}
			vb := b.BuscarVista(va.Nombre)
			if vb == nil || !esquema.MismaVista(*vb, va) || algunoTocado(va.Depende, tocado) {
				tocado[va.Clave()] = true
				cambio = true
			}
		}
		for _, fa := range a.Funciones {
			if tocado[fa.Clave()] {
				continue
			}
			for _, d := range fa.Depende {
				if strings.HasPrefix(d, "vista:") && tocado[d] {
					tocado[fa.Clave()], reemplazo[fa.Clave()] = true, true
					cambio = true
					break
				}
			}
		}
	}
	return tocado, reemplazo
}

func algunoTocado(deps []string, tocado map[string]bool) bool {
	for _, d := range deps {
		if tocado[d] {
			return true
		}
	}
	return false
}

// triggerTocado: el trigger cambia, desaparece, su tabla o vista se borra o recrea,
// su función se recrea, o nombra una columna que cambia (UPDATE OF col, WHEN …):
// PostgreSQL no deja cambiar el tipo de esa columna con el trigger puesto.
func triggerTocado(t esquema.Trigger, b esquema.Esquema, tocado map[string]bool) bool {
	tb := b.BuscarTrigger(t.Etiqueta())
	if tb == nil || !esquema.MismoTrigger(*tb, t) || tocado[esquema.ClaveTabla(t.Tabla)] ||
		tocado["vista:"+t.Tabla] || (t.Funcion != "" && tocado[t.Funcion]) {
		return true
	}
	for k := range tocado {
		if col, ok := strings.CutPrefix(k, "columna:"+t.Tabla+"."); ok && reMenciona(col).MatchString(t.Definicion) {
			return true
		}
	}
	return false
}

func reMenciona(col string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^a-z0-9_])"?` + regexp.QuoteMeta(col) + `"?($|[^a-z0-9_])`)
}

// ordenVistas ordena las vistas de `vs` para que cada una vaya después de las
// vistas de las que depende (las más internas primero).
func ordenVistas(vs []esquema.Vista) []esquema.Vista {
	por := map[string]esquema.Vista{}
	for _, v := range vs {
		por[v.Clave()] = v
	}
	nombres := make([]string, 0, len(por))
	for k := range por {
		nombres = append(nombres, k)
	}
	sort.Strings(nombres)
	var out []esquema.Vista
	visto := map[string]int{} // 1 = visitando, 2 = listo
	var visitar func(k string)
	visitar = func(k string) {
		if visto[k] != 0 {
			return // ya está, o es un ciclo (PostgreSQL no lo permite en vistas)
		}
		visto[k] = 1
		v := por[k]
		for _, d := range v.Depende {
			if _, ok := por[d]; ok {
				visitar(d)
			}
		}
		visto[k] = 2
		out = append(out, v)
	}
	for _, k := range nombres {
		visitar(k)
	}
	return out
}

func planificar(a, b esquema.Esquema) plan {
	var p plan
	tocado, reemplazo := tocados(a, b)

	// 1) Triggers que desaparecen, cambian o cuelgan de algo que se recrea.
	for _, t := range a.Triggers {
		if triggerTocado(t, b, tocado) {
			p.quitarTriggers = append(p.quitarTriggers, fmt.Sprintf("DROP TRIGGER %s ON %s;", t.Nombre, t.Tabla))
		}
	}

	// 2) Vistas tocadas: las más externas primero (orden inverso de dependencias).
	var vistasA []esquema.Vista
	for _, v := range a.Vistas {
		if tocado[v.Clave()] {
			vistasA = append(vistasA, v)
		}
	}
	ordenA := ordenVistas(vistasA)
	for i := len(ordenA) - 1; i >= 0; i-- {
		v := ordenA[i]
		p.quitarVistas = append(p.quitarVistas, dropVista(v))
		vb := b.BuscarVista(v.Nombre)
		nombre := map[bool]string{false: "La vista", true: "La vista materializada"}[v.Materializada]
		switch {
		case vb == nil:
			p.avisos = append(p.avisos, fmt.Sprintf("Se borra %s %s. Si algo la consulta, dejará de funcionar.", strings.ToLower(nombre), v.Nombre))
		case esquema.MismaVista(*vb, v):
			p.avisos = append(p.avisos, fmt.Sprintf("%s %s se quita y se vuelve a crear porque depende de algo que cambia; si tenía permisos (GRANT), vuelve a darlos.", nombre, v.Nombre))
		}
		if vb != nil && v.Materializada {
			p.avisos = append(p.avisos, fmt.Sprintf("La vista materializada %s se vuelve a llenar al recrearse: puede tardar.", v.Nombre))
		}
	}

	// 3) CHECK que desaparecen o cambian (antes de borrar funciones que puedan usar).
	for _, ta := range a.Tablas {
		tb := b.Buscar(ta.Nombre)
		if tb == nil {
			continue
		}
		defB := map[string]string{}
		for _, c := range tb.Checks {
			defB[c.Nombre] = c.Definicion
		}
		for _, c := range ta.Checks {
			if def, ok := defB[c.Nombre]; !ok || def != c.Definicion {
				p.quitarChecks = append(p.quitarChecks, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s;", ta.Nombre, c.Nombre))
			}
		}
	}

	// 4) Funciones que se borran o se recrean con DROP + CREATE.
	for _, f := range a.Funciones {
		if !tocado[f.Clave()] {
			continue
		}
		p.quitarFunciones = append(p.quitarFunciones, dropFuncion(f))
		if !reemplazo[f.Clave()] {
			usada := []string{}
			for _, t := range a.Triggers {
				if t.Funcion == f.Clave() && b.BuscarTrigger(t.Etiqueta()) != nil {
					usada = append(usada, t.Etiqueta())
				}
			}
			msg := fmt.Sprintf("Se borra %s %s.", map[bool]string{false: "la función", true: "el procedimiento"}[f.Procedimiento], f.Firma())
			if len(usada) > 0 {
				msg += " La usan los triggers " + strings.Join(usada, ", ") + "."
			}
			p.avisos = append(p.avisos, msg)
		}
	}

	// 5) ENUM: crear, agregar valores; quitar o renombrar valores no se puede en automático.
	for _, tb := range b.Tipos {
		ta := a.BuscarTipo(tb.Nombre)
		if ta == nil {
			p.crearTipos = append(p.crearTipos, sinSaltoFinal(DDLEnum(tb)))
			continue
		}
		sents, avs := cambiosEnum(*ta, tb)
		p.crearTipos = append(p.crearTipos, sents...)
		p.avisos = append(p.avisos, avs...)
	}
	for _, ta := range a.Tipos {
		if b.BuscarTipo(ta.Nombre) == nil {
			p.borrarTipos = append(p.borrarTipos, "DROP TYPE "+ta.Nombre+";")
		}
	}

	// 6) Funciones nuevas, cambiadas o recreadas. Las que dependen de una tabla
	// (p. ej. RETURNS SETOF clientes) van después de las tablas; las que dependen
	// de una vista, después de las vistas.
	for _, fb := range b.Funciones {
		fa := a.BuscarFuncion(fb.Clave())
		if fa != nil && !tocado[fb.Clave()] && fa.Definicion == fb.Definicion {
			continue
		}
		p.creaFunciones = true
		s := sinSaltoFinal(DDLFuncion(fb))
		switch {
		case dependeDe(fb.Depende, "vista:"):
			p.funcionesVistas = append(p.funcionesVistas, s)
		case dependeDe(fb.Depende, "tabla:") || dependeDe(fb.Depende, "columna:"):
			p.funcionesTablas = append(p.funcionesTablas, s)
		default:
			p.funcionesAntes = append(p.funcionesAntes, s)
		}
		if fb.SecurityDefiner && !fb.SearchPath {
			p.avisos = append(p.avisos, fmt.Sprintf("%s es SECURITY DEFINER sin SET search_path: quien la llame puede suplantar tablas o funciones. Agrega SET search_path = public, pg_temp.", fb.Firma()))
		}
	}

	// 7) CHECK nuevos o cambiados (después de las funciones: pueden llamarlas).
	for _, tb := range b.Tablas {
		ta := a.Buscar(tb.Nombre)
		defA := map[string]string{}
		if ta != nil {
			for _, c := range ta.Checks {
				defA[c.Nombre] = c.Definicion
			}
		}
		for _, c := range tb.Checks {
			if def, ok := defA[c.Nombre]; ta == nil || !ok || def != c.Definicion {
				p.ponerChecks = append(p.ponerChecks, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;", tb.Nombre, c.Nombre, c.Definicion))
			}
		}
	}

	// 8) Vistas nuevas o recreadas: las más internas primero.
	var vistasB []esquema.Vista
	for _, v := range b.Vistas {
		if a.BuscarVista(v.Nombre) == nil || tocado[v.Clave()] {
			vistasB = append(vistasB, v)
		}
	}
	for _, v := range ordenVistas(vistasB) {
		p.crearVistas = append(p.crearVistas, sinSaltoFinal(DDLVista(v)))
	}

	// 9) Triggers nuevos o recreados.
	for _, t := range b.Triggers {
		ta := a.BuscarTrigger(t.Etiqueta())
		if ta == nil || triggerTocado(*ta, b, tocado) {
			p.crearTriggers = append(p.crearTriggers, sinSaltoFinal(DDLTrigger(t)))
		}
	}
	return p
}

func dependeDe(deps []string, prefijo string) bool {
	for _, d := range deps {
		if strings.HasPrefix(d, prefijo) {
			return true
		}
	}
	return false
}

// cambiosEnum agrega los valores nuevos en su posición. Quitar, renombrar o
// reordenar valores no se puede hacer de forma segura: solo se avisa y se deja
// un comentario en el SQL para hacerlo a mano.
func cambiosEnum(a, b esquema.Enum) ([]string, []string) {
	var out, avisos []string
	enA := map[string]bool{}
	for _, v := range a.Valores {
		enA[v] = true
	}
	enB := map[string]bool{}
	for _, v := range b.Valores {
		enB[v] = true
	}
	// Los valores que siguen existiendo deben conservar su orden relativo.
	var comunesA, comunesB []string
	for _, v := range a.Valores {
		if enB[v] {
			comunesA = append(comunesA, v)
		}
	}
	for _, v := range b.Valores {
		if enA[v] {
			comunesB = append(comunesB, v)
		}
	}
	if strings.Join(comunesA, "\x00") != strings.Join(comunesB, "\x00") {
		avisos = append(avisos, fmt.Sprintf("El orden de los valores del tipo %s cambió: PostgreSQL no permite reordenarlos. Hazlo a mano (crear un tipo nuevo y convertir las columnas).", a.Nombre))
		out = append(out, fmt.Sprintf("-- marga: reordena a mano los valores del tipo %s", a.Nombre))
	}
	for i, v := range b.Valores {
		if enA[v] {
			continue
		}
		pos := ""
		for _, sig := range b.Valores[i+1:] {
			if enA[sig] {
				pos = " BEFORE " + pq.QuoteLiteral(sig)
				break
			}
		}
		out = append(out, fmt.Sprintf("ALTER TYPE %s ADD VALUE %s%s;", b.Nombre, pq.QuoteLiteral(v), pos))
	}
	for _, v := range a.Valores {
		if enB[v] {
			continue
		}
		avisos = append(avisos, fmt.Sprintf("Se quita el valor '%s' del tipo %s: PostgreSQL no permite quitarlo. Si lo renombraste usa ALTER TYPE %s RENAME VALUE; si no, hazlo a mano (y revisa las filas que lo usan).", v, a.Nombre, a.Nombre))
		out = append(out, fmt.Sprintf("-- marga: quita a mano el valor %s del tipo %s (PostgreSQL no tiene DROP VALUE)", pq.QuoteLiteral(v), a.Nombre))
	}
	return out, avisos
}
