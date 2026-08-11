package nacos

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientDisablesAmbientNetworkBehavior(t *testing.T) {
	client, err := newHTTPClient(validConfig("http://nacos:8848/nacos"))
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.TLSClientConfig != nil || !transport.DisableKeepAlives || client.Timeout != time.Second {
		t.Fatalf("client=%#v transport=%#v", client, transport)
	}
	if err := client.CheckRedirect(httptest.NewRequest(http.MethodGet, "http://nacos.test/next", nil), nil); err == nil {
		t.Fatal("redirect was accepted")
	}
	invalid := validConfig("http://nacos:8848/nacos")
	invalid.Mode = ModeDisabled
	if _, err := newHTTPClient(invalid); err == nil {
		t.Fatal("disabled configuration created an HTTP client")
	}
}

func TestHTTPClientAuthenticatesPrivateCAAndOptionalClient(t *testing.T) {
	material := newTLSMaterial(t)
	for _, test := range []struct {
		name, caFile, serverName string
		clientCertificate        bool
		requireClient            bool
		wantError                bool
	}{
		{name: "TLS", caFile: material.caFile, serverName: "nacos.internal"},
		{name: "mTLS", caFile: material.caFile, serverName: "nacos.internal", clientCertificate: true, requireClient: true},
		{name: "wrong CA", caFile: newTLSMaterial(t).caFile, serverName: "nacos.internal", wantError: true},
		{name: "wrong server name", caFile: material.caFile, serverName: "other.internal", wantError: true},
		{name: "missing client certificate", caFile: material.caFile, serverName: "nacos.internal", requireClient: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) }))
			server.TLS = material.serverTLS(test.requireClient)
			server.StartTLS()
			defer server.Close()

			config := validConfig("https://nacos.internal:8848/nacos")
			config.TLSCAFile = test.caFile
			config.TLSServerName = test.serverName
			if test.clientCertificate {
				config.TLSClientCertFile = material.clientCertFile
				config.TLSClientKeyFile = material.clientKeyFile
			}
			client, err := newHTTPClient(config)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Get(server.URL)
			if response != nil {
				_ = response.Body.Close()
			}
			if (err != nil) != test.wantError {
				t.Fatalf("request error=%v wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestHTTPClientRejectsUnsafeMaterialWithoutPathLeakage(t *testing.T) {
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "secret-marker.pem")
	if err := os.WriteFile(secretPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := validConfig("https://nacos.internal:8848/nacos")
	config.TLSCAFile = secretPath
	config.TLSServerName = "nacos.internal"
	if _, err := newHTTPClient(config); err == nil || strings.Contains(err.Error(), secretPath) || strings.Contains(err.Error(), "not a certificate") {
		t.Fatalf("unsafe or leaking error=%v", err)
	}
	config.TLSCAFile = directory
	if _, err := newHTTPClient(config); err == nil || strings.Contains(err.Error(), directory) {
		t.Fatalf("non-regular material error=%v", err)
	}
	for name, content := range map[string][]byte{
		"empty":     {},
		"oversized": make([]byte, maximumTLSMaterialBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name+".pem")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			config.TLSCAFile = path
			if _, err := newHTTPClient(config); err == nil || strings.Contains(err.Error(), path) {
				t.Fatalf("bounded material error=%v", err)
			}
		})
	}
}

func TestHTTPClientRejectsInvalidClientPair(t *testing.T) {
	material := newTLSMaterial(t)
	config := validConfig("https://nacos.internal:8848/nacos")
	config.TLSCAFile = material.caFile
	config.TLSServerName = "nacos.internal"
	config.TLSClientCertFile = material.clientCertFile
	config.TLSClientKeyFile = filepath.Join(t.TempDir(), "missing-key.pem")
	if _, err := newHTTPClient(config); err == nil || strings.Contains(err.Error(), config.TLSClientKeyFile) {
		t.Fatalf("missing key error=%v", err)
	}
	config.TLSClientKeyFile = material.caFile
	if _, err := newHTTPClient(config); err == nil {
		t.Fatal("invalid client certificate pair was accepted")
	}
}

type tlsMaterial struct {
	caFile, clientCertFile, clientKeyFile string
	serverCertificate                     tls.Certificate
	caPool                                *x509.CertPool
}

func newTLSMaterial(t *testing.T) tlsMaterial {
	t.Helper()
	directory := t.TempDir()
	caPublic, caPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "NeKiro test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPublic, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(directory, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	issue := func(name string, serial int64, usage x509.ExtKeyUsage, dnsNames []string) (string, string, tls.Certificate) {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, DNSNames: dnsNames, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, public, caPrivate)
		if err != nil {
			t.Fatal(err)
		}
		certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		keyDER, err := x509.MarshalPKCS8PrivateKey(private)
		if err != nil {
			t.Fatal(err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
		certificateFile, keyFile := filepath.Join(directory, name+".pem"), filepath.Join(directory, name+"-key.pem")
		if err := os.WriteFile(certificateFile, certificatePEM, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
			t.Fatal(err)
		}
		certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
		if err != nil {
			t.Fatal(err)
		}
		return certificateFile, keyFile, certificate
	}
	_, _, serverCertificate := issue("server", 2, x509.ExtKeyUsageServerAuth, []string{"nacos.internal"})
	clientCertificateFile, clientKeyFile, _ := issue("client", 3, x509.ExtKeyUsageClientAuth, nil)
	pool := x509.NewCertPool()
	pool.AddCert(caCertificate)
	return tlsMaterial{caFile: caFile, clientCertFile: clientCertificateFile, clientKeyFile: clientKeyFile, serverCertificate: serverCertificate, caPool: pool}
}

func (material tlsMaterial) serverTLS(requireClient bool) *tls.Config {
	configuration := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{material.serverCertificate}}
	if requireClient {
		configuration.ClientAuth = tls.RequireAndVerifyClientCert
		configuration.ClientCAs = material.caPool
	}
	return configuration
}
