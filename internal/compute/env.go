package compute

import (
	"strings"
)

// Merges base environment variables with overrides, caller values winning.
//
// Each entry is "KEY=value". When the same key appears in both slices the
// override entry is kept and the base entry is dropped. Entries without an
// '=' separator are kept as-is from base.
func MergeEnv(base, overrides []string) []string {
	overrideKeys := make(map[string]struct{}, len(overrides))
	for _, e := range overrides {
		if idx := strings.IndexByte(e, '='); idx > 0 {
			overrideKeys[e[:idx]] = struct{}{}
		}
	}

	out := make([]string, 0, len(base)+len(overrides))
	for _, e := range base {
		if idx := strings.IndexByte(e, '='); idx > 0 {
			if _, shadowed := overrideKeys[e[:idx]]; shadowed {
				continue
			}
		}
		out = append(out, e)
	}
	return append(out, overrides...)
}
