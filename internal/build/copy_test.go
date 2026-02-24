package build

import (
	"testing"
)

func TestParseCopy(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		workdir string
		src     string
		dest    string
		wantErr bool
	}{
		{
			name:  "absolute dest",
			input: "file.txt /opt/file.txt",
			src:   "file.txt",
			dest:  "/opt/file.txt",
		},
		{
			name:    "relative dest with workdir",
			input:   "file.txt out/",
			workdir: "/app",
			src:     "file.txt",
			dest:    "/app/out",
		},
		{
			name:    "relative dest without workdir",
			input:   "file.txt out/",
			wantErr: true,
		},
		{
			name:    "missing destination",
			input:   "file.txt",
			wantErr: true,
		},
		{
			name:    "too many tokens",
			input:   "a b c",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dest, err := parseCopy(tt.input, tt.workdir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertParseCopy(t, src, dest, tt.src, tt.dest)
		})
	}
}

func assertParseCopy(t *testing.T, gotSrc, gotDest, wantSrc, wantDest string) {
	t.Helper()
	if gotSrc != wantSrc {
		t.Errorf("src = %q, want %q", gotSrc, wantSrc)
	}
	if gotDest != wantDest {
		t.Errorf("dest = %q, want %q", gotDest, wantDest)
	}
}

func TestParseStageCopy(t *testing.T) {
	tests := []struct {
		name  string
		input string
		stage string
		path  string
		ok    bool
	}{
		{
			name:  "valid stage copy",
			input: "build:/app/bin",
			stage: "build",
			path:  "/app/bin",
			ok:    true,
		},
		{
			name:  "no colon",
			input: "/usr/local/bin",
		},
		{
			name:  "colon at start",
			input: ":/some/path",
		},
		{
			name:  "colon after slash",
			input: "/foo:bar",
		},
		{
			name:  "slash in prefix",
			input: "some/stage:path",
		},
		{
			name:  "simple host path",
			input: "file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, path, ok := parseStageCopy(tt.input)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			if stage != tt.stage {
				t.Errorf("stage = %q, want %q", stage, tt.stage)
			}
			if path != tt.path {
				t.Errorf("path = %q, want %q", path, tt.path)
			}
		})
	}
}
