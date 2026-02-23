// Minimal in-cluster K8s API client using only the standard library.
// This avoids pulling in client-go and its transitive dependencies to keep
// the binary small and the dependency tree manageable.
package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const base = "https://kubernetes.default.svc"
const saPath = "/var/run/secrets/kubernetes.io/serviceaccount/"

var httpClient *http.Client

func getClient() (*http.Client, error) {
	if httpClient != nil {
		return httpClient, nil
	}

	caCert, err := os.ReadFile(saPath + "ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caCert)

	httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: pool,
			},
		},
	}
	return httpClient, nil
}

func Do(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	token, err := os.ReadFile(saPath + "token")
	if err != nil {
		return nil, fmt.Errorf("read SA token: %w", err)
	}

	client, err := getClient()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return client.Do(req)
}

func Get(ctx context.Context, path string, result any) error {
	resp, err := Do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("K8s API %d: %s", resp.StatusCode, string(body))
	}

	return json.Unmarshal(body, result)
}
