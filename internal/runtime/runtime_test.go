package runtime

import (
	"strings"
	"testing"
)

func TestImageTag(t *testing.T) {
	tag := imageTag("/some/archive.tar")

	if !strings.HasPrefix(tag, "import/") {
		t.Fatalf("tag %q missing import/ prefix", tag)
	}
	if !strings.HasSuffix(tag, ":latest") {
		t.Fatalf("tag %q missing :latest suffix", tag)
	}

	if imageTag("/some/archive.tar") != tag {
		t.Fatal("imageTag is not deterministic")
	}

	if imageTag("/other/archive.tar") == tag {
		t.Fatal("different paths produced the same tag")
	}
}

func TestDefaultPlatform(t *testing.T) {
	p := defaultPlatform()
	if !strings.HasPrefix(p, "linux/") {
		t.Fatalf("defaultPlatform = %q, want linux/<arch>", p)
	}
	parts := strings.Split(p, "/")
	if len(parts) != 2 || parts[1] == "" {
		t.Fatalf("defaultPlatform = %q, want linux/<arch>", p)
	}
}
