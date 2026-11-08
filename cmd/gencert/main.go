// Command gencert genera un certificado TLS autofirmado para desarrollo local.
// En producción (Render, Railway, Fly.io) el TLS lo termina la plataforma y no
// hace falta este comando.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func main() {
	outDir := flag.String("out", "certs", "directorio donde escribir cert.pem y key.pem")
	host := flag.String("host", "localhost", "nombre del host para el certificado")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("no se pudo crear el directorio %s: %v", *outDir, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("no se pudo generar la clave: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Fatalf("no se pudo generar el número de serie: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: *host, Organization: []string{"AutoMarket"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{*host},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("no se pudo crear el certificado: %v", err)
	}

	certPath := filepath.Join(*outDir, "cert.pem")
	keyPath := filepath.Join(*outDir, "key.pem")

	if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
		log.Fatalf("no se pudo escribir el certificado: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Fatalf("no se pudo serializar la clave: %v", err)
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER); err != nil {
		log.Fatalf("no se pudo escribir la clave: %v", err)
	}

	log.Printf("certificado generado: %s y %s", certPath, keyPath)
}

// writePEM escribe un bloque PEM con permisos restrictivos.
func writePEM(path, kind string, der []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	return pem.Encode(file, &pem.Block{Type: kind, Bytes: der})
}
