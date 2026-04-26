package storage

import (
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrubArgsFromOpts(t *testing.T) {
	tests := []struct {
		name    string
		opts    map[string]string
		want    []string
		wantErr bool
	}{
		{name: "empty", opts: nil, want: nil},
		{name: "readonly", opts: map[string]string{"readonly": "true"}, want: []string{"-r"}},
		{name: "force", opts: map[string]string{"force": "true"}, want: []string{"-f"}},
		{name: "readonly_false_omitted", opts: map[string]string{"readonly": "false"}, want: []string{}},
		{name: "ioprio_class_idle", opts: map[string]string{"ioprio_class": "3"}, want: []string{"-c", "3"}},
		{name: "ioprio_classdata_highest", opts: map[string]string{"ioprio_classdata": "0"}, want: []string{"-n", "0"}},
		{name: "ioprio_classdata_lowest", opts: map[string]string{"ioprio_classdata": "7"}, want: []string{"-n", "7"}},
		{
			name: "all_combined",
			opts: map[string]string{"readonly": "true", "force": "true", "ioprio_class": "2", "ioprio_classdata": "4"},
			want: []string{"-r", "-f", "-c", "2", "-n", "4"},
		},
		{name: "unknown_key", opts: map[string]string{"bogus": "1"}, wantErr: true},
		{name: "ioprio_class_out_of_range", opts: map[string]string{"ioprio_class": "4"}, wantErr: true},
		{name: "ioprio_class_negative", opts: map[string]string{"ioprio_class": "-1"}, wantErr: true},
		{name: "ioprio_class_not_int", opts: map[string]string{"ioprio_class": "abc"}, wantErr: true},
		{name: "ioprio_classdata_out_of_range", opts: map[string]string{"ioprio_classdata": "8"}, wantErr: true},
		{name: "ioprio_classdata_negative", opts: map[string]string{"ioprio_classdata": "-1"}, wantErr: true},
		{name: "ioprio_classdata_not_int", opts: map[string]string{"ioprio_classdata": "abc"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := scrubArgsFromOpts(tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				var ve *config.ValidationError
				require.ErrorAs(t, err, &ve)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
