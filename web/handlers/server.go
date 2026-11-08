// Package handlers expone la interfaz web de AutoMarket mediante Gin. Es un
// adaptador de entrada/salida: traduce peticiones HTTP en llamadas al servicio
// de negocio y presenta los resultados con plantillas HTML.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"automarket/internal/domain"
	"automarket/internal/service"
)

// sessionCookie es el nombre de la cookie que transporta el token de sesión.
const sessionCookie = "automarket_token"

// sessionCookieMaxAge es la vigencia de la cookie en segundos (24 horas).
const sessionCookieMaxAge = 24 * 60 * 60

// Server agrupa los controladores HTTP y el servicio de negocio del que dependen.
type Server struct {
	svc *service.Service
}

// NewServer construye el servidor HTTP inyectando el servicio de negocio.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// Register monta todas las rutas de la aplicación sobre el enrutador de Gin.
func (s *Server) Register(r *gin.Engine) {
	r.GET("/", s.showCatalog)

	// Las fotos se sirven desde la base de datos mediante su identificador.
	r.GET("/media/:id", s.showPhoto)

	r.GET("/login", s.showLogin)
	r.POST("/login", s.login)
	r.POST("/logout", s.logout)

	r.GET("/register", s.showRegister)
	r.POST("/register", s.register)

	seller := r.Group("/seller", s.requireRole(domain.RoleVendedor))
	seller.GET("", s.showSeller)
	seller.POST("/publications", s.createPublication)
	seller.POST("/publications/:id/sold", s.markAsSold)

	admin := r.Group("/admin", s.requireRole(domain.RoleAdministrador))
	admin.GET("", s.showAdmin)
	admin.GET("/audit", s.showAudit)
	admin.POST("/publications/:id/approve", s.approvePublication)
	admin.POST("/publications/:id/delete", s.deletePublication)
	admin.POST("/users/:id/delete", s.deleteUser)

	r.NoRoute(s.showNotFound)
}

// requireRole aplica el marco AAA (Authentication + Authorization) a un grupo
// de rutas: valida la sesión y exige el rol indicado antes de continuar.
func (s *Server) requireRole(role domain.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(sessionCookie)
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		sess, err := s.svc.Session(token)
		if err != nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		if sess.Role != role {
			c.Redirect(http.StatusFound, "/")
			c.Abort()
			return
		}

		c.Set("token", token)
		c.Set("session", sess)
		c.Next()
	}
}

// ==========================================
// MODELOS DE VISTA
// ==========================================

// pageData alimenta el encabezado y los mensajes comunes de todas las vistas.
type pageData struct {
	Title    string
	LoggedIn bool
	Username string
	Role     domain.Role
	Message  string
	Error    string
}

type catalogView struct {
	pageData
	Publications []domain.Publication
}

type authView struct {
	pageData
}

type sellerView struct {
	pageData
	Publications []domain.Publication
}

type adminView struct {
	pageData
	Pending []domain.Publication
	Users   []domain.User
}

type auditView struct {
	pageData
	Logs        []domain.AuditLog
	IntegrityOK bool
}

// ==========================================
// HELPERS DE VISTA Y CONTEXTO
// ==========================================

// flashMessages traduce códigos de estado de la URL a mensajes legibles. Se usa
// una lista blanca para no reflejar texto arbitrario en la página.
var flashMessages = map[string]string{
	"registered":   "Cuenta creada correctamente. Ya puede iniciar sesión.",
	"published":    "Publicación registrada en estado PENDIENTE de aprobación.",
	"sold":         "Publicación marcada como VENDIDA.",
	"approved":     "Publicación aprobada y visible en el catálogo.",
	"deleted_pub":  "Publicación eliminada.",
	"deleted_user": "Usuario eliminado.",
}

// flash devuelve el mensaje asociado a un código de estado.
func flash(key string) string {
	return flashMessages[key]
}

// currentSession intenta resolver la sesión actual a partir de la cookie.
func (s *Server) currentSession(c *gin.Context) (domain.Session, bool) {
	token, err := c.Cookie(sessionCookie)
	if err != nil || token == "" {
		return domain.Session{}, false
	}
	sess, err := s.svc.Session(token)
	if err != nil {
		return domain.Session{}, false
	}
	return sess, true
}

// basePage construye el modelo común y, si hay sesión, refleja el usuario.
func (s *Server) basePage(c *gin.Context, title string) pageData {
	d := pageData{Title: title}
	if sess, ok := s.currentSession(c); ok {
		d.LoggedIn = true
		d.Username = sess.Username
		d.Role = sess.Role
	}
	return d
}

// sessionFrom recupera la sesión que el middleware dejó en el contexto.
func sessionFrom(c *gin.Context) (domain.Session, bool) {
	value, ok := c.Get("session")
	if !ok {
		return domain.Session{}, false
	}
	sess, ok := value.(domain.Session)
	return sess, ok
}

// tokenFrom recupera el token de sesión validado por el middleware.
func tokenFrom(c *gin.Context) string {
	return c.GetString("token")
}

// homeFor devuelve la página inicial según el rol del usuario autenticado.
func homeFor(role domain.Role) string {
	switch role {
	case domain.RoleAdministrador:
		return "/admin"
	case domain.RoleVendedor:
		return "/seller"
	default:
		return "/"
	}
}
