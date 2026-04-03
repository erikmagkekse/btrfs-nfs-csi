package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchLabels(t *testing.T) {
	tests := []struct {
		name    string
		labels  map[string]string
		filters []string
		want    bool
	}{
		{"nil_labels_no_filters", nil, nil, true},
		{"nil_labels_with_filter", nil, []string{"env=prod"}, false},
		{"empty_labels_no_filters", map[string]string{}, nil, true},
		{"empty_labels_with_filter", map[string]string{}, []string{"env=prod"}, false},
		{"exact_match", map[string]string{"env": "prod"}, []string{"env=prod"}, true},
		{"no_match_value", map[string]string{"env": "dev"}, []string{"env=prod"}, false},
		{"no_match_key", map[string]string{"team": "backend"}, []string{"env=prod"}, false},
		{"multiple_filters_all_match", map[string]string{"env": "prod", "team": "be"}, []string{"env=prod", "team=be"}, true},
		{"multiple_filters_partial", map[string]string{"env": "prod"}, []string{"env=prod", "team=be"}, false},
		{"bare_key_exists", map[string]string{"env": "prod"}, []string{"env"}, true},
		{"bare_key_missing", map[string]string{"team": "be"}, []string{"env"}, false},
		{"bare_key_empty_value", map[string]string{"env": ""}, []string{"env"}, true},
		{"empty_value_filter", map[string]string{"env": ""}, []string{"env="}, true},
		{"empty_value_no_match", map[string]string{"env": "prod"}, []string{"env="}, false},
		{"extra_labels_ok", map[string]string{"env": "prod", "team": "be", "x": "y"}, []string{"env=prod"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchLabels(tt.labels, tt.filters))
		})
	}
}

func TestGenerateLabelQuery(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   string
	}{
		{"empty", nil, ""},
		{"empty_slice", []string{}, ""},
		{"single", []string{"env=prod"}, "label=env%3Dprod"},
		{"multiple", []string{"env=prod", "team=be"}, "label=env%3Dprod&label=team%3Dbe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, generateLabelQuery(tt.labels).Encode())
		})
	}
}
