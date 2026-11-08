// Command api es el punto de entrada de AutoMarket. Cablea la persistencia
// (MySQL), el servicio de negocio y el servidor HTTP (Gin), y arranca la
// aplicación web. Toda la configuración llega por variables de entorno para
// facilitar el despliegue en un servidor.
package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"automarket/internal/domain"
	"automarket/internal/service"
	"automarket/internal/storage/mysql"
	"automarket/web/handlers"
)

const (
	// defaultAdminUser y defaultAdminPassword permiten el primer arranque.
	defaultAdminUser     = "admin"
	defaultAdminPassword = "admin"
	// defaultPort se usa cuando la plataforma no define PORT (Render sí lo hace).
	defaultPort = "8080"
)

func main() {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		log.Fatal("falta la variable DATABASE_DSN con la cadena de conexión a MySQL")
	}

	store, err := mysql.New(dsn)
	if err != nil {
		log.Fatalf("no se pudo inicializar la base de datos: %v", err)
	}
	defer store.Close()

	svc := service.New(store)
	seedAdmin(svc)

	router := gin.Default()
	if err := router.SetTrustedProxies(nil); err != nil {
		log.Printf("no se pudieron configurar los proxies de confianza: %v", err)
	}
	router.LoadHTMLGlob("web/templates/*.html")
	handlers.NewServer(svc).Register(router)

	addr := ":" + envOr("PORT", defaultPort)
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")

	// En un servidor gestionado (Render, Railway, Fly.io) el TLS lo termina la
	// plataforma. En local se puede activar HTTPS con un certificado propio.
	if certFile != "" && keyFile != "" {
		log.Printf("HTTPS activo en %s", addr)
		if err := router.RunTLS(addr, certFile, keyFile); err != nil {
			log.Fatalf("el servidor HTTPS no pudo arrancar: %v", err)
		}
		return
	}

	log.Printf("HTTP activo en %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("el servidor no pudo arrancar: %v", err)
	}
}

// seedAdmin crea el administrador inicial si todavía no existe ninguno. Las
// credenciales pueden configurarse con las variables AUTOMARKET_ADMIN_USER y
// AUTOMARKET_ADMIN_PASSWORD.
func seedAdmin(svc *service.Service) {
	if svc.HasAdmin() {
		return
	}

	username := envOr("AUTOMARKET_ADMIN_USER", defaultAdminUser)
	password := envOr("AUTOMARKET_ADMIN_PASSWORD", defaultAdminPassword)

	if err := svc.RegisterUser(username, password, domain.RoleAdministrador); err != nil {
		log.Fatalf("no se pudo crear el administrador inicial: %v", err)
	}
	log.Printf("administrador inicial creado (usuario %q)", username)
}

// envOr devuelve el valor de una variable de entorno o un valor por defecto.
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
