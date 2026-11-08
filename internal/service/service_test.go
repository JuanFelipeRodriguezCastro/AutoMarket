package service_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"automarket/internal/domain"
	"automarket/internal/service"
)

// fakeRepo es un doble en memoria de service.Repository. Permite probar la
// lógica de negocio sin tocar el sistema de archivos ni ningún mecanismo de I/O.
type fakeRepo struct {
	users        map[string]domain.User
	sessions     map[string]domain.Session
	publications map[string]domain.Publication
	logouts      []domain.LogoutRecord
	auditLogs    []domain.AuditLog
	photos       map[string]domain.Photo
	auditIntact  bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:        make(map[string]domain.User),
		sessions:     make(map[string]domain.Session),
		publications: make(map[string]domain.Publication),
		photos:       make(map[string]domain.Photo),
		auditIntact:  true,
	}
}

func (f *fakeRepo) SaveUser(u domain.User) error {
	if _, ok := f.users[u.Username]; ok {
		return errors.New("el nombre de usuario ya está registrado")
	}
	f.users[u.Username] = u
	return nil
}

func (f *fakeRepo) GetUserByUsername(username string) (domain.User, bool) {
	u, ok := f.users[username]
	return u, ok
}

func (f *fakeRepo) GetAllUsers() []domain.User {
	list := make([]domain.User, 0, len(f.users))
	for _, u := range f.users {
		list = append(list, u)
	}
	return list
}

func (f *fakeRepo) HasAdmin() bool {
	for _, u := range f.users {
		if u.Role == domain.RoleAdministrador {
			return true
		}
	}
	return false
}

func (f *fakeRepo) DeleteUser(id string) error {
	for username, u := range f.users {
		if u.ID == id {
			delete(f.users, username)
			return nil
		}
	}
	return errors.New("el usuario no existe")
}

func (f *fakeRepo) SavePublication(p domain.Publication) error {
	f.publications[p.ID] = p
	return nil
}

func (f *fakeRepo) GetPublicationByID(id string) (domain.Publication, bool) {
	p, ok := f.publications[id]
	return p, ok
}

func (f *fakeRepo) GetAllPublications() []domain.Publication {
	list := make([]domain.Publication, 0, len(f.publications))
	for _, p := range f.publications {
		list = append(list, p)
	}
	return list
}

func (f *fakeRepo) UpdatePublication(p domain.Publication) error {
	if _, ok := f.publications[p.ID]; !ok {
		return errors.New("la publicación no existe")
	}
	f.publications[p.ID] = p
	return nil
}

func (f *fakeRepo) DeletePublication(id string) error {
	if _, ok := f.publications[id]; !ok {
		return errors.New("la publicación no existe")
	}
	delete(f.publications, id)
	return nil
}

func (f *fakeRepo) SaveSession(s domain.Session) error {
	f.sessions[s.Token] = s
	return nil
}

func (f *fakeRepo) GetSessionByToken(token string) (domain.Session, bool) {
	s, ok := f.sessions[token]
	return s, ok
}

func (f *fakeRepo) DeleteSession(token string) error {
	delete(f.sessions, token)
	return nil
}

func (f *fakeRepo) SaveLogout(r domain.LogoutRecord) error {
	f.logouts = append(f.logouts, r)
	return nil
}

func (f *fakeRepo) SaveAuditLog(l domain.AuditLog) error {
	f.auditLogs = append(f.auditLogs, l)
	return nil
}

func (f *fakeRepo) GetAllAuditLogs() []domain.AuditLog {
	return f.auditLogs
}

func (f *fakeRepo) VerifyAuditIntegrity() bool {
	return f.auditIntact
}

func (f *fakeRepo) SavePhoto(p domain.Photo) error {
	f.photos[p.ID] = p
	return nil
}

func (f *fakeRepo) GetPhoto(id string) (domain.Photo, bool) {
	p, ok := f.photos[id]
	return p, ok
}

func (f *fakeRepo) DeletePhotos(ids []string) error {
	for _, id := range ids {
		delete(f.photos, id)
	}
	return nil
}

// loginUser registra y autentica un usuario, devolviendo su sesión activa.
func loginUser(t *testing.T, svc *service.Service, username string, role domain.Role) domain.Session {
	t.Helper()

	if err := svc.RegisterUser(username, "secret", role); err != nil {
		t.Fatalf("no se pudo registrar %q: %v", username, err)
	}
	sess, err := svc.Login(username, "secret")
	if err != nil {
		t.Fatalf("no se pudo autenticar %q: %v", username, err)
	}
	return sess
}

func TestRegisterUserRejectsEmptyCredentials(t *testing.T) {
	svc := service.New(newFakeRepo())

	if err := svc.RegisterUser("", "secret", domain.RoleVendedor); !errors.Is(err, service.ErrEmptyCredentials) {
		t.Fatalf("se esperaba ErrEmptyCredentials, se obtuvo %v", err)
	}
	if err := svc.RegisterUser("ana", "", domain.RoleVendedor); !errors.Is(err, service.ErrEmptyCredentials) {
		t.Fatalf("se esperaba ErrEmptyCredentials, se obtuvo %v", err)
	}
}

func TestLoginWithInvalidCredentials(t *testing.T) {
	svc := service.New(newFakeRepo())

	if err := svc.RegisterUser("ana", "correcta", domain.RoleVendedor); err != nil {
		t.Fatalf("registro inesperado: %v", err)
	}

	if _, err := svc.Login("ana", "incorrecta"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("se esperaba ErrInvalidCredentials, se obtuvo %v", err)
	}
	if _, err := svc.Login("desconocido", "x"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("se esperaba ErrInvalidCredentials, se obtuvo %v", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo)
	repo.sessions["expirada"] = domain.Session{
		Token:     "expirada",
		UserID:    "USR-1",
		Role:      domain.RoleAdministrador,
		ExpiresAt: time.Now().Add(-time.Hour),
	}

	if _, err := svc.PendingPublications("expirada"); !errors.Is(err, service.ErrInvalidSession) {
		t.Fatalf("PendingPublications: se esperaba ErrInvalidSession, se obtuvo %v", err)
	}
	if _, err := svc.AuditLogs("expirada"); !errors.Is(err, service.ErrInvalidSession) {
		t.Fatalf("AuditLogs: se esperaba ErrInvalidSession, se obtuvo %v", err)
	}
}

func TestCreatePublicationRequiresSellerRole(t *testing.T) {
	svc := service.New(newFakeRepo())
	admin := loginUser(t, svc, "admin", domain.RoleAdministrador)

	_, err := svc.CreatePublication(admin.Token, "Toyota", "Corolla", 2020, 50000, "300", "a@a.com", nil)
	if !errors.Is(err, service.ErrSellerOnly) {
		t.Fatalf("se esperaba ErrSellerOnly, se obtuvo %v", err)
	}
}

func TestCreatePublicationLeavesPendingAndAssociated(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)

	pub, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 45000000, "3001234567", "vendedor@a.com", nil)
	if err != nil {
		t.Fatalf("creación inesperada de error: %v", err)
	}
	if pub.Status != domain.StatusPendiente {
		t.Fatalf("se esperaba estado PENDIENTE, se obtuvo %q", pub.Status)
	}
	if pub.SellerID != seller.UserID {
		t.Fatalf("la publicación debe asociarse al vendedor %q, se obtuvo %q", seller.UserID, pub.SellerID)
	}
	if got := svc.PublicCatalog(); len(got) != 0 {
		t.Fatalf("una publicación pendiente no debe aparecer en el catálogo público, se obtuvo %d elementos", len(got))
	}
}

func TestCreatePublicationRejectsInvalidVehicleData(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)

	if _, err := svc.CreatePublication(seller.Token, "", "3", 2019, 100, "300", "a@a.com", nil); !errors.Is(err, service.ErrInvalidVehicleData) {
		t.Fatalf("se esperaba ErrInvalidVehicleData, se obtuvo %v", err)
	}
	if _, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 0, "300", "a@a.com", nil); !errors.Is(err, service.ErrInvalidVehicleData) {
		t.Fatalf("se esperaba ErrInvalidVehicleData, se obtuvo %v", err)
	}
}

func TestApprovePublicationRequiresAdmin(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)

	pub, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 45000000, "300", "a@a.com", nil)
	if err != nil {
		t.Fatalf("creación inesperada de error: %v", err)
	}

	if err := svc.ApprovePublication(seller.Token, pub.ID); !errors.Is(err, service.ErrAdminOnly) {
		t.Fatalf("un vendedor no debe poder aprobar: se esperaba ErrAdminOnly, se obtuvo %v", err)
	}
}

func TestApprovePublicationMakesItVisibleInCatalog(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)
	admin := loginUser(t, svc, "admin", domain.RoleAdministrador)

	pub, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 45000000, "300", "a@a.com", nil)
	if err != nil {
		t.Fatalf("creación inesperada de error: %v", err)
	}

	pending, err := svc.PendingPublications(admin.Token)
	if err != nil || len(pending) != 1 {
		t.Fatalf("se esperaba 1 publicación pendiente, se obtuvo %d (err=%v)", len(pending), err)
	}

	if err := svc.ApprovePublication(admin.Token, pub.ID); err != nil {
		t.Fatalf("aprobación inesperada de error: %v", err)
	}

	catalog := svc.PublicCatalog()
	if len(catalog) != 1 || catalog[0].ID != pub.ID {
		t.Fatalf("la publicación aprobada debe ser visible en el catálogo público")
	}

	if err := svc.ApprovePublication(admin.Token, "PUB-inexistente"); !errors.Is(err, service.ErrPublicationNotFound) {
		t.Fatalf("se esperaba ErrPublicationNotFound, se obtuvo %v", err)
	}
}

func TestMarkAsSoldOnlyByOwner(t *testing.T) {
	svc := service.New(newFakeRepo())
	owner := loginUser(t, svc, "dueno", domain.RoleVendedor)
	other := loginUser(t, svc, "otro", domain.RoleVendedor)

	pub, err := svc.CreatePublication(owner.Token, "Mazda", "3", 2019, 45000000, "300", "a@a.com", nil)
	if err != nil {
		t.Fatalf("creación inesperada de error: %v", err)
	}

	if err := svc.MarkAsSold(other.Token, pub.ID); !errors.Is(err, service.ErrNotPublicationOwner) {
		t.Fatalf("se esperaba ErrNotPublicationOwner, se obtuvo %v", err)
	}

	if err := svc.MarkAsSold(owner.Token, pub.ID); err != nil {
		t.Fatalf("el propietario debe poder marcar como vendido: %v", err)
	}

	// El catálogo solo expone publicaciones PUBLICADO, por lo que una vendida no aparece.
	if len(svc.PublicCatalog()) != 0 {
		t.Fatalf("una publicación vendida no debe aparecer en el catálogo público")
	}
}

func TestSellerPublicationsOnlyReturnsOwn(t *testing.T) {
	svc := service.New(newFakeRepo())
	ana := loginUser(t, svc, "ana", domain.RoleVendedor)
	luis := loginUser(t, svc, "luis", domain.RoleVendedor)

	if _, err := svc.CreatePublication(ana.Token, "Mazda", "3", 2019, 100, "300", "ana@a.com", nil); err != nil {
		t.Fatalf("publicación de ana: %v", err)
	}
	if _, err := svc.CreatePublication(luis.Token, "Kia", "Rio", 2021, 200, "301", "luis@a.com", nil); err != nil {
		t.Fatalf("publicación de luis: %v", err)
	}

	own, err := svc.SellerPublications(ana.Token)
	if err != nil {
		t.Fatalf("consulta inesperada de error: %v", err)
	}
	if len(own) != 1 || own[0].SellerID != ana.UserID {
		t.Fatalf("el vendedor solo debe ver sus propias publicaciones: %+v", own)
	}
}

func TestDeletePublicationRequiresAdmin(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)
	admin := loginUser(t, svc, "admin", domain.RoleAdministrador)

	pub, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 100, "300", "a@a.com", nil)
	if err != nil {
		t.Fatalf("creación inesperada de error: %v", err)
	}

	if err := svc.DeletePublication(seller.Token, pub.ID); !errors.Is(err, service.ErrAdminRequired) {
		t.Fatalf("un vendedor no debe poder eliminar: se esperaba ErrAdminRequired, se obtuvo %v", err)
	}
	if err := svc.DeletePublication(admin.Token, pub.ID); err != nil {
		t.Fatalf("el administrador debe poder eliminar: %v", err)
	}
	if err := svc.DeletePublication(admin.Token, pub.ID); !errors.Is(err, service.ErrPublicationNotFound) {
		t.Fatalf("se esperaba ErrPublicationNotFound, se obtuvo %v", err)
	}
}

func TestDeleteUserRequiresAdmin(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)
	admin := loginUser(t, svc, "admin", domain.RoleAdministrador)

	if err := svc.DeleteUser(seller.Token, seller.UserID); !errors.Is(err, service.ErrAdminRequired) {
		t.Fatalf("un vendedor no debe poder eliminar usuarios: se esperaba ErrAdminRequired, se obtuvo %v", err)
	}
	if err := svc.DeleteUser(admin.Token, seller.UserID); err != nil {
		t.Fatalf("el administrador debe poder eliminar usuarios: %v", err)
	}
	if err := svc.DeleteUser(admin.Token, "USR-inexistente"); !errors.Is(err, service.ErrUserNotFound) {
		t.Fatalf("se esperaba ErrUserNotFound, se obtuvo %v", err)
	}
}

func TestUsersRequiresAdmin(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)
	admin := loginUser(t, svc, "admin", domain.RoleAdministrador)

	if _, err := svc.Users(seller.Token); !errors.Is(err, service.ErrAdminRequired) {
		t.Fatalf("se esperaba ErrAdminRequired, se obtuvo %v", err)
	}
	users, err := svc.Users(admin.Token)
	if err != nil || len(users) != 2 {
		t.Fatalf("se esperaban 2 usuarios, se obtuvieron %d (err=%v)", len(users), err)
	}
}

func TestLogoutRemovesSessionAndRecordsIt(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo)
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)

	if err := svc.Logout(seller.Token); err != nil {
		t.Fatalf("logout inesperado de error: %v", err)
	}
	if _, ok := repo.GetSessionByToken(seller.Token); ok {
		t.Fatalf("la sesión debía eliminarse tras el cierre")
	}
	if len(repo.logouts) != 1 {
		t.Fatalf("se esperaba 1 registro de logout, se obtuvo %d", len(repo.logouts))
	}
}

func TestAuditTrailRegistersSuccessesAndFailures(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo)
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)

	// Acción denegada: un vendedor intenta aprobar una publicación inexistente.
	_ = svc.ApprovePublication(seller.Token, "PUB-x")

	var denegada bool
	for _, l := range repo.auditLogs {
		if l.Action == "APROBAR_PUBLICACION" && !l.Success {
			denegada = true
		}
	}
	if !denegada {
		t.Fatalf("se esperaba una traza de auditoría con la operación denegada (success=false)")
	}
}

func TestCreatePublicationStoresPhotos(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)

	photos := []string{"/media/lote1/a.png", "/media/lote1/b.jpg"}
	pub, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 100, "300", "a@a.com", photos)
	if err != nil {
		t.Fatalf("creación inesperada de error: %v", err)
	}
	if len(pub.Photos) != len(photos) {
		t.Fatalf("se esperaban %d fotos, se obtuvieron %d", len(photos), len(pub.Photos))
	}
	for i, want := range photos {
		if pub.Photos[i] != want {
			t.Fatalf("foto %d: se esperaba %q, se obtuvo %q", i, want, pub.Photos[i])
		}
	}
}

func TestCreatePublicationEnforcesMaxPhotos(t *testing.T) {
	svc := service.New(newFakeRepo())
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)

	// El límite exacto (22) debe aceptarse.
	photos := make([]string, domain.MaxPhotosPerPublication)
	for i := range photos {
		photos[i] = fmt.Sprintf("/media/lote/%d.png", i)
	}
	if _, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 100, "300", "a@a.com", photos); err != nil {
		t.Fatalf("se esperaba aceptar %d fotos: %v", len(photos), err)
	}

	// Una foto más debe rechazarse.
	photos = append(photos, "/media/lote/extra.png")
	if _, err := svc.CreatePublication(seller.Token, "Mazda", "3", 2019, 100, "300", "a@a.com", photos); !errors.Is(err, service.ErrTooManyPhotos) {
		t.Fatalf("se esperaba ErrTooManyPhotos, se obtuvo %v", err)
	}
}

func TestAuditIntegrityRequiresAdmin(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo)
	seller := loginUser(t, svc, "vendedor", domain.RoleVendedor)
	admin := loginUser(t, svc, "admin", domain.RoleAdministrador)

	if _, err := svc.AuditIntegrity(seller.Token); !errors.Is(err, service.ErrAdminRequired) {
		t.Fatalf("un vendedor no debe verificar la auditoría: se esperaba ErrAdminRequired, se obtuvo %v", err)
	}

	ok, err := svc.AuditIntegrity(admin.Token)
	if err != nil || !ok {
		t.Fatalf("la auditoría íntegra debe verificarse como válida (ok=%v, err=%v)", ok, err)
	}

	repo.auditIntact = false
	ok, err = svc.AuditIntegrity(admin.Token)
	if err != nil || ok {
		t.Fatalf("una auditoría alterada debe detectarse (ok=%v, err=%v)", ok, err)
	}
}
