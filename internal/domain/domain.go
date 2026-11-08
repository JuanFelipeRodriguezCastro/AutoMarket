// Package domain concentra las entidades y tipos del núcleo de AutoMarket.
// Es una capa pura: no depende de la persistencia ni de ningún mecanismo de
// entrada/salida (consola, HTTP, archivos), lo que preserva la separación
// estructural.
package domain

import "time"

// Role representa los roles de usuario permitidos por el sistema.
type Role string

const (
	// RoleVendedor publica vehículos y reporta sus ventas.
	RoleVendedor Role = "VENDEDOR"
	// RoleAdministrador autoriza publicaciones y elimina publicaciones o usuarios.
	RoleAdministrador Role = "ADMINISTRADOR"
)

// PublicationStatus representa el estado del ciclo de vida de una publicación.
type PublicationStatus string

const (
	// StatusPendiente indica que la publicación espera aprobación del administrador.
	StatusPendiente PublicationStatus = "PENDIENTE"
	// StatusPublicado indica que la publicación es visible en el catálogo público.
	StatusPublicado PublicationStatus = "PUBLICADO"
	// StatusVendido indica que el vendedor reportó la venta del vehículo.
	StatusVendido PublicationStatus = "VENDIDO"
)

// MaxPhotosPerPublication es el número máximo de fotos que puede tener una
// publicación.
const MaxPhotosPerPublication = 22

// User almacena los datos de la cuenta de un usuario.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Role     Role   `json:"role"`
}

// Publication almacena la información detallada del vehículo en venta.
type Publication struct {
	ID        string            `json:"id"`
	SellerID  string            `json:"seller_id"`
	Brand     string            `json:"brand"`
	Model     string            `json:"model"`
	Year      int               `json:"year"`
	Price     float64           `json:"price"`
	Phone     string            `json:"phone"`
	Email     string            `json:"email"`
	Photos    []string          `json:"photos"`
	Status    PublicationStatus `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
}

// Session representa la sesión activa de un usuario autenticado.
type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AuditLog almacena un evento para la auditoría del sistema (Accounting - AAA).
// PrevHash y Hash forman una cadena de integridad: cada entrada incluye el hash
// de la anterior, de modo que cualquier alteración del historial es detectable.
type AuditLog struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	UserID    string    `json:"user_id"`
	Role      Role      `json:"role"`
	Success   bool      `json:"success"`
	Details   string    `json:"details"`
	PrevHash  string    `json:"prev_hash"`
	Hash      string    `json:"hash"`
}

// LogoutRecord registra el cierre de una sesión (Accounting - AAA).
type LogoutRecord struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	Role      Role      `json:"role"`
	Timestamp time.Time `json:"timestamp"`
}

// Photo almacena una imagen asociada a una publicación. El contenido binario
// vive en la base de datos; la publicación solo referencia la foto mediante su
// URL pública (/media/<id>).
type Photo struct {
	ID          string `json:"id"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}
