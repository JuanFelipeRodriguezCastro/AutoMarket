package storage_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"automarket/internal/domain"
	"automarket/internal/storage"
)

func newStorage(t *testing.T, base string) (*storage.Storage, string) {
	t.Helper()

	dir := filepath.Join(base, "data")
	s, err := storage.New(dir)
	if err != nil {
		t.Fatalf("no se pudo crear el almacenamiento: %v", err)
	}
	return s, dir
}

func TestNewCreatesEmptyDataDirectory(t *testing.T) {
	s, dir := newStorage(t, t.TempDir())
	if s == nil {
		t.Fatal("se esperaba un Storage no nulo")
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("se esperaba el directorio de datos creado en %q: %v", dir, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no se pudo leer el directorio de datos: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("no deben crearse archivos hasta que existan datos, hay %d", len(entries))
	}
}

func TestUserPersistsInItsOwnFile(t *testing.T) {
	s, dir := newStorage(t, t.TempDir())

	u := domain.User{ID: "USR-1", Username: "ana", Password: "secret", Role: domain.RoleVendedor}
	if err := s.SaveUser(u); err != nil {
		t.Fatalf("no se pudo guardar el usuario: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "users.json")); err != nil {
		t.Fatalf("se esperaba users.json: %v", err)
	}
	// La separación de persistencia implica que otros archivos no se escriben aún.
	if _, err := os.Stat(filepath.Join(dir, "publications.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publications.json no debería existir todavía (err=%v)", err)
	}

	// Recarga desde disco: el estado debe sobrevivir a un nuevo Storage.
	reloaded, err := storage.New(dir)
	if err != nil {
		t.Fatalf("no se pudo recargar el almacenamiento: %v", err)
	}
	got, ok := reloaded.GetUserByUsername("ana")
	if !ok || got.ID != "USR-1" || got.Role != domain.RoleVendedor {
		t.Fatalf("el usuario no se restauró correctamente: %+v (ok=%v)", got, ok)
	}
}

func TestDuplicateUsernameIsRejected(t *testing.T) {
	s, _ := newStorage(t, t.TempDir())

	u := domain.User{ID: "USR-1", Username: "ana", Password: "secret", Role: domain.RoleVendedor}
	if err := s.SaveUser(u); err != nil {
		t.Fatalf("primer guardado inesperado: %v", err)
	}
	if err := s.SaveUser(u); err == nil {
		t.Fatal("se esperaba error al guardar un username repetido")
	}
}

func TestHasAdmin(t *testing.T) {
	s, _ := newStorage(t, t.TempDir())

	if s.HasAdmin() {
		t.Fatal("no debería existir administrador en un almacenamiento vacío")
	}
	if err := s.SaveUser(domain.User{ID: "USR-1", Username: "root", Password: "x", Role: domain.RoleAdministrador}); err != nil {
		t.Fatalf("guardado inesperado: %v", err)
	}
	if !s.HasAdmin() {
		t.Fatal("debería detectarse el administrador registrado")
	}
}

func TestGetAllUsersAndDeleteUser(t *testing.T) {
	s, _ := newStorage(t, t.TempDir())

	if err := s.SaveUser(domain.User{ID: "USR-1", Username: "ana", Password: "x", Role: domain.RoleVendedor}); err != nil {
		t.Fatalf("guardado inesperado: %v", err)
	}
	if users := s.GetAllUsers(); len(users) != 1 {
		t.Fatalf("se esperaba 1 usuario, se obtuvieron %d", len(users))
	}

	if err := s.DeleteUser("USR-1"); err != nil {
		t.Fatalf("eliminación inesperada de error: %v", err)
	}
	if _, ok := s.GetUserByUsername("ana"); ok {
		t.Fatal("el usuario debía eliminarse")
	}
	if err := s.DeleteUser("USR-1"); err == nil {
		t.Fatal("se esperaba error al eliminar un usuario inexistente")
	}
}

func TestDeletePublication(t *testing.T) {
	s, _ := newStorage(t, t.TempDir())

	pub := domain.Publication{ID: "PUB-1", SellerID: "USR-1", Brand: "Mazda", Model: "3", Year: 2019, Price: 100, Status: domain.StatusPendiente}
	if err := s.SavePublication(pub); err != nil {
		t.Fatalf("guardado inesperado: %v", err)
	}

	if err := s.DeletePublication("PUB-1"); err != nil {
		t.Fatalf("eliminación inesperada de error: %v", err)
	}
	if _, ok := s.GetPublicationByID("PUB-1"); ok {
		t.Fatal("la publicación debía eliminarse")
	}
	if err := s.DeletePublication("PUB-1"); err == nil {
		t.Fatal("se esperaba error al eliminar una publicación inexistente")
	}
}

func TestEachCollectionPersistsInSeparateFile(t *testing.T) {
	s, dir := newStorage(t, t.TempDir())

	if err := s.SaveSession(domain.Session{Token: "TOK-1", UserID: "USR-1", Username: "ana", Role: domain.RoleVendedor, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("guardado de sesión: %v", err)
	}
	if err := s.SaveLogout(domain.LogoutRecord{Token: "TOK-1", UserID: "USR-1", Role: domain.RoleVendedor, Timestamp: time.Now()}); err != nil {
		t.Fatalf("guardado de logout: %v", err)
	}
	if err := s.SavePublication(domain.Publication{ID: "PUB-1", SellerID: "USR-1", Brand: "Mazda", Model: "3", Year: 2019, Price: 100, Status: domain.StatusPendiente}); err != nil {
		t.Fatalf("guardado de publicación: %v", err)
	}
	if err := s.SaveAuditLog(domain.AuditLog{ID: "LOG-1", Action: "CREAR_PUBLICACION", Success: true}); err != nil {
		t.Fatalf("guardado de auditoría: %v", err)
	}

	for _, name := range []string{"logins.json", "logouts.json", "publications.json", "audit.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("se esperaba el archivo %q: %v", name, err)
		}
	}

	reloaded, err := storage.New(dir)
	if err != nil {
		t.Fatalf("no se pudo recargar: %v", err)
	}
	if _, ok := reloaded.GetSessionByToken("TOK-1"); !ok {
		t.Fatal("la sesión no se restauró")
	}
	if pub, ok := reloaded.GetPublicationByID("PUB-1"); !ok || pub.Status != domain.StatusPendiente {
		t.Fatalf("la publicación no se restauró correctamente (ok=%v)", ok)
	}
	if logs := reloaded.GetAllAuditLogs(); len(logs) != 1 {
		t.Fatalf("se esperaba 1 traza de auditoría, se obtuvo %d", len(logs))
	}
}

func TestAuditChainIsIntegrityChecked(t *testing.T) {
	s, dir := newStorage(t, t.TempDir())

	for i, action := range []string{"INICIAR_SESION", "CREAR_PUBLICACION", "APROBAR_PUBLICACION"} {
		log := domain.AuditLog{
			ID:        fmt.Sprintf("LOG-%d", i),
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Action:    action,
			UserID:    "USR-1",
			Role:      domain.RoleAdministrador,
			Success:   true,
			Details:   "detalle",
		}
		if err := s.SaveAuditLog(log); err != nil {
			t.Fatalf("guardado de auditoría %d: %v", i, err)
		}
	}

	if !s.VerifyAuditIntegrity() {
		t.Fatal("la cadena de auditoría recién creada debe ser íntegra")
	}

	logs := s.GetAllAuditLogs()
	if logs[0].Hash == "" || logs[0].PrevHash != "" {
		t.Fatalf("la primera entrada debe iniciar la cadena: %+v", logs[0])
	}
	if logs[1].PrevHash != logs[0].Hash || logs[2].PrevHash != logs[1].Hash {
		t.Fatal("cada entrada debe enlazar con el hash de la anterior")
	}

	// La cadena debe seguir siendo válida tras recargar desde disco.
	reloaded, err := storage.New(dir)
	if err != nil {
		t.Fatalf("no se pudo recargar: %v", err)
	}
	if !reloaded.VerifyAuditIntegrity() {
		t.Fatal("la cadena debe verificarse correctamente tras recargar")
	}

	// Alterar un registro en disco debe romper la cadena.
	path := filepath.Join(dir, "audit.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no se pudo leer audit.json: %v", err)
	}
	tampered := strings.Replace(string(content), `"detalle"`, `"alterado"`, 1)
	if tampered == string(content) {
		t.Fatal("no se encontró el contenido a alterar en audit.json")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatalf("no se pudo alterar audit.json: %v", err)
	}

	broken, err := storage.New(dir)
	if err != nil {
		t.Fatalf("no se pudo recargar el almacenamiento alterado: %v", err)
	}
	if broken.VerifyAuditIntegrity() {
		t.Fatal("una alteración del historial debe detectarse como cadena no íntegra")
	}
}
