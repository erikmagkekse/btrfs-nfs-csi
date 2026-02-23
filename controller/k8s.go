package controller

import (
	"context"

	"github.com/erikmagkekse/btrfs-nfs-csi/k8s"
	"github.com/erikmagkekse/btrfs-nfs-csi/model"
)

// k8sDiscoverAgentURLs returns a map of agentURL → StorageClass name for our driver.
func k8sDiscoverAgentURLs(ctx context.Context) (map[string]string, error) {
	var scList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Provisioner string            `json:"provisioner"`
			Parameters  map[string]string `json:"parameters"`
		} `json:"items"`
	}

	if err := k8s.Get(ctx, "/apis/storage.k8s.io/v1/storageclasses", &scList); err != nil {
		ctrlK8sOpsTotal.WithLabelValues("error").Inc()
		return nil, err
	}
	ctrlK8sOpsTotal.WithLabelValues("success").Inc()

	urlToSC := make(map[string]string)
	for _, sc := range scList.Items {
		if sc.Provisioner != model.DriverName {
			continue
		}
		if url := sc.Parameters[paramAgentURL]; url != "" {
			urlToSC[url] = sc.Metadata.Name
		}
	}
	return urlToSC, nil
}
