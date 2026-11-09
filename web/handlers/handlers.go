package handlers

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"automarket/internal/domain"
)

// ==========================================
// HU-VIS-01 — CATÁLOGO PÚBLICO (VISITANTE)
// ==========================================

// showCatalog presenta el catálogo público con los datos de contacto de cada
// vendedor. Es accesible sin autenticación.
func (s *Server) showCatalog(c *gin.Context) {
	d := catalogView{
		pageData:     s.basePage(c, "Catálogo de vehículos"),
		Publications: toPubViews(s.svc.PublicCatalog()),
	}
	d.Nav = "catalog"
	setFlash(&d.pageData, c.Query("status"))
	c.HTML(http.StatusOK, "catalog.html", d)
}

// ==========================================
// AUTENTICACIÓN
// ==========================================

func (s *Server) showLogin(c *gin.Context) {
	d := authView{pageData: s.basePage(c, "Iniciar sesión")}
	setFlash(&d.pageData, c.Query("status"))
	c.HTML(http.StatusOK, "login.html", d)
}

// login autentica al usuario y crea su sesión (Authentication - AAA).
func (s *Server) login(c *gin.Context) {
	sess, err := s.svc.Login(c.PostForm("username"), c.PostForm("password"))
	if err != nil {
		d := authView{pageData: s.basePage(c, "Iniciar sesión")}
		d.Error = err.Error()
		c.HTML(http.StatusUnauthorized, "login.html", d)
		return
	}

	c.SetCookie(sessionCookie, sess.Token, sessionCookieMaxAge, "/", "", false, true)
	c.Redirect(http.StatusFound, homeFor(sess.Role))
}

// logout invalida la sesión activa y borra la cookie.
func (s *Server) logout(c *gin.Context) {
	if token, err := c.Cookie(sessionCookie); err == nil && token != "" {
		_ = s.svc.Logout(token)
	}
	c.SetCookie(sessionCookie, "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

func (s *Server) showRegister(c *gin.Context) {
	d := authView{pageData: s.basePage(c, "Registro de vendedor")}
	c.HTML(http.StatusOK, "register.html", d)
}

// register da de alta una cuenta de Vendedor (HU-VEN-01).
func (s *Server) register(c *gin.Context) {
	err := s.svc.RegisterUser(c.PostForm("username"), c.PostForm("password"), domain.RoleVendedor)
	if err != nil {
		d := authView{pageData: s.basePage(c, "Registro de vendedor")}
		d.Error = err.Error()
		c.HTML(http.StatusBadRequest, "register.html", d)
		return
	}
	c.Redirect(http.StatusFound, "/login?status=registered")
}

// ==========================================
// HU-VEN-01 — PANEL DEL VENDEDOR
// ==========================================

func (s *Server) showSeller(c *gin.Context) {
	s.renderSeller(c, http.StatusOK, "")
}

// renderSeller presenta el panel del vendedor con sus publicaciones.
func (s *Server) renderSeller(c *gin.Context, status int, errMsg string) {
	pubs, err := s.svc.SellerPublications(tokenFrom(c))

	d := sellerView{
		pageData:     s.basePage(c, "Panel del vendedor"),
		Publications: toPubViews(pubs),
	}
	d.Nav = "seller"
	setFlash(&d.pageData, c.Query("status"))
	d.Error = errMsg
	if err != nil && d.Error == "" {
		d.Error = err.Error()
	}
	c.HTML(status, "seller.html", d)
}

// createPublication registra un vehículo en estado PENDIENTE asociado al
// vendedor, junto con sus fotos (máximo 22).
func (s *Server) createPublication(c *gin.Context) {
	// El formulario es multipart para poder adjuntar las fotos.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)
	if err := c.Request.ParseMultipartForm(uploadMemoryBytes); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		s.renderSeller(c, http.StatusBadRequest, "No se pudo procesar el formulario con las fotos.")
		return
	}

	var files []*multipart.FileHeader
	if c.Request.MultipartForm != nil {
		files = c.Request.MultipartForm.File["photos"]
	}
	if len(files) > domain.MaxPhotosPerPublication {
		s.renderSeller(c, http.StatusBadRequest,
			fmt.Sprintf("Se permiten como máximo %d fotos por publicación.", domain.MaxPhotosPerPublication))
		return
	}

	photos, cleanup, err := s.savePhotos(files)
	if err != nil {
		s.renderSeller(c, http.StatusBadRequest, err.Error())
		return
	}

	// Si la publicación no llega a crearse, se eliminan los archivos subidos.
	saved := false
	defer func() {
		if !saved {
			cleanup()
		}
	}()

	year, errYear := strconv.Atoi(c.PostForm("year"))
	price, errPrice := strconv.ParseFloat(c.PostForm("price"), 64)
	if errYear != nil || errPrice != nil {
		s.renderSeller(c, http.StatusBadRequest, "El año y el precio deben ser valores numéricos válidos.")
		return
	}

	_, err = s.svc.CreatePublication(
		tokenFrom(c),
		c.PostForm("brand"),
		c.PostForm("model"),
		year,
		price,
		c.PostForm("phone"),
		c.PostForm("email"),
		photos,
	)
	if err != nil {
		s.renderSeller(c, http.StatusBadRequest, err.Error())
		return
	}

	saved = true
	c.Redirect(http.StatusFound, "/seller?status=published")
}

// ==========================================
// CARGA DE FOTOS (I/O)
// ==========================================

const (
	// maxPhotoBytes limita el tamaño de cada foto (5 MB).
	maxPhotoBytes = 5 << 20
	// maxUploadBytes limita el tamaño total del formulario (22 fotos de 5 MB).
	maxUploadBytes = 120 << 20
	// uploadMemoryBytes es la memoria usada antes de volcar el formulario a disco.
	uploadMemoryBytes = 32 << 20
	// mediaURLPrefix es la ruta pública desde la que se sirven las fotos.
	mediaURLPrefix = "/media/"
)

// allowedImageTypes son los tipos MIME aceptados. La validación se hace sobre el
// contenido real del archivo, nunca sobre el nombre o el tipo declarado.
var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

// savePhotos valida y persiste las fotos subidas. Devuelve sus URLs públicas y
// una función de limpieza para borrarlas si la publicación no llega a crearse.
func (s *Server) savePhotos(files []*multipart.FileHeader) ([]string, func(), error) {
	noop := func() {}
	if len(files) == 0 {
		return nil, noop, nil
	}

	urls := make([]string, 0, len(files))
	ids := make([]string, 0, len(files))
	cleanup := func() { _ = s.svc.DeletePhotos(ids) }

	for _, header := range files {
		data, contentType, err := readPhoto(header)
		if err != nil {
			cleanup()
			return nil, noop, err
		}
		id, err := s.svc.SavePhoto(data, contentType)
		if err != nil {
			cleanup()
			return nil, noop, errors.New("no se pudo guardar una de las fotos")
		}
		ids = append(ids, id)
		urls = append(urls, mediaURLPrefix+id)
	}
	return urls, cleanup, nil
}

// readPhoto valida el contenido real de la imagen y devuelve sus bytes junto al
// tipo MIME detectado. El adaptador de entrada solo hace I/O; la persistencia
// queda detrás del servicio.
func readPhoto(header *multipart.FileHeader) ([]byte, string, error) {
	if header.Size > maxPhotoBytes {
		return nil, "", fmt.Errorf("cada foto debe pesar como máximo %d MB", maxPhotoBytes>>20)
	}

	src, err := header.Open()
	if err != nil {
		return nil, "", errors.New("no se pudo leer una de las fotos")
	}
	defer src.Close()

	data, err := io.ReadAll(io.LimitReader(src, maxPhotoBytes+1))
	if err != nil {
		return nil, "", errors.New("no se pudo leer una de las fotos")
	}
	if len(data) == 0 {
		return nil, "", errors.New("una de las fotos está vacía")
	}
	if len(data) > maxPhotoBytes {
		return nil, "", fmt.Errorf("cada foto debe pesar como máximo %d MB", maxPhotoBytes>>20)
	}

	contentType := http.DetectContentType(data)
	if !allowedImageTypes[contentType] {
		return nil, "", errors.New("solo se permiten imágenes JPEG, PNG, WEBP o GIF")
	}
	return data, contentType, nil
}

// showPhoto sirve una foto almacenada en la base de datos.
func (s *Server) showPhoto(c *gin.Context) {
	photo, ok := s.svc.Photo(c.Param("id"))
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, photo.ContentType, photo.Data)
}

// markAsSold permite al vendedor reportar la venta de su vehículo.
func (s *Server) markAsSold(c *gin.Context) {
	if err := s.svc.MarkAsSold(tokenFrom(c), c.Param("id")); err != nil {
		s.renderSeller(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/seller?status=sold")
}

// ==========================================
// HU-ADM-01 — PANEL DEL ADMINISTRADOR
// ==========================================

func (s *Server) showAdmin(c *gin.Context) {
	s.renderAdmin(c, http.StatusOK, "")
}

// renderAdmin presenta las publicaciones pendientes y la lista de usuarios.
func (s *Server) renderAdmin(c *gin.Context, status int, errMsg string) {
	token := tokenFrom(c)
	pending, pendingErr := s.svc.PendingPublications(token)
	users, usersErr := s.svc.Users(token)

	d := adminView{
		pageData: s.basePage(c, "Administración"),
		Tab:      adminTab(c),
		Pending:  toPubViews(pending),
		Users:    make([]userView, len(users)),
	}
	d.Nav = "admin"
	setFlash(&d.pageData, c.Query("status"))

	// Se resuelve el nombre de cada vendedor para mostrarlo en las tarjetas.
	names := make(map[string]string, len(users))
	for i, u := range users {
		names[u.ID] = u.Username
		d.Users[i] = userView{User: u, IsSelf: u.Username == d.Username}
	}
	for i := range d.Pending {
		if name, ok := names[d.Pending[i].SellerID]; ok {
			d.Pending[i].SellerName = name
		} else {
			d.Pending[i].SellerName = d.Pending[i].SellerID
		}
	}
	d.Error = errMsg
	if d.Error == "" {
		if pendingErr != nil {
			d.Error = pendingErr.Error()
		} else if usersErr != nil {
			d.Error = usersErr.Error()
		}
	}
	c.HTML(status, "admin.html", d)
}

// approvePublication cambia una publicación PENDIENTE a PUBLICADO.
func (s *Server) approvePublication(c *gin.Context) {
	if err := s.svc.ApprovePublication(tokenFrom(c), c.Param("id")); err != nil {
		s.renderAdmin(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin?status=approved")
}

// deletePublication elimina una publicación del sistema.
func (s *Server) deletePublication(c *gin.Context) {
	if err := s.svc.DeletePublication(tokenFrom(c), c.Param("id")); err != nil {
		s.renderAdmin(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin?status=deleted_pub")
}

// deleteUser elimina una cuenta de usuario.
func (s *Server) deleteUser(c *gin.Context) {
	if err := s.svc.DeleteUser(tokenFrom(c), c.Param("id")); err != nil {
		s.renderAdmin(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusFound, "/admin?tab=users&status=deleted_user")
}

// adminTab devuelve la pestaña activa del panel de administración.
func adminTab(c *gin.Context) string {
	// Un error al eliminar un usuario debe mostrarse en la pestaña de usuarios.
	if c.Query("tab") == "users" || c.FullPath() == "/admin/users/:id/delete" {
		return "users"
	}
	return "pending"
}

// showAudit presenta la traza de auditoría y verifica su integridad.
func (s *Server) showAudit(c *gin.Context) {
	token := tokenFrom(c)
	logs, err := s.svc.AuditLogs(token)
	integrity, integrityErr := s.svc.AuditIntegrity(token)
	if integrityErr != nil && err == nil {
		err = integrityErr
	}

	// Se muestran los eventos más recientes primero.
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	d := auditView{
		pageData:    s.basePage(c, "Auditoría"),
		Logs:        toAuditEntries(logs),
		IntegrityOK: integrity,
	}
	d.Nav = "admin"
	if err != nil {
		d.Error = err.Error()
	}
	c.HTML(http.StatusOK, "audit.html", d)
}

// showNotFound presenta la página de error para rutas inexistentes.
func (s *Server) showNotFound(c *gin.Context) {
	c.HTML(http.StatusNotFound, "error.html", pageData{
		Title: "Página no encontrada",
		Error: "La página solicitada no existe.",
	})
}
