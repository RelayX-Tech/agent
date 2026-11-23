// Package tests 校验 Agent 的控制通道边界（AC2：无 HTTPS/JSON 控制契约）。
package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentHasNoHTTPControlEndpoint 扫描 Agent 源码，禁止出现 HTTP/JSON 服务端（控制通道只允许 gRPC）。
func TestAgentHasNoHTTPControlEndpoint(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"http.Server{", "ListenAndServe", "ListenAndServeTLS", "http.HandleFunc("}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "tests", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, bad := range forbidden {
			if strings.Contains(string(src), bad) {
				t.Errorf("%s 出现 HTTP 控制端点 %q：Agent 控制通道只允许 gRPC", path, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
