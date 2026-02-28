package k8s

// Lightweight K8s resource types - avoids client-go dependency.

type ObjectMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type StorageClass struct {
	Metadata    ObjectMeta        `json:"metadata"`
	Provisioner string            `json:"provisioner"`
	Parameters  map[string]string `json:"parameters"`
}

type Secret struct {
	Metadata ObjectMeta        `json:"metadata"`
	Data     map[string]string `json:"data"`
}
