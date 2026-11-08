package mysql_test

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"automarket/internal/domain"
	"automarket/internal/storage/mysql"
)

// TestStore es una prueba de integración contra un MySQL real. Se omite si no
// se define AUTOMARKET_TEST_DSN, de modo que `go test ./...` siga funcionando
// sin base de datos.
//
// Ejemplo:
//
//	AUTOMARKET_TEST_DSN="automarket:automarket@tcp(127.0.0.1:3306)/automarket_test?parseTime=true" go test ./internal/storage/mysql
func TestStore(t *testing.T) {
	dsn := os.Getenv("AUTOMARKET_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTOMARKET_TEST_DSN no está definido")
	}

	store, err := mysql.New(dsn)
	if err != nil {
		t.Fatalf("no se pudo abrir el almacenamiento: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	clean := func() {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Fatalf("no se pudo abrir la conexión de limpieza: %v", err)
		}
		defer db.Close()

		for _, table := range []string{"photos", "audit", "logouts", "sessions", "publications", "users"} {
			if _, err := db.Exec("DELETE FROM " + table); err != nil {
				t.Fatalf("no se pudo limpiar %s: %v", table, err)
			}
		}
		if _, err := db.Exec("UPDATE audit_state SET last_hash = '' WHERE id = 1"); err != nil {
			t.Fatalf("no se pudo reiniciar audit_state: %v", err)
		}
	}
	clean()
	t.Cleanup(clean)

	// Usuarios.
	admin := domain.User{ID: "USR-admin", Username: "admin", Password: "admin", Role: domain.RoleAdministrador}
	if err := store.SaveUser(admin); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}
	if err := store.SaveUser(admin); err == nil {
		t.Fatal("se esperaba error al duplicar el nombre de usuario")
	}
	if !store.HasAdmin() {
		t.Fatal("HasAdmin debería ser true")
	}
	if got, ok := store.GetUserByUsername("admin"); !ok || got.ID != admin.ID {
		t.Fatalf("GetUserByUsername devolvió %+v, %v", got, ok)
	}
	if got := store.GetAllUsers(); len(got) != 1 {
		t.Fatalf("GetAllUsers devolvió %d usuarios", len(got))
	}

	// Fotos.
	photo := domain.Photo{ID: "PHO-1", ContentType: "image/png", Data: []byte("contenido")}
	if err := store.SavePhoto(photo); err != nil {
		t.Fatalf("SavePhoto: %v", err)
	}
	if got, ok := store.GetPhoto(photo.ID); !ok || string(got.Data) != "contenido" {
		t.Fatalf("GetPhoto devolvió %+v, %v", got, ok)
	}

	// Publicaciones.
	pub := domain.Publication{
		ID: "PUB-1", SellerID: admin.ID, Brand: "Toyota", Model: "Corolla", Year: 2020,
		Price: 50000, Phone: "3000000000", Email: "vendedor@example.com",
		Photos: []string{"/media/PHO-1"}, Status: domain.StatusPendiente, CreatedAt: time.Now().UTC(),
	}
	if err := store.SavePublication(pub); err != nil {
		t.Fatalf("SavePublication: %v", err)
	}
	if got, ok := store.GetPublicationByID(pub.ID); !ok || got.Brand != "Toyota" || len(got.Photos) != 1 {
		t.Fatalf("GetPublicationByID devolvió %+v, %v", got, ok)
	}
	pub.Status = domain.StatusPublicado
	if err := store.UpdatePublication(pub); err != nil {
		t.Fatalf("UpdatePublication: %v", err)
	}
	if got, _ := store.GetPublicationByID(pub.ID); got.Status != domain.StatusPublicado {
		t.Fatalf("el estado no se actualizó: %s", got.Status)
	}

	// Sesiones.
	sess := domain.Session{Token: "tok-1", UserID: admin.ID, Username: "admin", Role: domain.RoleAdministrador, ExpiresAt: time.Now().Add(time.Hour).UTC()}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if got, ok := store.GetSessionByToken(sess.Token); !ok || got.UserID != admin.ID {
		t.Fatalf("GetSessionByToken devolvió %+v, %v", got, ok)
	}
	if err := store.SaveLogout(domain.LogoutRecord{Token: sess.Token, UserID: admin.ID, Role: domain.RoleAdministrador, Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("SaveLogout: %v", err)
	}
	if err := store.DeleteSession(sess.Token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, ok := store.GetSessionByToken(sess.Token); ok {
		t.Fatal("la sesión debería estar eliminada")
	}

	// Auditoría encadenada.
	for i, action := range []string{"LOGIN", "CREAR_PUBLICACION"} {
		log := domain.AuditLog{
			ID:        fmt.Sprintf("AUD-%d", i+1),
			Timestamp: time.Now().UTC(),
			Action:    action,
			UserID:    admin.ID,
			Role:      domain.RoleAdministrador,
			Success:   true,
			Details:   "detalle",
		}
		if err := store.SaveAuditLog(log); err != nil {
			t.Fatalf("SaveAuditLog: %v", err)
		}
	}
	if logs := store.GetAllAuditLogs(); len(logs) != 2 {
		t.Fatalf("GetAllAuditLogs devolvió %d entradas", len(logs))
	}
	if !store.VerifyAuditIntegrity() {
		t.Fatal("la cadena de auditoría debería ser íntegra")
	}

	// Alteración deliberada: la verificación debe fallar.
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("no se pudo abrir la conexión: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE audit SET details = 'alterado' ORDER BY seq LIMIT 1"); err != nil {
		t.Fatalf("no se pudo alterar la auditoría: %v", err)
	}
	if store.VerifyAuditIntegrity() {
		t.Fatal("la verificación debería detectar la alteración")
	}

	// Borrado de publicación y de sus fotos.
	if err := store.DeletePublication(pub.ID); err != nil {
		t.Fatalf("DeletePublication: %v", err)
	}
	if _, ok := store.GetPhoto(photo.ID); ok {
		t.Fatal("la foto asociada debería haberse eliminado")
	}
	if err := store.DeletePhotos([]string{"PHO-inexistente"}); err != nil {
		t.Fatalf("DeletePhotos: %v", err)
	}
	if err := store.DeleteUser(admin.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
}
