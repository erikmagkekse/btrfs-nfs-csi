package v1

import "strings"

type labeled interface {
	GetLabels() map[string]string
}

func filterByLabels[T labeled](items []T, filters []string) []T {
	filtered := items[:0]
	for i := range items {
		if matchLabels(items[i].GetLabels(), filters) {
			filtered = append(filtered, items[i])
		}
	}
	return filtered
}

func matchLabels(labels map[string]string, filters []string) bool {
	if len(labels) == 0 {
		return len(filters) == 0
	}
	for _, f := range filters {
		k, v, hasValue := strings.Cut(f, "=")
		if !hasValue {
			if _, exists := labels[k]; !exists {
				return false
			}
			continue
		}
		got, exists := labels[k]
		if !exists || got != v {
			return false
		}
	}
	return true
}
