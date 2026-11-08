// Package mysql implementa la persistencia de AutoMarket sobre MySQL/MariaDB.
// Cumple el puerto service.Repository y concentra todo el acceso a datos:
// usuarios, publicaciones, sesiones, cierres de sesión, auditoría y fotos.
//
// La base de datos es la única fuente de verdad: no se usan archivos locales.
package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"automarket/internal/domain"
)

// mediaPrefix es el prefijo público con el que las publicaciones referencian
// sus fotos. Debe coincidir con la ruta expuesta por la capa web.
const mediaPrefix = "/media/"

// Store implementa service.Repository sobre una base de datos MySQL.
type Store struct {
	db *sql.DB
}

// New abre la conexión, configura el pool y aplica las migraciones.
func New(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("el DSN de MySQL está vacío")
	}

	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("DSN de MySQL inválido: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.ClientFoundRows = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir la conexión a MySQL: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(3 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("no se pudo conectar a MySQL: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close libera el pool de conexiones.
func (s *Store) Close() error {
	return s.db.Close()
}

// schema contiene las tablas del sistema. Se aplica de forma idempotente al
// arrancar, de modo que la aplicación pueda desplegarse sin pasos manuales.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id VARCHAR(64) PRIMARY KEY,
		username VARCHAR(255) NOT NULL UNIQUE,
		password VARCHAR(255) NOT NULL,
		role VARCHAR(32) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`CREATE TABLE IF NOT EXISTS publications (
		id VARCHAR(64) PRIMARY KEY,
		seller_id VARCHAR(64) NOT NULL,
		brand VARCHAR(255) NOT NULL,
		model VARCHAR(255) NOT NULL,
		year INT NOT NULL,
		price DOUBLE NOT NULL,
		phone VARCHAR(64) NOT NULL,
		email VARCHAR(255) NOT NULL,
		photos LONGTEXT NOT NULL,
		status VARCHAR(32) NOT NULL,
		created_at DATETIME(6) NOT NULL,
		INDEX idx_publications_status (status),
		INDEX idx_publications_seller (seller_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`CREATE TABLE IF NOT EXISTS sessions (
		token VARCHAR(128) PRIMARY KEY,
		user_id VARCHAR(64) NOT NULL,
		username VARCHAR(255) NOT NULL,
		role VARCHAR(32) NOT NULL,
		expires_at DATETIME(6) NOT NULL,
		INDEX idx_sessions_expires (expires_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`CREATE TABLE IF NOT EXISTS logouts (
		seq BIGINT AUTO_INCREMENT PRIMARY KEY,
		token VARCHAR(128) NOT NULL,
		user_id VARCHAR(64) NOT NULL,
		role VARCHAR(32) NOT NULL,
		created_at DATETIME(6) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`CREATE TABLE IF NOT EXISTS audit (
		seq BIGINT AUTO_INCREMENT PRIMARY KEY,
		id VARCHAR(64) NOT NULL UNIQUE,
		created_at DATETIME(6) NOT NULL,
		action VARCHAR(128) NOT NULL,
		user_id VARCHAR(64) NOT NULL,
		role VARCHAR(32) NOT NULL,
		success BOOLEAN NOT NULL,
		details TEXT NOT NULL,
		prev_hash CHAR(64) NOT NULL,
		hash CHAR(64) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`CREATE TABLE IF NOT EXISTS audit_state (
		id TINYINT PRIMARY KEY,
		last_hash CHAR(64) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	`INSERT IGNORE INTO audit_state (id, last_hash) VALUES (1, '')`,

	`CREATE TABLE IF NOT EXISTS photos (
		id VARCHAR(64) PRIMARY KEY,
		content_type VARCHAR(128) NOT NULL,
		data LONGBLOB NOT NULL,
		created_at DATETIME(6) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

// migrate aplica el esquema de forma idempotente.
func (s *Store) migrate(ctx context.Context) error {
	for _, statement := range schema {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("no se pudo aplicar la migración: %w", err)
		}
	}
	return nil
}

// ==========================================
// USUARIOS
// ==========================================

// SaveUser registra un usuario y traduce la violación de unicidad a un error de
// negocio legible.
func (s *Store) SaveUser(u domain.User) error {
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO users (id, username, password, role) VALUES (?, ?, ?, ?)`,
		u.ID, u.Username, u.Password, string(u.Role))
	if err != nil {
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return fmt.Errorf("el nombre de usuario '%s' ya está registrado", u.Username)
		}
		return fmt.Errorf("no se pudo guardar el usuario: %w", err)
	}
	return nil
}

// GetUserByUsername busca un usuario por su nombre de usuario.
func (s *Store) GetUserByUsername(username string) (domain.User, bool) {
	var u domain.User
	var role string
	err := s.db.QueryRowContext(context.Background(),
		`SELECT id, username, password, role FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Password, &role)
	if err != nil {
		return domain.User{}, false
	}
	u.Role = domain.Role(role)
	return u, true
}

// GetAllUsers devuelve todos los usuarios ordenados por nombre.
func (s *Store) GetAllUsers() []domain.User {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, username, password, role FROM users ORDER BY username`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		var u domain.User
		var role string
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &role); err != nil {
			return nil
		}
		u.Role = domain.Role(role)
		users = append(users, u)
	}
	return users
}

// HasAdmin indica si ya existe al menos un administrador.
func (s *Store) HasAdmin() bool {
	var count int
	err := s.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM users WHERE role = ?`, string(domain.RoleAdministrador)).Scan(&count)
	return err == nil && count > 0
}

// DeleteUser elimina un usuario por su ID.
func (s *Store) DeleteUser(id string) error {
	result, err := s.db.ExecContext(context.Background(), `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("no se pudo eliminar el usuario: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("el usuario con ID %s no existe", id)
	}
	return nil
}

// ==========================================
// PUBLICACIONES
// ==========================================

// encodePhotos serializa la lista de URLs de fotos para almacenarla en la fila.
func encodePhotos(photos []string) string {
	if len(photos) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(photos)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// decodePhotos recupera la lista de URLs de fotos.
func decodePhotos(raw string) []string {
	var photos []string
	if err := json.Unmarshal([]byte(raw), &photos); err != nil {
		return nil
	}
	return photos
}

// scanner abstrae sql.Row y sql.Rows para compartir el escaneo de publicaciones.
type scanner interface {
	Scan(dest ...any) error
}

// scanPublication convierte una fila en una publicación del dominio.
func scanPublication(row scanner) (domain.Publication, error) {
	var p domain.Publication
	var photos, status string
	if err := row.Scan(&p.ID, &p.SellerID, &p.Brand, &p.Model, &p.Year, &p.Price,
		&p.Phone, &p.Email, &photos, &status, &p.CreatedAt); err != nil {
		return domain.Publication{}, err
	}
	p.Photos = decodePhotos(photos)
	p.Status = domain.PublicationStatus(status)
	p.CreatedAt = p.CreatedAt.UTC()
	return p, nil
}

// SavePublication inserta una nueva publicación.
func (s *Store) SavePublication(p domain.Publication) error {
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO publications
		 (id, seller_id, brand, model, year, price, phone, email, photos, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.SellerID, p.Brand, p.Model, p.Year, p.Price, p.Phone, p.Email,
		encodePhotos(p.Photos), string(p.Status), p.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("no se pudo guardar la publicación: %w", err)
	}
	return nil
}

// GetPublicationByID busca una publicación por su ID.
func (s *Store) GetPublicationByID(id string) (domain.Publication, bool) {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, seller_id, brand, model, year, price, phone, email, photos, status, created_at
		 FROM publications WHERE id = ?`, id)
	p, err := scanPublication(row)
	if err != nil {
		return domain.Publication{}, false
	}
	return p, true
}

// GetAllPublications devuelve todas las publicaciones ordenadas por fecha.
func (s *Store) GetAllPublications() []domain.Publication {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, seller_id, brand, model, year, price, phone, email, photos, status, created_at
		 FROM publications ORDER BY created_at, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	publications := make([]domain.Publication, 0)
	for rows.Next() {
		p, err := scanPublication(rows)
		if err != nil {
			return nil
		}
		publications = append(publications, p)
	}
	return publications
}

// UpdatePublication reemplaza los datos de una publicación existente.
func (s *Store) UpdatePublication(p domain.Publication) error {
	result, err := s.db.ExecContext(context.Background(),
		`UPDATE publications
		 SET seller_id = ?, brand = ?, model = ?, year = ?, price = ?, phone = ?, email = ?,
		     photos = ?, status = ?, created_at = ?
		 WHERE id = ?`,
		p.SellerID, p.Brand, p.Model, p.Year, p.Price, p.Phone, p.Email,
		encodePhotos(p.Photos), string(p.Status), p.CreatedAt.UTC(), p.ID)
	if err != nil {
		return fmt.Errorf("no se pudo actualizar la publicación: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("la publicación con ID %s no existe", p.ID)
	}
	return nil
}

// DeletePublication elimina la publicación y sus fotos asociadas.
func (s *Store) DeletePublication(id string) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var photosRaw string
	if err := tx.QueryRowContext(ctx,
		`SELECT photos FROM publications WHERE id = ? FOR UPDATE`, id).Scan(&photosRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("la publicación con ID %s no existe", id)
		}
		return err
	}

	for _, url := range decodePhotos(photosRaw) {
		if photoID := photoIDFromURL(url); photoID != "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM photos WHERE id = ?`, photoID); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM publications WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// photoIDFromURL extrae el identificador de una URL /media/<id>.
func photoIDFromURL(url string) string {
	if !strings.HasPrefix(url, mediaPrefix) {
		return ""
	}
	return strings.TrimPrefix(url, mediaPrefix)
}

// ==========================================
// SESIONES (LOGINS)
// ==========================================

// SaveSession guarda o actualiza una sesión activa.
func (s *Store) SaveSession(sess domain.Session) error {
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO sessions (token, user_id, username, role, expires_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   user_id = VALUES(user_id), username = VALUES(username),
		   role = VALUES(role), expires_at = VALUES(expires_at)`,
		sess.Token, sess.UserID, sess.Username, string(sess.Role), sess.ExpiresAt.UTC())
	if err != nil {
		return fmt.Errorf("no se pudo guardar la sesión: %w", err)
	}
	return nil
}

// GetSessionByToken busca una sesión activa por su token.
func (s *Store) GetSessionByToken(token string) (domain.Session, bool) {
	var sess domain.Session
	var role string
	err := s.db.QueryRowContext(context.Background(),
		`SELECT token, user_id, username, role, expires_at FROM sessions WHERE token = ?`, token).
		Scan(&sess.Token, &sess.UserID, &sess.Username, &role, &sess.ExpiresAt)
	if err != nil {
		return domain.Session{}, false
	}
	sess.Role = domain.Role(role)
	sess.ExpiresAt = sess.ExpiresAt.UTC()
	return sess, true
}

// DeleteSession elimina una sesión activa.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// SaveLogout registra el cierre de una sesión.
func (s *Store) SaveLogout(r domain.LogoutRecord) error {
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO logouts (token, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
		r.Token, r.UserID, string(r.Role), r.Timestamp.UTC())
	if err != nil {
		return fmt.Errorf("no se pudo registrar el cierre de sesión: %w", err)
	}
	return nil
}

// ==========================================
// AUDITORÍA (ACCOUNTING)
// ==========================================

// auditHash calcula el hash encadenado de una entrada de auditoría.
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

// SaveAuditLog agrega una entrada encadenada. La transacción y el bloqueo de
// audit_state serializan las escrituras concurrentes.
func (s *Store) SaveAuditLog(log domain.AuditLog) error {
	ctx := context.Background()
	log.Timestamp = log.Timestamp.UTC().Truncate(time.Microsecond)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var prev string
	if err := tx.QueryRowContext(ctx,
		`SELECT last_hash FROM audit_state WHERE id = 1 FOR UPDATE`).Scan(&prev); err != nil {
		return fmt.Errorf("no se pudo leer el estado de auditoría: %w", err)
	}

	log.PrevHash = prev
	log.Hash = auditHash(prev, log)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit
		 (id, created_at, action, user_id, role, success, details, prev_hash, hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.Timestamp, log.Action, log.UserID, string(log.Role),
		log.Success, log.Details, log.PrevHash, log.Hash); err != nil {
		return fmt.Errorf("no se pudo guardar la auditoría: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE audit_state SET last_hash = ? WHERE id = 1`, log.Hash); err != nil {
		return fmt.Errorf("no se pudo actualizar el estado de auditoría: %w", err)
	}
	return tx.Commit()
}

// GetAllAuditLogs devuelve la traza completa en orden de inserción.
func (s *Store) GetAllAuditLogs() []domain.AuditLog {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, created_at, action, user_id, role, success, details, prev_hash, hash
		 FROM audit ORDER BY seq`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	logs := make([]domain.AuditLog, 0)
	for rows.Next() {
		var log domain.AuditLog
		var role string
		if err := rows.Scan(&log.ID, &log.Timestamp, &log.Action, &log.UserID, &role,
			&log.Success, &log.Details, &log.PrevHash, &log.Hash); err != nil {
			return nil
		}
		log.Role = domain.Role(role)
		log.Timestamp = log.Timestamp.UTC()
		logs = append(logs, log)
	}
	return logs
}

// VerifyAuditIntegrity recalcula la cadena y la compara con lo almacenado. El
// último hash de audit_state detecta además el borrado de las últimas entradas.
func (s *Store) VerifyAuditIntegrity() bool {
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, created_at, action, user_id, role, success, details, prev_hash, hash
		 FROM audit ORDER BY seq`)
	if err != nil {
		return false
	}
	defer rows.Close()

	prev := ""
	for rows.Next() {
		var log domain.AuditLog
		var role string
		if err := rows.Scan(&log.ID, &log.Timestamp, &log.Action, &log.UserID, &role,
			&log.Success, &log.Details, &log.PrevHash, &log.Hash); err != nil {
			return false
		}
		log.Role = domain.Role(role)
		log.Timestamp = log.Timestamp.UTC()

		if log.PrevHash != prev {
			return false
		}
		if log.Hash != auditHash(log.PrevHash, log) {
			return false
		}
		prev = log.Hash
	}
	if err := rows.Err(); err != nil {
		return false
	}

	var last string
	if err := s.db.QueryRowContext(ctx,
		`SELECT last_hash FROM audit_state WHERE id = 1`).Scan(&last); err != nil {
		return false
	}
	return last == prev
}

// ==========================================
// FOTOS
// ==========================================

// SavePhoto guarda el contenido binario de una imagen.
func (s *Store) SavePhoto(p domain.Photo) error {
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO photos (id, content_type, data, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.ContentType, p.Data, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("no se pudo guardar la foto: %w", err)
	}
	return nil
}

// GetPhoto recupera una imagen por su ID.
func (s *Store) GetPhoto(id string) (domain.Photo, bool) {
	var p domain.Photo
	err := s.db.QueryRowContext(context.Background(),
		`SELECT id, content_type, data FROM photos WHERE id = ?`, id).
		Scan(&p.ID, &p.ContentType, &p.Data)
	if err != nil {
		return domain.Photo{}, false
	}
	return p, true
}

// DeletePhotos elimina un conjunto de imágenes.
func (s *Store) DeletePhotos(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM photos WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("no se pudieron eliminar las fotos: %w", err)
	}
	return nil
}
