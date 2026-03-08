package nifi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientGetClusterSummary(t *testing.T) {
	caCertPEM, serverTLS := newTestTLSConfig(t)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nifi-api/access/token":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST token request, got %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("username"); got != "admin" {
				t.Fatalf("unexpected username %q", got)
			}
			if got := r.Form.Get("password"); got != "secret" {
				t.Fatalf("unexpected password %q", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("token-123"))
		case "/nifi-api/flow/cluster/summary":
			if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
				t.Fatalf("unexpected authorization header %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"clusterSummary":{"connectedNodeCount":3,"totalNodeCount":3,"connectedToCluster":true,"clustered":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	server.TLS = serverTLS
	server.StartTLS()
	defer server.Close()

	client := NewHTTPClient()
	summary, err := client.GetClusterSummary(context.Background(), ClusterSummaryRequest{
		BaseURL:   server.URL,
		Username:  "admin",
		Password:  "secret",
		CACertPEM: caCertPEM,
	})
	if err != nil {
		t.Fatalf("GetClusterSummary returned error: %v", err)
	}

	if !summary.Healthy(3) {
		t.Fatalf("expected healthy cluster summary, got %+v", summary)
	}
}

func newTestTLSConfig(t *testing.T) ([]byte, *tls.Config) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-ca",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	certificate, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("build X509 key pair: %v", err)
	}

	return caCertPEM, &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}
}

func TestClusterSummaryHealthy(t *testing.T) {
	summary := ClusterSummary{
		ConnectedNodeCount: 3,
		TotalNodeCount:     3,
		ConnectedToCluster: true,
		Clustered:          true,
	}
	if !summary.Healthy(3) {
		t.Fatalf("expected summary to be healthy: %+v", summary)
	}
}
