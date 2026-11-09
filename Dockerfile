# ---- Etapa de compilación ----
FROM golang:1.26.5-alpine AS build

WORKDIR /src

# Se copian primero las dependencias para aprovechar la caché de capas.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Binario estático: no depende de librerías del sistema en la imagen final.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/automarket ./cmd/api

# ---- Etapa de ejecución ----
FROM alpine:3.22

# Usuario sin privilegios.
RUN adduser -D -u 10001 automarket

WORKDIR /app

COPY --from=build /out/automarket /app/automarket
COPY --from=build /src/web/templates /app/web/templates
COPY --from=build /src/web/static /app/web/static

USER automarket

ENV GIN_MODE=release

EXPOSE 8080

ENTRYPOINT ["/app/automarket"]
