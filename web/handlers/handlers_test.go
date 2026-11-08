package handlers_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"automarket/internal/domain"
	"automarket/internal/service"
	"automarket/internal/storage"
	"automarket/web/handlers"
)

// newTestApp levanta la aplicación completa (storage + service + Gin) sobre un
// directorio temporal y devuelve el servidor de pruebas junto al servicio.
func newTestApp(t *testing.T) (*httptest.Server, *service.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("no se pudo crear el almacenamiento: %v", err)
	}
	svc := service.New(store)

	router := gin.New()
	router.LoadHTMLGlob("../templates/*.html")
	handlers.NewServer(svc).Register(router)

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv, svc
}

func newClient(t *testing.T, followRedirects bool) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("no se pudo crear el cookie jar: %v", err)
	}

	client := &http.Client{Jar: jar}
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

func postForm(t *testing.T, client *http.Client, target string, form url.Values) *http.Response {
	t.Helper()

	resp, err := client.PostForm(target, form)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	return resp
}

func getPage(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()

	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("no se pudo leer la respuesta: %v", err)
	}
	return string(body)
}

func mustRegister(t *testing.T, svc *service.Service, username string, role domain.Role) {
	t.Helper()
	if err := svc.RegisterUser(username, "secret", role); err != nil {
		t.Fatalf("no se pudo registrar %q: %v", username, err)
	}
}

func mustLogin(t *testing.T, svc *service.Service, username string) domain.Session {
	t.Helper()
	sess, err := svc.Login(username, "secret")
	if err != nil {
		t.Fatalf("no se pudo autenticar %q: %v", username, err)
	}
	return sess
}

// TestFullPublicationLifecycle recorre las tres historias de usuario por HTTP:
// registro/publicación del vendedor, aprobación del administrador y visibilidad
// en el catálogo público, para terminar con la eliminación de la publicación.
func TestFullPublicationLifecycle(t *testing.T) {
	srv, svc := newTestApp(t)
	client := newClient(t, true)

	// HU-VEN-01: registro autónomo del vendedor.
	resp := postForm(t, client, srv.URL+"/register", url.Values{
		"username": {"ana"},
		"password": {"secret"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registro: se esperaba 200 tras el redirect, se obtuvo %d", resp.StatusCode)
	}
	readBody(t, resp)

	// HU-VEN-01: inicio de sesión del vendedor.
	resp = postForm(t, client, srv.URL+"/login", url.Values{
		"username": {"ana"},
		"password": {"secret"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: se esperaba 200 tras el redirect, se obtuvo %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "Panel del vendedor") {
		t.Fatalf("tras el login se esperaba el panel del vendedor")
	}

	// HU-VEN-01: publicación de un vehículo (queda PENDIENTE).
	resp = postForm(t, client, srv.URL+"/seller/publications", url.Values{
		"brand": {"Mazda"},
		"model": {"3"},
		"year":  {"2019"},
		"price": {"45000000"},
		"phone": {"3001234567"},
		"email": {"ana@a.com"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publicar: se esperaba 200 tras el redirect, se obtuvo %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "Mazda") {
		t.Fatalf("la publicación recién creada debe aparecer en el panel del vendedor")
	}

	// HU-VIS-01: una publicación pendiente todavía no es visible al visitante.
	resp = getPage(t, client, srv.URL+"/")
	if body := readBody(t, resp); strings.Contains(body, "Mazda") {
		t.Fatalf("una publicación pendiente no debe aparecer en el catálogo público")
	}

	// HU-ADM-01: el administrador inicia sesión y autoriza la publicación.
	mustRegister(t, svc, "root", domain.RoleAdministrador)
	adminSession := mustLogin(t, svc, "root")

	pending, err := svc.PendingPublications(adminSession.Token)
	if err != nil || len(pending) != 1 {
		t.Fatalf("se esperaba 1 publicación pendiente, se obtuvo %d (err=%v)", len(pending), err)
	}
	pubID := pending[0].ID

	resp = postForm(t, client, srv.URL+"/login", url.Values{
		"username": {"root"},
		"password": {"secret"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login admin: se esperaba 200 tras el redirect, se obtuvo %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = postForm(t, client, srv.URL+"/admin/publications/"+pubID+"/approve", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("aprobar: se esperaba 200 tras el redirect, se obtuvo %d", resp.StatusCode)
	}
	readBody(t, resp)

	// HU-VIS-01: la publicación aprobada ya es visible con los datos de contacto.
	resp = getPage(t, client, srv.URL+"/")
	body := readBody(t, resp)
	if !strings.Contains(body, "Mazda") {
		t.Fatalf("la publicación aprobada debe aparecer en el catálogo público")
	}
	if !strings.Contains(body, "3001234567") || !strings.Contains(body, "ana@a.com") {
		t.Fatalf("el catálogo debe mostrar el contacto del vendedor")
	}

	// HU-ADM-01: el administrador elimina la publicación.
	resp = postForm(t, client, srv.URL+"/admin/publications/"+pubID+"/delete", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("eliminar: se esperaba 200 tras el redirect, se obtuvo %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp = getPage(t, client, srv.URL+"/")
	if body := readBody(t, resp); strings.Contains(body, "Mazda") {
		t.Fatalf("la publicación eliminada no debe aparecer en el catálogo público")
	}
}

func TestProtectedRouteRedirectsAnonymous(t *testing.T) {
	srv, _ := newTestApp(t)
	client := newClient(t, false)

	resp := getPage(t, client, srv.URL+"/seller")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("se esperaba 302, se obtuvo %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Fatalf("se esperaba redirección a /login, se obtuvo %q", loc)
	}
}

func TestSellerCannotAccessAdmin(t *testing.T) {
	srv, svc := newTestApp(t)
	mustRegister(t, svc, "ana", domain.RoleVendedor)

	client := newClient(t, false)

	login := postForm(t, client, srv.URL+"/login", url.Values{
		"username": {"ana"},
		"password": {"secret"},
	})
	if login.StatusCode != http.StatusFound {
		t.Fatalf("login: se esperaba 302, se obtuvo %d", login.StatusCode)
	}
	login.Body.Close()

	resp := getPage(t, client, srv.URL+"/admin")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("se esperaba 302, se obtuvo %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("se esperaba redirección a /, se obtuvo %q", loc)
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	srv, svc := newTestApp(t)
	mustRegister(t, svc, "ana", domain.RoleVendedor)

	client := newClient(t, false)
	resp := postForm(t, client, srv.URL+"/login", url.Values{
		"username": {"ana"},
		"password": {"incorrecta"},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("se esperaba 401, se obtuvo %d", resp.StatusCode)
	}
}

// onePixelPNG es una imagen PNG válida de 1x1 usada en las pruebas de carga.
var onePixelPNG = func() []byte {
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		panic(err)
	}
	return data
}()

// postPhotos envía el formulario de publicación como multipart con las fotos
// indicadas, simulando la selección de archivos del navegador.
func postPhotos(t *testing.T, client *http.Client, target string, fields url.Values, photos [][]byte) *http.Response {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key := range fields {
		if err := writer.WriteField(key, fields.Get(key)); err != nil {
			t.Fatalf("campo %q: %v", key, err)
		}
	}
	for i, photo := range photos {
		part, err := writer.CreateFormFile("photos", fmt.Sprintf("foto-%d.png", i))
		if err != nil {
			t.Fatalf("no se pudo crear la parte %d: %v", i, err)
		}
		if _, err := part.Write(photo); err != nil {
			t.Fatalf("no se pudo escribir la foto %d: %v", i, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("no se pudo cerrar el formulario: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, target, &body)
	if err != nil {
		t.Fatalf("no se pudo crear la petición: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	return resp
}

func TestSellerCanPublishWithPhotos(t *testing.T) {
	srv, svc := newTestApp(t)
	mustRegister(t, svc, "ana", domain.RoleVendedor)

	client := newClient(t, true)
	resp := postForm(t, client, srv.URL+"/login", url.Values{
		"username": {"ana"},
		"password": {"secret"},
	})
	readBody(t, resp)

	resp = postPhotos(t, client, srv.URL+"/seller/publications", url.Values{
		"brand": {"Mazda"},
		"model": {"3"},
		"year":  {"2019"},
		"price": {"45000000"},
		"phone": {"3001234567"},
		"email": {"ana@a.com"},
	}, [][]byte{onePixelPNG, onePixelPNG})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publicar con fotos: se esperaba 200 tras el redirect, se obtuvo %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	start := strings.Index(body, "/media/")
	if start < 0 {
		t.Fatal("el panel del vendedor debe mostrar las fotos de la publicación")
	}
	end := start
	for end < len(body) && body[end] != '"' {
		end++
	}
	mediaURL := srv.URL + body[start:end]

	mediaResp := getPage(t, client, mediaURL)
	defer mediaResp.Body.Close()
	if mediaResp.StatusCode != http.StatusOK {
		t.Fatalf("la foto debe servirse desde %s, se obtuvo %d", mediaURL, mediaResp.StatusCode)
	}
	if ct := mediaResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Fatalf("se esperaba una imagen PNG, se obtuvo %q", ct)
	}
}

func TestPublicationRejectsMoreThanMaxPhotos(t *testing.T) {
	srv, svc := newTestApp(t)
	mustRegister(t, svc, "ana", domain.RoleVendedor)

	client := newClient(t, true)
	resp := postForm(t, client, srv.URL+"/login", url.Values{
		"username": {"ana"},
		"password": {"secret"},
	})
	readBody(t, resp)

	photos := make([][]byte, domain.MaxPhotosPerPublication+1)
	for i := range photos {
		photos[i] = onePixelPNG
	}

	resp = postPhotos(t, client, srv.URL+"/seller/publications", url.Values{
		"brand": {"Mazda"},
		"model": {"3"},
		"year":  {"2019"},
		"price": {"45000000"},
		"phone": {"3001234567"},
		"email": {"ana@a.com"},
	}, photos)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("se esperaba 400 al superar el máximo de fotos, se obtuvo %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "22") {
		t.Fatal("el error debe indicar el máximo de 22 fotos")
	}
}

func TestAdminCanViewAuditTrail(t *testing.T) {
	srv, svc := newTestApp(t)
	mustRegister(t, svc, "root", domain.RoleAdministrador)

	client := newClient(t, true)
	resp := postForm(t, client, srv.URL+"/login", url.Values{
		"username": {"root"},
		"password": {"secret"},
	})
	readBody(t, resp)

	resp = getPage(t, client, srv.URL+"/admin/audit")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("se esperaba 200 en /admin/audit, se obtuvo %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Íntegra") {
		t.Fatal("la vista de auditoría debe indicar que la traza es íntegra")
	}
	if !strings.Contains(body, "LOGIN") {
		t.Fatal("la vista de auditoría debe mostrar los eventos registrados")
	}
}
