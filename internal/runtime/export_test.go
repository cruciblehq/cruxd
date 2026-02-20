package runtime

import (
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestManifestGCLabels(t *testing.T) {
	m := ocispec.Manifest{
		Config: ocispec.Descriptor{
			Digest: digest.FromString("config"),
		},
		Layers: []ocispec.Descriptor{
			{Digest: digest.FromString("layer0")},
			{Digest: digest.FromString("layer1")},
		},
	}

	labels := manifestGCLabels(m)

	configLabel := labels["containerd.io/gc.ref.content.config"]
	if configLabel != m.Config.Digest.String() {
		t.Fatalf("config label = %q, want %q", configLabel, m.Config.Digest.String())
	}

	for i, layer := range m.Layers {
		key := "containerd.io/gc.ref.content.l." + string(rune('0'+i))
		got := labels[key]
		if got != layer.Digest.String() {
			t.Fatalf("labels[%q] = %q, want %q", key, got, layer.Digest.String())
		}
	}

	if len(labels) != 3 {
		t.Fatalf("len(labels) = %d, want 3", len(labels))
	}
}

func TestManifestGCLabelsNoLayers(t *testing.T) {
	m := ocispec.Manifest{
		Config: ocispec.Descriptor{
			Digest: digest.FromString("config-only"),
		},
	}

	labels := manifestGCLabels(m)
	if len(labels) != 1 {
		t.Fatalf("len(labels) = %d, want 1", len(labels))
	}
	if labels["containerd.io/gc.ref.content.config"] != m.Config.Digest.String() {
		t.Fatal("config label mismatch")
	}
}
