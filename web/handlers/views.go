package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"automarket/internal/domain"
)

// ==========================================
// PRESENTACIÓN — envoltorios de vista
// ==========================================
//
// Estos tipos envuelven las entidades del dominio y añaden métodos de formato
// que las plantillas invocan directamente ({{.PriceLabel}}, {{.Cover}}, ...).
// Así el dominio sigue libre de detalles de presentación.

// pubView es una publicación preparada para la interfaz.
type pubView struct {
	domain.Publication
	// SellerName es el nombre del vendedor (solo se completa en administración).
	SellerName string
}

// PriceLabel devuelve el precio con separador de miles: "$ 21.500.000".
func (p pubView) PriceLabel() string {
	return "$ " + thousands(int64(p.Price+0.5))
}

// PhotoLabel devuelve "Sin fotos", "1 foto" o "N fotos".
func (p pubView) PhotoLabel() string {
	return plural(len(p.Photos), "Sin fotos", "foto", "fotos")
}

// Cover devuelve la URL de la primera foto, o cadena vacía si no tiene.
func (p pubView) Cover() string {
	if len(p.Photos) == 0 {
		return ""
	}
	return p.Photos[0]
}

// Title devuelve "Marca Modelo".
func (p pubView) Title() string {
	return p.Brand + " " + p.Model
}

// SearchText es el texto en minúsculas usado por el buscador del catálogo.
func (p pubView) SearchText() string {
	return strings.ToLower(p.Brand + " " + p.Model)
}

// CreatedUnix sirve para ordenar por "Recientes" en el navegador.
func (p pubView) CreatedUnix() int64 {
	return p.CreatedAt.Unix()
}

// StatusLabel traduce el estado a un texto amigable.
func (p pubView) StatusLabel() string {
	switch p.Status {
	case domain.StatusPendiente:
		return "En revisión"
	case domain.StatusPublicado:
		return "Publicado"
	case domain.StatusVendido:
		return "Vendido"
	}
	return string(p.Status)
}

// StatusClass devuelve la clase CSS del distintivo de estado.
func (p pubView) StatusClass() string {
	switch p.Status {
	case domain.StatusPendiente:
		return "pending"
	case domain.StatusPublicado:
		return "live"
	default:
		return "sold"
	}
}

// CanSell indica si el vendedor todavía puede reportar la venta.
func (p pubView) CanSell() bool {
	return p.Status != domain.StatusVendido
}

func toPubViews(pubs []domain.Publication) []pubView {
	out := make([]pubView, len(pubs))
	for i, p := range pubs {
		out[i] = pubView{Publication: p}
	}
	return out
}

// userView es un usuario preparado para la interfaz de administración.
type userView struct {
	domain.User
	IsSelf bool
}

// Initial devuelve la inicial en mayúscula para el avatar.
func (u userView) Initial() string {
	return initial(u.Username)
}

// RoleLabel traduce el rol a un texto amigable.
func (u userView) RoleLabel() string {
	return roleLabel(u.Role)
}

// auditEntry es un evento de auditoría preparado para la interfaz.
type auditEntry struct {
	domain.AuditLog
}

// auditLabels traduce los códigos de acción a textos legibles.
var auditLabels = map[string]string{
	"LOGIN":                "Inicio de sesión",
	"LOGOUT":               "Cierre de sesión",
	"REGISTER":             "Registro",
	"CREAR_PUBLICACION":    "Nueva publicación",
	"APROBAR_PUBLICACION":  "Publicación aprobada",
	"ELIMINAR_PUBLICACION": "Publicación eliminada",
	"MARCAR_VENDIDO":       "Venta reportada",
	"ELIMINAR_USUARIO":     "Usuario eliminado",
	"LISTAR_PENDIENTES":    "Consulta de pendientes",
	"LISTAR_USUARIOS":      "Consulta de usuarios",
	"LISTAR_AUDITORIA":     "Consulta de auditoría",
	"VERIFICAR_AUDITORIA":  "Verificación de integridad",
}

// Label devuelve el nombre legible de la acción (o el código si no se conoce).
func (a auditEntry) Label() string {
	if l, ok := auditLabels[a.Action]; ok {
		return l
	}
	return a.Action
}

// TimeLabel devuelve la fecha y hora del evento: "09 nov 2026 · 14:05".
func (a auditEntry) TimeLabel() string {
	t := a.Timestamp.Local()
	return fmt.Sprintf("%02d %s %d · %02d:%02d", t.Day(), monthsES[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}

func toAuditEntries(logs []domain.AuditLog) []auditEntry {
	out := make([]auditEntry, len(logs))
	for i, l := range logs {
		out[i] = auditEntry{AuditLog: l}
	}
	return out
}

// ==========================================
// HELPERS DE FORMATO
// ==========================================

var monthsES = [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

// thousands formatea un entero con punto como separador de miles.
func thousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// plural elige el texto según la cantidad: cero, singular o plural.
func plural(n int, zero, one, many string) string {
	switch n {
	case 0:
		return zero
	case 1:
		return "1 " + one
	default:
		return strconv.Itoa(n) + " " + many
	}
}

func initial(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func roleLabel(r domain.Role) string {
	switch r {
	case domain.RoleAdministrador:
		return "Administrador"
	case domain.RoleVendedor:
		return "Vendedor"
	}
	return string(r)
}
