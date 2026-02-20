package runtime

import (
	"sort"
	"testing"
)

func TestMergeEnv(t *testing.T) {
	tests := []struct {
		name      string
		base      []string
		overrides []string
		want      []string
	}{
		{
			name:      "override existing key",
			base:      []string{"A=1", "B=2"},
			overrides: []string{"A=override"},
			want:      []string{"A=override", "B=2"},
		},
		{
			name:      "add new key",
			base:      []string{"A=1"},
			overrides: []string{"B=2"},
			want:      []string{"A=1", "B=2"},
		},
		{
			name:      "empty base",
			base:      nil,
			overrides: []string{"A=1"},
			want:      []string{"A=1"},
		},
		{
			name:      "empty overrides",
			base:      []string{"A=1"},
			overrides: nil,
			want:      []string{"A=1"},
		},
		{
			name:      "both empty",
			base:      nil,
			overrides: nil,
			want:      []string{},
		},
		{
			name:      "value with equals sign",
			base:      []string{"CMD=foo=bar"},
			overrides: nil,
			want:      []string{"CMD=foo=bar"},
		},
		{
			name:      "malformed entries skipped",
			base:      []string{"NOEQUALS", "A=1"},
			overrides: []string{"ALSO_BAD", "B=2"},
			want:      []string{"A=1", "B=2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeEnv(tt.base, tt.overrides)
			sort.Strings(got)
			sort.Strings(tt.want)

			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNextExecID(t *testing.T) {
	a := nextExecID()
	b := nextExecID()
	if a == b {
		t.Fatalf("nextExecID returned duplicate: %q", a)
	}
	if a == "" || b == "" {
		t.Fatal("nextExecID returned empty string")
	}
}
