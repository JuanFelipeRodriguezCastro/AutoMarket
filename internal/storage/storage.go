// Package storage implementa la persistencia de AutoMarket sobre archivos JSON
// locales. Mantiene el estado en memoria protegido por un mutex y sincroniza
// cada colección con su propio archivo dentro de un directorio de datos.
// No conoce las reglas de negocio ni la interfaz de usuario.
package storage

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"automarket/internal/domain"
)

// Nombres de los archivos de persistencia (uno por colección).
const (
	usersFile        = "users.json"
	loginsFile       = "logins.json"
	logoutsFile      = "logouts.json"
	publicationsFile = "publications.json"
	auditFile        = "audit.json"
)

// Storage mantiene el estado en memoria y lo sincroniza con archivos JSON.
type Storage struct {
	mu  sync.RWMutex
	dir string

	users        map[string]domain.User
	sessions     map[string]domain.Session
	logouts      []domain.LogoutRecord
	publications map[string]domain.Publication
	auditLogs    []domain.AuditLog
	photos       map[string]domain.Photo
}

// New crea el directorio de datos (si no existe), restaura el estado previo
// desde disco y devuelve un Storage listo para usarse.
func New(dir string) (*Storage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo crear el directorio de datos %q: %w", dir, err)
	}

	s := &Storage{
		dir:          dir,
		users:        make(map[string]domain.User),
		sessions:     make(map[string]domain.Session),
		logouts:      make([]domain.LogoutRecord, 0),
		publications: make(map[string]domain.Publication),
		auditLogs:    make([]domain.AuditLog, 0),
		photos:       make(map[string]domain.Photo),
	}

	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// path devuelve la ruta absoluta de un archivo de datos.
func (s *Storage) path(name string) string {
	return filepath.Join(s.dir, name)
}

// load restaura el estado desde los archivos JSON, ignorando los que aún no
// existen y garantizando que los mapas y slices nunca queden en nil.
func (s *Storage) load() error {
	if err := loadJSON(s.path(usersFile), &s.users); err != nil {
		return err
	}
	if err := loadJSON(s.path(loginsFile), &s.sessions); err != nil {
		return err
	}
	if err := loadJSON(s.path(logoutsFile), &s.logouts); err != nil {
		return err
	}
	if err := loadJSON(s.path(publicationsFile), &s.publications); err != nil {
		return err
	}
	if err := loadJSON(s.path(auditFile), &s.auditLogs); err != nil {
		return err
	}

	if s.users == nil {
		s.users = make(map[string]domain.User)
	}
	if s.sessions == nil {
		s.sessions = make(map[string]domain.Session)
	}
	if s.logouts == nil {
		s.logouts = make([]domain.LogoutRecord, 0)
	}
	if s.publications == nil {
		s.publications = make(map[string]domain.Publication)
	}
	if s.auditLogs == nil {
		s.auditLogs = make([]domain.AuditLog, 0)
	}
	return nil
}

// loadJSON lee y deserializa un archivo JSON. Si el archivo no existe (o está
// vacío) se considera un estado inicial válido y no se altera el destino.
func loadJSON(path string, dest any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("no se pudo leer %q: %w", path, err)
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("no se pudo interpretar %q: %w", path, err)
	}
	return nil
}

// writeJSON serializa un valor y lo escribe en disco de forma síncrona.
func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("no se pudo serializar %q: %w", path, err)
	}
	return os.WriteFile(path, raw, 0o644)
}

// ==========================================
// USUARIOS
// ==========================================

// SaveUser registra un nuevo usuario validando que el username no esté repetido.
func (s *Storage) SaveUser(u domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[u.Username]; exists {
		return fmt.Errorf("el nombre de usuario '%s' ya está registrado", u.Username)
	}
	s.users[u.Username] = u
	return writeJSON(s.path(usersFile), s.users)
}

// GetUserByUsername busca un usuario por su nombre de usuario.
func (s *Storage) GetUserByUsername(username string) (domain.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	u, exists := s.users[username]
	return u, exists
}

// GetAllUsers devuelve una copia de todos los usuarios registrados.
func (s *Storage) GetAllUsers() []domain.User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]domain.User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	return list
}

// HasAdmin indica si ya existe al menos un usuario con rol Administrador.
func (s *Storage) HasAdmin() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if u.Role == domain.RoleAdministrador {
			return true
		}
	}
	return false
}

// DeleteUser elimina un usuario a partir de su ID.
func (s *Storage) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for username, u := range s.users {
		if u.ID == id {
			delete(s.users, username)
			return writeJSON(s.path(usersFile), s.users)
		}
	}
	return fmt.Errorf("el usuario con ID %s no existe", id)
}

// ==========================================
// PUBLICACIONES
// ==========================================

// SavePublication inserta una nueva publicación en memoria y actualiza el disco.
func (s *Storage) SavePublication(p domain.Publication) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.publications[p.ID] = p
	return writeJSON(s.path(publicationsFile), s.publications)
}

// GetPublicationByID busca una publicación por su ID único.
func (s *Storage) GetPublicationByID(id string) (domain.Publication, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, exists := s.publications[id]
	return p, exists
}

// GetAllPublications devuelve una copia de todas las publicaciones registradas.
func (s *Storage) GetAllPublications() []domain.Publication {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]domain.Publication, 0, len(s.publications))
	for _, p := range s.publications {
		list = append(list, p)
	}
	return list
}

// UpdatePublication reemplaza los datos de una publicación existente.
func (s *Storage) UpdatePublication(p domain.Publication) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.publications[p.ID]; !exists {
		return fmt.Errorf("la publicación con ID %s no existe", p.ID)
	}
	s.publications[p.ID] = p
	return writeJSON(s.path(publicationsFile), s.publications)
}

// DeletePublication elimina una publicación a partir de su ID.
func (s *Storage) DeletePublication(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.publications[id]; !exists {
		return fmt.Errorf("la publicación con ID %s no existe", id)
	}
	delete(s.publications, id)
	return writeJSON(s.path(publicationsFile), s.publications)
}

// ==========================================
// SESIONES (LOGINS)
// ==========================================

// SaveSession guarda una sesión activa asociada a un usuario autenticado.
func (s *Storage) SaveSession(sess domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[sess.Token] = sess
	return writeJSON(s.path(loginsFile), s.sessions)
}

// GetSessionByToken busca la sesión activa correspondiente a un token.
func (s *Storage) GetSessionByToken(token string) (domain.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, exists := s.sessions[token]
	return sess, exists
}

// DeleteSession elimina una sesión activa (cierre de sesión).
func (s *Storage) DeleteSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, token)
	return writeJSON(s.path(loginsFile), s.sessions)
}

// ==========================================
// LOGOUTS (ACCOUNTING)
// ==========================================

// SaveLogout registra el cierre de una sesión en el historial de logouts.
func (s *Storage) SaveLogout(r domain.LogoutRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logouts = append(s.logouts, r)
	return writeJSON(s.path(logoutsFile), s.logouts)
}

// ==========================================
// AUDITORÍA (ACCOUNTING)
// ==========================================

// auditHash calcula el hash encadenado de una entrada de auditoría. Incluye el
// hash de la entrada anterior, por lo que alterar cualquier registro rompe la
// cadena de integridad.
func auditHash(prev string, log domain.AuditLog) string {
	canonical := strings.Join([]string{
		prev,
		log.ID,
		log.Timestamp.UTC().Format(time.RFC3339Nano),
		log.Action,
		log.UserID,
		string(log.Role),
		fmt.Sprintf("%t", log.Success),
		log.Details,
	}, "\x1f")

	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", sum)
}

// SaveAuditLog agrega una traza de auditoría al historial general, enlazándola
// con la anterior mediante su hash (cadena de integridad).
func (s *Storage) SaveAuditLog(log domain.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if n := len(s.auditLogs); n > 0 {
		log.PrevHash = s.auditLogs[n-1].Hash
	} else {
		log.PrevHash = ""
	}
	log.Hash = auditHash(log.PrevHash, log)

	s.auditLogs = append(s.auditLogs, log)
	return writeJSON(s.path(auditFile), s.auditLogs)
}

// VerifyAuditIntegrity recalcula la cadena de hashes y comprueba que ninguna
// entrada haya sido alterada, reordenada o eliminada.
func (s *Storage) VerifyAuditIntegrity() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prev := ""
	for _, log := range s.auditLogs {
		if log.PrevHash != prev {
			return false
		}
		if log.Hash != auditHash(log.PrevHash, log) {
			return false
		}
		prev = log.Hash
	}
	return true
}

// GetAllAuditLogs devuelve una copia del historial de eventos de auditoría.
func (s *Storage) GetAllAuditLogs() []domain.AuditLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs := make([]domain.AuditLog, len(s.auditLogs))
	copy(logs, s.auditLogs)
	return logs
}

// ==========================================
// FOTOS
// ==========================================

// SavePhoto guarda una imagen en memoria. Esta implementación se conserva como
// doble de pruebas; la producción usa internal/storage/mysql.
func (s *Storage) SavePhoto(p domain.Photo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.photos[p.ID] = p
	return nil
}

// GetPhoto recupera una imagen por su identificador.
func (s *Storage) GetPhoto(id string) (domain.Photo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, exists := s.photos[id]
	return p, exists
}

// DeletePhotos elimina un conjunto de imágenes.
func (s *Storage) DeletePhotos(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		delete(s.photos, id)
	}
	return nil
}
