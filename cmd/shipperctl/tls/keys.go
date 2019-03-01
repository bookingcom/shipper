package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
)

const (
	csrDataType           = "CERTIFICATE REQUEST"
	RSAPrivateKeyDataType = "RSA PRIVATE KEY"
)

func GenerateCSRForServiceInNamespace(privateKey *rsa.PrivateKey, service, namespace string) ([]byte, error) {
	template := &x509.CertificateRequest{
		SignatureAlgorithm: x509.SHA256WithRSA,
		Subject: pkix.Name{
			CommonName: fmt.Sprintf("%s.%s.svc", service, namespace),
		},
		DNSNames: []string{
			service,
			fmt.Sprintf("%s.%s", service, namespace),
			fmt.Sprintf("%s.%s.svc", service, namespace),
		},
	}

	csr, err := x509.CreateCertificateRequest(rand.Reader, template, privateKey)
	if err != nil {
		return nil, err
	}

	// Kubernetes expects a PEM-encoded request, so convert our CSR to PEM
	block := &pem.Block{
		Type:  string(csrDataType),
		Bytes: csr,
	}

	return pem.EncodeToMemory(block), nil
}

func EncodePrivateKeyAsPEM(data []byte) []byte {
	block := &pem.Block{
		Type:  string(RSAPrivateKeyDataType),
		Bytes: data,
	}

	return pem.EncodeToMemory(block)
}
