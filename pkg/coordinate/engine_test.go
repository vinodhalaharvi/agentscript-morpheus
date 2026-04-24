package coordinate

import "testing"

func TestLooksLikeFilePath(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		// Engine-internal keys — always reject
		{"__tick__/1", false},
		{"__input__", false},
		{"__engine__", false},

		// Nested paths — accept
		{"cmd/server/main.go", true},
		{"internal/service/config.go", true},
		{"api/proto/taskmgr/v1/task.proto", true},
		{"migrations/000001_create.up.sql", true},

		// Leaf files by extension — accept
		{"go.mod", true},
		{"go.sum", true},
		{"main.go", true},
		{"config.go", true},
		{"config_test.go", true},
		{"Dockerfile", true},
		{"Makefile", true},
		{"docker-compose.yaml", true},

		// Coordination status — reject (no extension, no slash)
		{"module_ready", false},
		{"code_ready", false},
		{"tests_ready", false},
		{"build-status", false},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got := looksLikeFilePath(tc.key)
			if got != tc.want {
				t.Errorf("looksLikeFilePath(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}
