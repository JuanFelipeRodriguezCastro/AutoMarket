# AutoMarket — Sprint 3 (MySQL, HTTPS, auditoría y fotos)

Componente backend de la plataforma **AutoMarket** para el despliegue de
información sobre venta de vehículos usados. Este entregable corresponde al
**Sprint 3** y, además de consolidar el **Accounting** descentralizado y la carga
de **fotos** (hasta 22 por vehículo), incorpora el cambio de última hora pedido
por el docente:

- **Motor de base de datos MySQL/MariaDB** para toda la información (usuarios,
  publicaciones, sesiones, cierres de sesión, auditoría y fotos).
- **HTTPS** en el despliegue (TLS terminado por la plataforma o certificado
  autofirmado en local).
- **Despliegue en un servidor gratuito** (Render) con una base de datos MySQL
  gestionada también gratuita (TiDB Cloud Serverless o Aiven).

> Nota sobre el alcance: el enunciado original pedía HTTP sin TLS y prohibía
> drivers de base de datos. El cambio de última hora del docente autoriza
> explícitamente MySQL y HTTPS para este sprint; por eso se añade
> `github.com/go-sql-driver/mysql`, además de Gin.

## Requisitos

- Go 1.26+ (probado con go1.26.5)
- MySQL 8.0+ o MariaDB 10.6+
- Docker Desktop (para la ejecución local con `docker compose`)
- Dependencias externas: `github.com/gin-gonic/gin` y
  `github.com/go-sql-driver/mysql`

## Ejecución local

### Opción A — Docker Compose (recomendada)

```bash
docker compose up --build -d
```

- Aplicación: <http://localhost:8081>
- MySQL: contenedor `automarket-sprint3-mysql` (puerto interno 3306; opcional
  publicado en el host como 3307)
- Los datos quedan en el volumen `automarket-sprint3_mysql_data`.

El contenedor de la aplicación se llama `automarket-sprint3` y usa la imagen
`automarket-sprint3:local` (internamente escucha en el puerto 8080). El nombre y
la imagen son propios de este sprint para no chocar con los otros.

Comandos útiles:

```bash
docker compose ps
docker compose logs -f automarket
docker compose down          # detener (los datos del volumen permanecen)
docker compose down -v       # detener y borrar la base de datos
```

Para inspeccionar la base de datos desde el host:

```bash
mysql -h 127.0.0.1 -P 3307 -uautomarket -pautomarket automarket
```

### Opción B — MySQL local

1. Crea la base de datos y el usuario:

   ```sql
   CREATE DATABASE automarket CHARACTER SET utf8mb4;
   CREATE USER 'automarket'@'%' IDENTIFIED BY 'automarket';
   GRANT ALL PRIVILEGES ON automarket.* TO 'automarket'@'%';
   FLUSH PRIVILEGES;
   ```

2. Exporta el DSN y arranca:

   ```bash
   export DATABASE_DSN='automarket:automarket@tcp(127.0.0.1:3306)/automarket?parseTime=true&charset=utf8mb4'
   go run ./cmd/api
   ```

   > Si usas el MySQL de `docker compose` desde el host, el puerto publicado es
   > `3307` y el DSN cambia a `tcp(127.0.0.1:3307)`.

   En PowerShell:

   ```powershell
   $env:DATABASE_DSN='automarket:automarket@tcp(127.0.0.1:3306)/automarket?parseTime=true&charset=utf8mb4'
   go run ./cmd/api
   ```

La aplicación crea el esquema automáticamente al arrancar (`CREATE TABLE IF NOT
EXISTS`), así que no hay migraciones manuales.

### HTTPS local (opcional)

En Render el TLS lo termina la plataforma. Para probar HTTPS en local:

```bash
go run ./cmd/gencert -out certs
TLS_CERT_FILE=certs/cert.pem TLS_KEY_FILE=certs/key.pem go run ./cmd/api
```

Luego abrir <https://localhost:8080> y aceptar la advertencia del certificado
autofirmado. El comando `gencert` usa solo la biblioteca estándar
(`crypto/tls`, `crypto/x509`).

## Despliegue gratuito en Render + MySQL gestionado

Render ofrece **servicios web gratuitos** con HTTPS automático. No incluye MySQL
gratuito, así que la base de datos se crea en un proveedor externo con plan
gratuito.

### 1. Base de datos MySQL gratuita

Opciones recomendadas:

| Proveedor | Plan gratuito | Notas |
| --- | --- | --- |
| **TiDB Cloud Serverless** | 5 GiB | Compatible con MySQL; exige TLS |
| **Aiven for MySQL** | 1 GiB | Compatible con MySQL; exige TLS |

Crea la base de datos `automarket` y copia el DSN. Ejemplos:

```text
# TiDB Cloud Serverless
usuario:clave@tcp(gateway01.us-east-1.prod.aws.tidbcloud.com:4000)/automarket?parseTime=true&tls=true

# Aiven
avnadmin:clave@tcp(mysql-xxxx.aivencloud.com:12345)/defaultdb?parseTime=true&tls=true
```

### 2. Servicio web en Render

1. Sube el proyecto a GitHub.
2. En Render: **New → Blueprint** y selecciona el repositorio. Render lee
   `render.yaml` y crea el servicio Docker.
3. Configura las variables marcadas con `sync: false`:
   - `DATABASE_DSN`: el DSN del paso anterior.
   - `AUTOMARKET_ADMIN_PASSWORD`: la contraseña del administrador inicial.
4. Render define `PORT` automáticamente y publica el servicio por **HTTPS**.

La URL pública tendrá la forma `https://automarket-sprint3.onrender.com`. El
plan gratuito **suspende el servicio tras unos minutos de inactividad**; la
primera petición puede tardar en responder.

### 3. Verificación

```bash
curl -I https://TU-SERVICIO.onrender.com/
curl -I https://TU-SERVICIO.onrender.com/login
```

## Variables de entorno

| Variable | Obligatoria | Descripción |
| --- | --- | --- |
| `DATABASE_DSN` | Sí | Cadena de conexión a MySQL/MariaDB |
| `PORT` | No | Puerto de escucha (por defecto `8080`; Render lo define) |
| `AUTOMARKET_ADMIN_USER` | No | Usuario del administrador inicial (`admin`) |
| `AUTOMARKET_ADMIN_PASSWORD` | No | Contraseña del administrador inicial (`admin`) |
| `TLS_CERT_FILE` | No | Certificado PEM para HTTPS local |
| `TLS_KEY_FILE` | No | Clave PEM para HTTPS local |
| `GIN_MODE` | No | `release` en producción |

## Estructura

```
cmd/api/                 Punto de entrada: cablea MySQL, servicio y Gin
cmd/gencert/             Genera un certificado TLS autofirmado (solo stdlib)
internal/domain/         Entidades puras (User, Publication, Session, AuditLog, Photo)
internal/storage/mysql/  Persistencia MySQL (producción)
internal/storage/        Implementación en memoria/JSON usada como doble de pruebas
internal/service/        Reglas de negocio + AAA (puerto Repository)
web/handlers/            Controladores HTTP, middleware y modelos de vista
web/templates/           Vistas HTML (partials + una plantilla por página)
```

La producción **solo** usa `internal/storage/mysql`. El paquete
`internal/storage` se conserva como doble de pruebas para que las pruebas
unitarias y de integración HTTP no necesiten una base de datos.

## Fotos de las publicaciones

El vendedor puede adjuntar hasta **22 fotos** por publicación desde el formulario
del panel (`multipart/form-data`). Las imágenes se validan por su **contenido
real** (`http.DetectContentType`) y solo se aceptan **JPEG, PNG, WEBP o GIF** de
hasta 5 MB cada una. El binario se guarda en la tabla `photos` de MySQL y se
sirve públicamente en `/media/<id>`. Si la publicación no llega a crearse, las
fotos recién insertadas se eliminan.

## Rutas y trazabilidad con las historias de usuario

| Método | Ruta | Autorización | Historia |
| --- | --- | --- | --- |
| GET | `/` | Público | HU-VIS-01: catálogo + contacto del vendedor |
| GET | `/media/:id` | Público | Foto almacenada en MySQL |
| GET | `/register` | Público | HU-VEN-01: formulario de registro |
| POST | `/register` | Público | HU-VEN-01: alta de vendedor |
| GET | `/login` | Público | Autenticación |
| POST | `/login` | Público | Autenticación |
| POST | `/logout` | Autenticado | Cierre de sesión |
| GET | `/seller` | VENDEDOR | HU-VEN-01: panel y publicaciones propias |
| POST | `/seller/publications` | VENDEDOR | HU-VEN-01: publicar con fotos (queda PENDIENTE) |
| POST | `/seller/publications/:id/sold` | VENDEDOR (dueño) | HU-VEN-01: reportar venta |
| GET | `/admin` | ADMINISTRADOR | HU-ADM-01: pendientes y usuarios |
| GET | `/admin/audit` | ADMINISTRADOR | Auditoría: traza + verificación de integridad |
| POST | `/admin/publications/:id/approve` | ADMINISTRADOR | HU-ADM-01: autorizar publicación |
| POST | `/admin/publications/:id/delete` | ADMINISTRADOR | HU-ADM-01: eliminar publicación |
| POST | `/admin/users/:id/delete` | ADMINISTRADOR | HU-ADM-01: eliminar usuario |

## Marco de seguridad AAA

- **Authentication:** el token de sesión viaja en una cookie `automarket_token`;
  el middleware `requireRole` valida que la sesión exista y no esté expirada.
- **Authorization:** cada grupo de rutas exige el rol `VENDEDOR` o
  `ADMINISTRADOR`; el servicio vuelve a verificar el rol (defensa en profundidad).
- **Accounting:** toda operación (éxito o fallo) se registra en la tabla `audit`;
  los inicios y cierres de sesión se reflejan además en `sessions` y `logouts`.
  Cada entrada de `audit` incluye el `hash` de la anterior (`prev_hash`),
  formando una **cadena de integridad** SHA-256. La tabla `audit_state` guarda el
  último hash para detectar también el borrado de las últimas entradas. La vista
  `/admin/audit` recalcula la cadena y muestra **Íntegra** o **Alterada**.

## Persistencia (MySQL)

| Tabla | Contenido |
| --- | --- |
| `users` | cuentas de usuario |
| `publications` | publicaciones de vehículos (con las URLs de sus fotos) |
| `sessions` | sesiones activas |
| `logouts` | historial de cierres de sesión |
| `audit` | traza de auditoría encadenada por hash |
| `audit_state` | último hash de la cadena (detección de borrados) |
| `photos` | contenido binario de las imágenes (`LONGBLOB`) |

## Pruebas

```bash
go test ./...
go vet ./...
```

Incluye pruebas unitarias del servicio (reglas de negocio y límite de 22 fotos),
pruebas de integración HTTP (ciclo completo de las historias de usuario, subida y
servicio de fotos, rechazo al superar el máximo de fotos y consulta de la traza
de auditoría) y una prueba de integración de MySQL que se omite si no se define
`AUTOMARKET_TEST_DSN`:

```bash
AUTOMARKET_TEST_DSN='automarket:automarket@tcp(127.0.0.1:3306)/automarket_test?parseTime=true' \
  go test ./internal/storage/mysql
```

## Decisión de diseño

La capa de servicio sigue dependiendo de un puerto (`service.Repository`) y no
conoce MySQL. El adaptador `internal/storage/mysql` implementa ese puerto y
concentra todo el SQL. La capa web sigue sin acceder a la base de datos: lee los
archivos del formulario, los valida y los entrega al servicio. Así se mantiene la
separación estructural (I/O, lógica y persistencia) incluso con el motor de base
de datos.
