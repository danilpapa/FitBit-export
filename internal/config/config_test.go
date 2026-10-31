package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveEnvValue(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{
			name:     "replaces existing value",
			existing: "FIRST=1\nTOKEN=old\n# comment\nLAST=2\n",
			want:     "FIRST=1\nTOKEN=new\n# comment\nLAST=2\n",
		},
		{
			name:     "appends missing value",
			existing: "FIRST=1",
			want:     "FIRST=1\nTOKEN=new\n",
		},
		{
			name:     "ignores keys with the same prefix",
			existing: "TOKEN_OLD=1\n",
			want:     "TOKEN_OLD=1\nTOKEN=new\n",
		},
		{
			name: "creates empty file content",
			want: "TOKEN=new\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(test.existing), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := saveEnvValue(path, "TOKEN", "new"); err != nil {
				t.Fatalf("saveEnvValue() error = %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("file = %q, want %q", got, test.want)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("mode = %o, want 600", info.Mode().Perm())
			}
		})
	}
}

func TestSaveEnvValueCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if err := saveEnvValue(path, "TOKEN", "new"); err != nil {
		t.Fatalf("saveEnvValue() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "TOKEN=new\n" {
		t.Fatalf("file = %q", got)
	}
}
