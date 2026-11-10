package agent

import (
	"testing"

	"github.com/cruciblehq/spec/compute"
)

func TestRegistry(t *testing.T) {
	r := newRegistry()
	id := compute.ContainerID("handle-1")

	if _, ok := r.get(id); ok {
		t.Fatal("empty registry returned a container")
	}

	c := &fakeContainer{}
	r.add(id, c)
	got, ok := r.get(id)
	if !ok {
		t.Fatal("get after add missed")
	}
	if got != c {
		t.Fatal("get returned a different container")
	}

	r.remove(id)
	if _, ok := r.get(id); ok {
		t.Fatal("get after remove still hit")
	}
}
