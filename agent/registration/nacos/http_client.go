package nacos

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const maximumTLSMaterialBytes int64 = 1 << 20

func newHTTPClient(config Config) (*http.Client, error) {
	if config.Mode != ModeNacos || config.Validate() != nil {
		return nil, errors.New("Nacos registration transport configuration is invalid")
	}
	origin, _ := url.Parse(config.APIOrigin)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	transport.TLSClientConfig = nil
	if origin.Scheme == "https" {
		roots, err := loadCAPool(config.TLSCAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: config.TLSServerName}
		if config.TLSClientCertFile != "" {
			certificatePEM, err := readTLSMaterial(config.TLSClientCertFile, "client certificate")
			if err != nil {
				return nil, err
			}
			keyPEM, err := readTLSMaterial(config.TLSClientKeyFile, "client key")
			if err != nil {
				return nil, err
			}
			certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
			if err != nil {
				return nil, errors.New("Nacos TLS client certificate pair is invalid")
			}
			tlsConfig.Certificates = []tls.Certificate{certificate}
		}
		transport.TLSClientConfig = tlsConfig
	}
	return &http.Client{
		Transport: transport,
		Timeout:   config.RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("Nacos redirects are disabled")
		},
	}, nil
}

func loadCAPool(path string) (*x509.CertPool, error) {
	content, err := readTLSMaterial(path, "CA")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	certificates := 0
	for len(strings.TrimSpace(string(content))) != 0 {
		block, rest := pem.Decode(content)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, errors.New("Nacos TLS CA material is invalid")
		}
		parsed, err := x509.ParseCertificates(block.Bytes)
		if err != nil || len(parsed) == 0 {
			return nil, errors.New("Nacos TLS CA material is invalid")
		}
		for _, certificate := range parsed {
			if !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
				return nil, errors.New("Nacos TLS CA material is invalid")
			}
			pool.AddCert(certificate)
			certificates++
		}
		content = rest
	}
	if certificates == 0 {
		return nil, errors.New("Nacos TLS CA material is invalid")
	}
	return pool, nil
}

func readTLSMaterial(path, kind string) ([]byte, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("Nacos TLS " + kind + " material is unavailable")
	}
	if !pathInfo.Mode().IsRegular() || pathInfo.Size() <= 0 || pathInfo.Size() > maximumTLSMaterialBytes {
		return nil, errors.New("Nacos TLS " + kind + " material is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("Nacos TLS " + kind + " material is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) || info.Size() <= 0 || info.Size() > maximumTLSMaterialBytes {
		return nil, errors.New("Nacos TLS " + kind + " material is invalid")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumTLSMaterialBytes+1))
	if err != nil || int64(len(content)) != info.Size() || int64(len(content)) > maximumTLSMaterialBytes {
		return nil, errors.New("Nacos TLS " + kind + " material is invalid")
	}
	return content, nil
}
