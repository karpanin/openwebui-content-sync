package adapter

import (
	"context"
	"testing"
	"time"

	"github.com/openwebui-content-sync/internal/config"
)

func TestGitLabAdapter_Name(t *testing.T) {
	adapter := &GitLabAdapter{}
	if adapter.Name() != "gitlab" {
		t.Errorf("Expected name 'gitlab', got '%s'", adapter.Name())
	}
}

func TestGitLabAdapter_GetSetLastSync(t *testing.T) {
	adapter := &GitLabAdapter{}
	now := time.Now()

	adapter.SetLastSync(now)
	if !adapter.GetLastSync().Equal(now) {
		t.Errorf("Expected last sync time %v, got %v", now, adapter.GetLastSync())
	}
}

func TestNewGitLabAdapter(t *testing.T) {
	tests := []struct {
		name        string
		config      config.GitLabConfig
		expectError bool
	}{
		{
			name: "valid config cloud",
			config: config.GitLabConfig{
				Token: "test-token",
				Mappings: []config.RepositoryMapping{
					{Repository: "owner/repo", KnowledgeID: "knowledge-id"},
				},
			},
			expectError: false,
		},
		{
			name: "valid config on-prem",
			config: config.GitLabConfig{
				Token: "test-token",
				BaseURL: "https://gitlab.example.com",
				Mappings: []config.RepositoryMapping{
					{Repository: "owner/repo", KnowledgeID: "knowledge-id"},
				},
			},
			expectError: false,
		},
		{
			name: "missing token",
			config: config.GitLabConfig{
				Token: "",
				Mappings: []config.RepositoryMapping{
					{Repository: "owner/repo", KnowledgeID: "knowledge-id"},
				},
			},
			expectError: true,
		},
		{
			name: "no mappings",
			config: config.GitLabConfig{
				Token:    "test-token",
				Mappings: []config.RepositoryMapping{},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := NewGitLabAdapter(tt.config, "")
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if adapter == nil {
				t.Errorf("Expected adapter but got nil")
			}
		})
	}
}

func TestIsGitLabTextFile(t *testing.T) {
	tests := []struct {
		filename string
		expected bool
	}{
		{"test.md", true},
		{"test.txt", true},
		{"test.go", true},
		{"test.py", true},
		{"test.js", true},
		{"test.ts", true},
		{"test.json", true},
		{"test.yaml", true},
		{"test.yml", true},
		{"test.xml", true},
		{"test.html", true},
		{"test.css", true},
		{"test.sh", true},
		{"test.dockerfile", true},
		{"test.gitignore", true},
		{"test.env", true},
		{"test.png", false},
		{"test.jpg", false},
		{"test.jpeg", false},
		{"test.gif", false},
		{"test.exe", false},
		{"test.dll", false},
		{"test.so", false},
		{"test.dylib", false},
		{"test", true},     // No extension should be considered text
		{"test.TXT", true}, // Case insensitive
		{"test.MD", true},  // Case insensitive
	}

	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			result := isGitLabTextFile(test.filename)
			if result != test.expected {
				t.Errorf("isGitLabTextFile(%s) = %v, expected %v", test.filename, result, test.expected)
			}
		})
	}
}

func TestGitLabAdapter_FetchFiles_Error(t *testing.T) {
	// This test would require mocking the GitLab API or using a real token.
	// For now, testing error cases with invalid tokens/urls behaves unpredictably depending on network.
	// We skip the fetch test here to avoid flaky tests without mocks.
	// In a real scenario, we would use httptest to mock the GitLab server.
}
