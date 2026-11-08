// Package service contiene la lógica de negocio de AutoMarket. Implementa el
// marco de seguridad AAA (Authentication, Authorization, Accounting) sobre las
// reglas del dominio. Es una capa pura: recibe datos por parámetros y retorna
// valores, sin depender de la consola, de HTTP ni de la persistencia concreta.
package service

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"automarket/internal/domain"
)

// Errores de negocio, expuestos para que la capa de entrada pueda identificarlos.
var (
	ErrEmptyCredentials    = errors.New("el usuario y la contraseña no pueden estar vacíos")
	ErrInvalidCredentials  = errors.New("usuario o contraseña incorrectos")
	ErrInvalidSession      = errors.New("sesión no válida o expirada")
	ErrSellerOnly          = errors.New("solo usuarios con rol VENDEDOR pueden publicar vehículos")
	ErrAdminOnly           = errors.New("solo administradores pueden aprobar publicaciones")
	ErrAdminRequired       = errors.New("acceso denegado: requiere permisos de Administrador")
	ErrPublicationNotFound = errors.New("la publicación especificada no existe")
	ErrUserNotFound        = errors.New("el usuario especificado no existe")
	ErrNotPublicationOwner = errors.New("solo el vendedor propietario puede marcar este vehículo como vendido")
	ErrInvalidVehicleData  = errors.New("los datos del vehículo no son válidos")
	ErrTooManyPhotos       = fmt.Errorf("se permiten como máximo %d fotos por publicación", domain.MaxPhotosPerPublication)
)

// Repository define el puerto de persistencia que el servicio necesita.
// Cualquier implementación que cumpla esta interfaz (el Storage real o un doble
// de pruebas) puede inyectarse, invirtiendo la dependencia respecto a la
// persistencia concreta.
type Repository interface {
	SaveUser(domain.User) error
	GetUserByUsername(username string) (domain.User, bool)
	GetAllUsers() []domain.User
	HasAdmin() bool
	DeleteUser(id string) error

	SavePublication(domain.Publication) error
	GetPublicationByID(id string) (domain.Publication, bool)
	GetAllPublications() []domain.Publication
	UpdatePublication(domain.Publication) error
	DeletePublication(id string) error

	SaveSession(domain.Session) error
	GetSessionByToken(token string) (domain.Session, bool)
	DeleteSession(token string) error
	SaveLogout(domain.LogoutRecord) error

	SaveAuditLog(domain.AuditLog) error
	GetAllAuditLogs() []domain.AuditLog
	VerifyAuditIntegrity() bool

	SavePhoto(domain.Photo) error
	GetPhoto(id string) (domain.Photo, bool)
	DeletePhotos(ids []string) error
}

// Service implementa las reglas de negocio de AutoMarket.
type Service struct {
	repo Repository
}

// New construye el servicio inyectando el repositorio de persistencia.
func New(repo Repository) *Service {
	return &Service{repo: repo}
}

// idCounter garantiza identificadores únicos incluso cuando dos llamadas
// ocurren dentro del mismo "tick" del reloj (resolución limitada en Windows).
var idCounter atomic.Uint64

// newID genera un identificador único con el prefijo indicado.
func newID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), idCounter.Add(1))
}

// logAction registra un evento en la traza de auditoría (Accounting - AAA).
func (s *Service) logAction(userID string, role domain.Role, action string, success bool, details string) {
	_ = s.repo.SaveAuditLog(domain.AuditLog{
		ID:        newID("LOG"),
		Timestamp: time.Now(),
		Action:    action,
		UserID:    userID,
		Role:      role,
		Success:   success,
		Details:   details,
	})
}

// authenticate valida que el token corresponda a una sesión existente y vigente
// (Authentication - AAA).
func (s *Service) authenticate(token string) (domain.Session, error) {
	sess, ok := s.repo.GetSessionByToken(token)
	if !ok || time.Now().After(sess.ExpiresAt) {
		return domain.Session{}, ErrInvalidSession
	}
	return sess, nil
}

// Session valida un token y devuelve la sesión asociada. Se expone para que el
// adaptador HTTP pueda autenticar las peticiones.
func (s *Service) Session(token string) (domain.Session, error) {
	return s.authenticate(token)
}

// HasAdmin indica si ya existe al menos un administrador registrado.
func (s *Service) HasAdmin() bool {
	return s.repo.HasAdmin()
}

// SavePhoto persiste una imagen y devuelve su identificador público. El
// adaptador de entrada se encarga de leer y validar el archivo; el servicio solo
// conoce el contenido ya validado.
func (s *Service) SavePhoto(data []byte, contentType string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("la imagen está vacía")
	}
	id := newID("PHO")
	if err := s.repo.SavePhoto(domain.Photo{ID: id, ContentType: contentType, Data: data}); err != nil {
		return "", err
	}
	return id, nil
}

// Photo recupera una imagen por su identificador.
func (s *Service) Photo(id string) (domain.Photo, bool) {
	return s.repo.GetPhoto(id)
}

// DeletePhotos elimina un conjunto de imágenes (por ejemplo, si la publicación
// no llega a crearse).
func (s *Service) DeletePhotos(ids []string) error {
	return s.repo.DeletePhotos(ids)
}

// RegisterUser registra una nueva cuenta en la plataforma.
func (s *Service) RegisterUser(username, password string, role domain.Role) error {
	if username == "" || password == "" {
		return ErrEmptyCredentials
	}

	userID := newID("USR")
	newUser := domain.User{
		ID:       userID,
		Username: username,
		Password: password,
		Role:     role,
	}

	if err := s.repo.SaveUser(newUser); err != nil {
		s.logAction(userID, role, "REGISTER", false, err.Error())
		return err
	}

	s.logAction(userID, role, "REGISTER", true, fmt.Sprintf("Usuario '%s' registrado exitosamente", username))
	return nil
}

// Login autentica al usuario y, si las credenciales son válidas, crea una sesión
// con 24 horas de vigencia (Authentication - AAA).
func (s *Service) Login(username, password string) (domain.Session, error) {
	u, ok := s.repo.GetUserByUsername(username)
	if !ok || u.Password != password {
		s.logAction(username, "", "LOGIN", false, "Credenciales incorrectas")
		return domain.Session{}, ErrInvalidCredentials
	}

	sess := domain.Session{
		Token:     newID("TOK-" + u.Username),
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	if err := s.repo.SaveSession(sess); err != nil {
		s.logAction(u.ID, u.Role, "LOGIN", false, "Error al guardar la sesión")
		return domain.Session{}, err
	}

	s.logAction(u.ID, u.Role, "LOGIN", true, "Inicio de sesión exitoso")
	return sess, nil
}

// Logout invalida la sesión activa y registra el cierre (Accounting - AAA).
func (s *Service) Logout(token string) error {
	sess, ok := s.repo.GetSessionByToken(token)
	if !ok {
		return nil
	}

	s.logAction(sess.UserID, sess.Role, "LOGOUT", true, "Cierre de sesión exitoso")

	if err := s.repo.SaveLogout(domain.LogoutRecord{
		Token:     token,
		UserID:    sess.UserID,
		Role:      sess.Role,
		Timestamp: time.Now(),
	}); err != nil {
		return err
	}

	return s.repo.DeleteSession(token)
}

// CreatePublication registra la solicitud de publicación de un vehículo. Deja la
// publicación en estado PENDIENTE, la asocia al vendedor autenticado y guarda
// las URLs de las fotos adjuntadas (máximo 22).
func (s *Service) CreatePublication(token, brand, model string, year int, price float64, phone, email string, photos []string) (domain.Publication, error) {
	sess, err := s.authenticate(token)
	if err != nil {
		return domain.Publication{}, err
	}

	// Autorización (Authorization - AAA)
	if sess.Role != domain.RoleVendedor {
		s.logAction(sess.UserID, sess.Role, "CREAR_PUBLICACION", false, "Acceso denegado: requiere rol VENDEDOR")
		return domain.Publication{}, ErrSellerOnly
	}

	if brand == "" || model == "" || price <= 0 || year < 1900 {
		s.logAction(sess.UserID, sess.Role, "CREAR_PUBLICACION", false, "Datos de publicación inválidos")
		return domain.Publication{}, ErrInvalidVehicleData
	}

	if len(photos) > domain.MaxPhotosPerPublication {
		s.logAction(sess.UserID, sess.Role, "CREAR_PUBLICACION", false, "Cantidad de fotos excedida")
		return domain.Publication{}, ErrTooManyPhotos
	}

	pub := domain.Publication{
		ID:        newID("PUB"),
		SellerID:  sess.UserID,
		Brand:     brand,
		Model:     model,
		Year:      year,
		Price:     price,
		Phone:     phone,
		Email:     email,
		Photos:    photos,
		Status:    domain.StatusPendiente,
		CreatedAt: time.Now(),
	}

	if err := s.repo.SavePublication(pub); err != nil {
		s.logAction(sess.UserID, sess.Role, "CREAR_PUBLICACION", false, err.Error())
		return domain.Publication{}, err
	}

	s.logAction(sess.UserID, sess.Role, "CREAR_PUBLICACION", true, fmt.Sprintf("Publicación %s creada en estado PENDIENTE", pub.ID))
	return pub, nil
}

// ApprovePublication cambia una publicación PENDIENTE a PUBLICADO, haciéndola
// visible en el catálogo público (solo Administrador).
func (s *Service) ApprovePublication(token, pubID string) error {
	sess, err := s.authenticate(token)
	if err != nil {
		return err
	}

	// Autorización (Authorization - AAA)
	if sess.Role != domain.RoleAdministrador {
		s.logAction(sess.UserID, sess.Role, "APROBAR_PUBLICACION", false, "Acceso denegado: requiere rol ADMINISTRADOR")
		return ErrAdminOnly
	}

	pub, ok := s.repo.GetPublicationByID(pubID)
	if !ok {
		s.logAction(sess.UserID, sess.Role, "APROBAR_PUBLICACION", false, "Publicación no encontrada")
		return ErrPublicationNotFound
	}

	pub.Status = domain.StatusPublicado
	if err := s.repo.UpdatePublication(pub); err != nil {
		return err
	}

	s.logAction(sess.UserID, sess.Role, "APROBAR_PUBLICACION", true, fmt.Sprintf("Publicación %s aprobada y publicada", pubID))
	return nil
}

// MarkAsSold marca una publicación como VENDIDA (solo el vendedor propietario).
func (s *Service) MarkAsSold(token, pubID string) error {
	sess, err := s.authenticate(token)
	if err != nil {
		return err
	}

	pub, ok := s.repo.GetPublicationByID(pubID)
	if !ok {
		return ErrPublicationNotFound
	}

	// Verificación de propiedad (solo el vendedor dueño puede modificar su publicación)
	if pub.SellerID != sess.UserID {
		s.logAction(sess.UserID, sess.Role, "MARCAR_VENDIDO", false, "Intento no autorizado sobre publicación ajena")
		return ErrNotPublicationOwner
	}

	pub.Status = domain.StatusVendido
	if err := s.repo.UpdatePublication(pub); err != nil {
		return err
	}

	s.logAction(sess.UserID, sess.Role, "MARCAR_VENDIDO", true, fmt.Sprintf("Publicación %s marcada como VENDIDA", pubID))
	return nil
}

// DeletePublication elimina una publicación del sistema (solo Administrador).
func (s *Service) DeletePublication(token, pubID string) error {
	sess, err := s.authenticate(token)
	if err != nil {
		return err
	}

	if sess.Role != domain.RoleAdministrador {
		s.logAction(sess.UserID, sess.Role, "ELIMINAR_PUBLICACION", false, "Acceso denegado: requiere rol ADMINISTRADOR")
		return ErrAdminRequired
	}

	if _, ok := s.repo.GetPublicationByID(pubID); !ok {
		return ErrPublicationNotFound
	}

	if err := s.repo.DeletePublication(pubID); err != nil {
		s.logAction(sess.UserID, sess.Role, "ELIMINAR_PUBLICACION", false, err.Error())
		return err
	}

	s.logAction(sess.UserID, sess.Role, "ELIMINAR_PUBLICACION", true, fmt.Sprintf("Publicación %s eliminada", pubID))
	return nil
}

// DeleteUser elimina una cuenta de usuario (solo Administrador).
func (s *Service) DeleteUser(token, userID string) error {
	sess, err := s.authenticate(token)
	if err != nil {
		return err
	}

	if sess.Role != domain.RoleAdministrador {
		s.logAction(sess.UserID, sess.Role, "ELIMINAR_USUARIO", false, "Acceso denegado: requiere rol ADMINISTRADOR")
		return ErrAdminRequired
	}

	if err := s.repo.DeleteUser(userID); err != nil {
		s.logAction(sess.UserID, sess.Role, "ELIMINAR_USUARIO", false, err.Error())
		return ErrUserNotFound
	}

	s.logAction(sess.UserID, sess.Role, "ELIMINAR_USUARIO", true, fmt.Sprintf("Usuario %s eliminado", userID))
	return nil
}

// PublicCatalog retorna las publicaciones visibles para cualquier visitante.
func (s *Service) PublicCatalog() []domain.Publication {
	var catalog []domain.Publication
	for _, pub := range s.repo.GetAllPublications() {
		if pub.Status == domain.StatusPublicado {
			catalog = append(catalog, pub)
		}
	}
	return catalog
}

// SellerPublications retorna las publicaciones del vendedor autenticado.
func (s *Service) SellerPublications(token string) ([]domain.Publication, error) {
	sess, err := s.authenticate(token)
	if err != nil {
		return nil, err
	}

	var own []domain.Publication
	for _, pub := range s.repo.GetAllPublications() {
		if pub.SellerID == sess.UserID {
			own = append(own, pub)
		}
	}
	return own, nil
}

// PendingPublications retorna las publicaciones pendientes de aprobación
// (solo Administrador).
func (s *Service) PendingPublications(token string) ([]domain.Publication, error) {
	sess, err := s.authenticate(token)
	if err != nil {
		return nil, err
	}
	if sess.Role != domain.RoleAdministrador {
		s.logAction(sess.UserID, sess.Role, "LISTAR_PENDIENTES", false, "Acceso denegado: requiere rol ADMINISTRADOR")
		return nil, ErrAdminRequired
	}

	var pending []domain.Publication
	for _, pub := range s.repo.GetAllPublications() {
		if pub.Status == domain.StatusPendiente {
			pending = append(pending, pub)
		}
	}
	return pending, nil
}

// Users retorna la lista de usuarios registrados (solo Administrador).
func (s *Service) Users(token string) ([]domain.User, error) {
	sess, err := s.authenticate(token)
	if err != nil {
		return nil, err
	}
	if sess.Role != domain.RoleAdministrador {
		s.logAction(sess.UserID, sess.Role, "LISTAR_USUARIOS", false, "Acceso denegado: requiere rol ADMINISTRADOR")
		return nil, ErrAdminRequired
	}
	return s.repo.GetAllUsers(), nil
}

// AuditLogs retorna la traza de auditoría del sistema (solo Administrador).
func (s *Service) AuditLogs(token string) ([]domain.AuditLog, error) {
	sess, err := s.authenticate(token)
	if err != nil {
		return nil, err
	}
	if sess.Role != domain.RoleAdministrador {
		s.logAction(sess.UserID, sess.Role, "LISTAR_AUDITORIA", false, "Acceso denegado: requiere rol ADMINISTRADOR")
		return nil, ErrAdminRequired
	}

	return s.repo.GetAllAuditLogs(), nil
}

// AuditIntegrity verifica que la cadena de hashes del historial de auditoría no
// haya sido alterada (solo Administrador).
func (s *Service) AuditIntegrity(token string) (bool, error) {
	sess, err := s.authenticate(token)
	if err != nil {
		return false, err
	}
	if sess.Role != domain.RoleAdministrador {
		s.logAction(sess.UserID, sess.Role, "VERIFICAR_AUDITORIA", false, "Acceso denegado: requiere rol ADMINISTRADOR")
		return false, ErrAdminRequired
	}
	return s.repo.VerifyAuditIntegrity(), nil
}
