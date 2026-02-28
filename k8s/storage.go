package k8s

import (
	"context"
	"encoding/base64"
	"fmt"
)

// ListStorageClasses returns all StorageClasses matching the given provisioner.
func ListStorageClasses(ctx context.Context, provisioner string) ([]StorageClass, error) {
	var list struct {
		Items []StorageClass `json:"items"`
	}
	if err := Get(ctx, "/apis/storage.k8s.io/v1/storageclasses", &list); err != nil {
		return nil, fmt.Errorf("list storage classes: %w", err)
	}

	var result []StorageClass
	for _, sc := range list.Items {
		if sc.Provisioner != provisioner {
			continue
		}
		result = append(result, sc)
	}
	return result, nil
}

// GetSecretValue reads a single key from a K8s Secret and returns the decoded value.
func GetSecretValue(ctx context.Context, namespace, name, key string) (string, error) {
	var secret Secret
	path := fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, name)
	if err := Get(ctx, path, &secret); err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}

	encoded := secret.Data[key]
	if encoded == "" {
		return "", nil
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode secret key %q: %w", key, err)
	}
	return string(decoded), nil
}
