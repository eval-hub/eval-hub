package certificates

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"k8s.io/client-go/util/cert"
)

type CertConfigs struct {
	CertificateDir string `json:"certificate_dir,omitempty"`
	CACerts        string `json:"ca_cert,omitempty"`
	ServerCert     string `json:"server_cert,omitempty"`
	ServerKey      string `json:"server_key,omitempty"`
	ClientCert     string `json:"client_cert,omitempty"`
}

func useSelfSignedCertificates(configs *CertConfigs) bool {
	return (configs.ServerCert == "") && (configs.CACerts == "")
}

func GetCertConfigs(configs *CertConfigs, logger *slog.Logger) error {
	if useSelfSignedCertificates(configs) {
		logger.Info("Creating self-signed certificates because no certificates were provided", "config", configs)
		err := createSelfSignedCertificates(configs, logger)
		if err != nil {
			return fmt.Errorf("Failed to create self-signed certificates: %w", err)
		}
		return nil
	}
	return nil
}

func getCertPool(dir string, file string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	// use OpenInRoot as it is a safe function that disallows reading outside of the root directory
	f, err := os.OpenInRoot(dir, file)
	if err != nil {
		return nil, fmt.Errorf("Cannot load client certificate from certFile=%q: %w", filepath.Join(dir, file), err)
	}
	clientPEMBlock, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("Cannot load client certificate from certFile=%q: %w", filepath.Join(dir, file), err)
	}
	if !pool.AppendCertsFromPEM(clientPEMBlock) {
		return nil, fmt.Errorf("Failed append client cert to the pool")
	}
	return pool, nil
}

func getOpt(name string, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

func createSelfSignedCertificates(certificates *CertConfigs, logger *slog.Logger) error {
	// if we are self signed then no CA or client cert
	certificates.CACerts = ""
	certificates.ClientCert = ""

	host, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("Failed to retrieve hostname for self-signed cert: %w", err)
	}
	certBytes, keyBytes, err := cert.GenerateSelfSignedCertKey(host, nil, []string{"localhost"})
	if err != nil {
		return fmt.Errorf("Failed to generate self signed cert and key: %w", err)
	}
	if certificates.CertificateDir != "" {
		// check that it exists
		info, err := os.Stat(certificates.CertificateDir)
		if err != nil {
			logger.Info("Certificate directory does not exist, so creating a temporary directory", "dir", certificates.CertificateDir)
			certificates.CertificateDir = ""
		} else if !info.IsDir() {
			logger.Info("Certificate directory is not a directory, so creating a temporary directory", "dir", certificates.CertificateDir)
			certificates.CertificateDir = ""
		}
	}
	if certificates.CertificateDir == "" {
		// create a temporary directory to write the server.crt and server.key
		dir := strings.TrimSuffix(os.TempDir(), "/")
		dir = fmt.Sprintf("%s/certs-%s", dir, uuid.New().String())
		err = os.Mkdir(dir, 0750)
		if err != nil {
			return fmt.Errorf("Failed to create the temporary directory for certificates: %w", err)
		}
		certificates.CertificateDir = dir
		logger.Info("Created a temporary directory for certificates", "dir", certificates.CertificateDir)
	}

	certificates.ServerCert = getOpt(certificates.ServerCert, "server.crt")
	certificates.ServerKey = getOpt(certificates.ServerKey, "server.key")

	serverCertFile := filepath.Join(certificates.CertificateDir, certificates.ServerCert)
	serverKeyFile := filepath.Join(certificates.CertificateDir, certificates.ServerKey)

	if info, err := os.Stat(serverCertFile); (err == nil) && (info.Size() > 0) {
		logger.Info("Server certificate already exists, so skip creating", "file", info.Name())
	} else {
		err = os.WriteFile(serverCertFile, certBytes, 0400)
		if err != nil {
			return fmt.Errorf("Failed to write server certificate for self-signed cert: %w", err)
		}
		logger.Info("Wrote self-signed certificate file", "server.crt", serverCertFile)
	}
	if info, err := os.Stat(serverKeyFile); (err == nil) && (info.Size() > 0) {
		logger.Info("Server key already exists, so skip creating", "file", info.Name())
	} else {
		err = os.WriteFile(serverKeyFile, keyBytes, 0400)
		if err != nil {
			return fmt.Errorf("Failed to write server key for self-signed cert: %w", err)
		}
		logger.Info("Wrote self-signed certificate file", "server.key", serverKeyFile)
	}

	return nil
}

func GetCertificates(certificates *CertConfigs, logger *slog.Logger) ([]tls.Certificate, *x509.CertPool, *x509.CertPool, error) {
	if certificates == nil {
		return nil, nil, nil, nil
	}
	var certs []tls.Certificate
	var rootCAs *x509.CertPool
	var clientCAs *x509.CertPool

	// This can be optional
	if certificates.ServerCert != "" {
		serverCert := filepath.Join(certificates.CertificateDir, certificates.ServerCert)
		serverKey := filepath.Join(certificates.CertificateDir, certificates.ServerKey)

		// Load client cert
		cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
		if err == nil {
			certs = append(certs, cert)
		} else {
			return nil, nil, nil, fmt.Errorf("Failed to load the server certificate %s / %s: %w", serverCert, serverKey, err)
		}
	}

	if certificates.CACerts != "" {
		// Get the SystemCertPool
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("Failed to load the x509.SystemCertPool: %w", err)
		}
		if pool == nil {
			pool = x509.NewCertPool()
			logger.Info("Warning: using an empty X509 certificate pool...")
		}
		// this can be a comma separated list of certs
		for caCert := range strings.SplitSeq(certificates.CACerts, ",") {
			// use OpenInRoot as it is a safe function that disallows reading outside of the root directory
			f, err := os.OpenInRoot(getDirAndName(certificates.CertificateDir, caCert))
			if err != nil {
				return nil, nil, nil, fmt.Errorf("Failed to load the CA certificate %s: %w", filepath.Join(certificates.CertificateDir, caCert), err)
			}
			clientPEMBlock, err := io.ReadAll(f)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("Failed to read the CA certificate %s: %w", filepath.Join(certificates.CertificateDir, caCert), err)
			}
			// Append our cert to the system pool
			if ok := pool.AppendCertsFromPEM(clientPEMBlock); !ok {
				return nil, nil, nil, fmt.Errorf("Failed append client cert %s to the pool", caCert)
			}
		}
		// Trust the augmented cert pool in our client
		rootCAs = pool
	}

	if certificates.ClientCert != "" {
		clientCert := filepath.Join(certificates.CertificateDir, certificates.ClientCert)
		pool, err := getCertPool(certificates.CertificateDir, certificates.ClientCert)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("Failed to load the client certificate %s: %w", clientCert, err)
		}
		clientCAs = pool
	}

	return certs, rootCAs, clientCAs, nil
}

// This function will accept an absolute file name and split the name, or it will return the dir and name
func getDirAndName(dir string, name string) (string, string) {
	if filepath.IsAbs(name) {
		return filepath.Dir(name), filepath.Base(name)
	}
	return dir, name
}
