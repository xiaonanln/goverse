package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewAccessValidator(t *testing.T) {
	tests := []struct {
		name    string
		rules   []AccessRule
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty rules",
			rules:   []AccessRule{},
			wantErr: false,
		},
		{
			name: "valid literal patterns",
			rules: []AccessRule{
				{Object: "ChatRoomMgr0", Method: "ListChatRooms", Access: AccessAllow},
				{Object: "ConfigManager0", Method: "GetConfig", Access: AccessInternal},
			},
			wantErr: false,
		},
		{
			name: "valid regex patterns",
			rules: []AccessRule{
				{Object: "/ChatRoom-.*/", Method: "/(Join|Leave|SendMessage)/", Access: AccessAllow},
				{Object: "/.*Scheduler.*/", Method: "/.*/", Access: AccessInternal},
			},
			wantErr: false,
		},
		{
			name: "mixed literal and regex patterns",
			rules: []AccessRule{
				{Object: "ChatRoomMgr0", Method: "/.*/", Access: AccessInternal},
				{Object: "/ChatRoom-[a-zA-Z0-9_-]+/", Method: "SendMessage", Access: AccessAllow},
			},
			wantErr: false,
		},
		{
			name: "invalid object regex pattern",
			rules: []AccessRule{
				{Object: "/ChatRoom-[invalid/", Method: "Method", Access: AccessAllow},
			},
			wantErr: true,
			errMsg:  "invalid object pattern",
		},
		{
			name: "invalid method regex pattern",
			rules: []AccessRule{
				{Object: "Object", Method: "/(invalid[/", Access: AccessAllow},
			},
			wantErr: true,
			errMsg:  "invalid method pattern",
		},
		{
			name: "invalid access level",
			rules: []AccessRule{
				{Object: "Object", Method: "Method", Access: "INVALID"},
			},
			wantErr: true,
			errMsg:  "invalid access level",
		},
		{
			name: "all valid access levels",
			rules: []AccessRule{
				{Object: "Obj1", Method: "M1", Access: AccessReject},
				{Object: "Obj2", Method: "M2", Access: AccessInternal},
				{Object: "Obj3", Method: "M3", Access: AccessExternal},
				{Object: "Obj4", Method: "M4", Access: AccessAllow},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := NewAccessValidator(tt.rules)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(tt.rules) > 0 && v == nil {
					t.Fatalf("expected non-nil validator for non-empty rules")
				}
			}
		})
	}
}

func TestAccessValidator_CheckClientAccess(t *testing.T) {
	rules := []AccessRule{
		// ChatRoomMgr singleton: clients can list rooms
		{Object: "ChatRoomMgr0", Method: "ListChatRooms", Access: AccessAllow},
		// ChatRoomMgr: other methods internal only
		{Object: "ChatRoomMgr0", Method: "/.*/", Access: AccessInternal},
		// ChatRoom: clients can interact with these methods
		{Object: "/ChatRoom-[a-zA-Z0-9_-]+/", Method: "/(Join|Leave|SendMessage)/", Access: AccessAllow},
		// ChatRoom: other methods internal only
		{Object: "/ChatRoom-[a-zA-Z0-9_-]+/", Method: "/.*/", Access: AccessInternal},
		// Counter: external only method
		{Object: "/Counter-.*/", Method: "ExternalOnly", Access: AccessExternal},
		// Default: deny everything else
		{Object: "/.*/", Method: "/.*/", Access: AccessReject},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name     string
		objectID string
		method   string
		allowed  bool
	}{
		// ChatRoomMgr0 tests
		{name: "ChatRoomMgr0 ListChatRooms allowed", objectID: "ChatRoomMgr0", method: "ListChatRooms", allowed: true},
		{name: "ChatRoomMgr0 InternalMethod denied", objectID: "ChatRoomMgr0", method: "InternalMethod", allowed: false},
		{name: "ChatRoomMgr0 CreateRoom denied", objectID: "ChatRoomMgr0", method: "CreateRoom", allowed: false},

		// ChatRoom tests
		{name: "ChatRoom Join allowed", objectID: "ChatRoom-General", method: "Join", allowed: true},
		{name: "ChatRoom Leave allowed", objectID: "ChatRoom-General", method: "Leave", allowed: true},
		{name: "ChatRoom SendMessage allowed", objectID: "ChatRoom-test_room", method: "SendMessage", allowed: true},
		{name: "ChatRoom NotifyMembers denied", objectID: "ChatRoom-General", method: "NotifyMembers", allowed: false},

		// Counter external-only tests
		{name: "Counter ExternalOnly allowed", objectID: "Counter-test", method: "ExternalOnly", allowed: true},

		// Unmatched objects denied
		{name: "Unknown object denied", objectID: "UnknownObject", method: "AnyMethod", allowed: false},
		{name: "InternalScheduler denied", objectID: "InternalScheduler-1", method: "Run", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckClientAccess(tt.objectID, tt.method)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected denied, got allowed")
			}
		})
	}
}

func TestAccessValidator_CheckNodeAccess(t *testing.T) {
	rules := []AccessRule{
		// ChatRoom: all methods allowed for internal
		{Object: "/ChatRoom-.*/", Method: "/.*/", Access: AccessInternal},
		// Counter: external only method
		{Object: "/Counter-.*/", Method: "ExternalOnly", Access: AccessExternal},
		// Counter: other methods allowed
		{Object: "/Counter-.*/", Method: "/.*/", Access: AccessAllow},
		// Default: reject
		{Object: "/.*/", Method: "/.*/", Access: AccessReject},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name     string
		objectID string
		method   string
		allowed  bool
	}{
		// ChatRoom INTERNAL - nodes can access
		{name: "ChatRoom internal access allowed", objectID: "ChatRoom-test", method: "NotifyMembers", allowed: true},
		{name: "ChatRoom internal SendMessage allowed", objectID: "ChatRoom-test", method: "SendMessage", allowed: true},

		// Counter external-only - nodes cannot access
		{name: "Counter ExternalOnly denied for nodes", objectID: "Counter-test", method: "ExternalOnly", allowed: false},
		{name: "Counter Increment allowed for nodes", objectID: "Counter-test", method: "Increment", allowed: true},

		// Rejected objects
		{name: "Unknown object denied", objectID: "UnknownObject", method: "Method", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckNodeAccess(tt.objectID, tt.method)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected denied, got allowed")
			}
		})
	}
}

func TestAccessValidator_FirstMatchWins(t *testing.T) {
	// Test that rules are evaluated top-to-bottom and first match wins
	rules := []AccessRule{
		{Object: "/ChatRoom-.*/", Method: "SpecificMethod", Access: AccessAllow}, // Specific method first
		{Object: "/ChatRoom-.*/", Method: "/.*/", Access: AccessInternal},        // Catch-all second
		{Object: "/.*/", Method: "/.*/", Access: AccessReject},                   // Default deny last
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// SpecificMethod should match first rule (ALLOW) - clients allowed
	if err := v.CheckClientAccess("ChatRoom-test", "SpecificMethod"); err != nil {
		t.Errorf("SpecificMethod should be allowed for clients: %v", err)
	}

	// OtherMethod should match second rule (INTERNAL) - clients denied
	if err := v.CheckClientAccess("ChatRoom-test", "OtherMethod"); err == nil {
		t.Error("OtherMethod should be denied for clients (INTERNAL)")
	}

	// OtherMethod should match second rule (INTERNAL) - nodes allowed
	if err := v.CheckNodeAccess("ChatRoom-test", "OtherMethod"); err != nil {
		t.Errorf("OtherMethod should be allowed for nodes: %v", err)
	}
}

func TestAccessValidator_DefaultDeny(t *testing.T) {
	// Test that no matching rule results in REJECT
	rules := []AccessRule{
		{Object: "SpecificObject", Method: "SpecificMethod", Access: AccessAllow},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// Unmatched objects should be denied
	if err := v.CheckClientAccess("OtherObject", "OtherMethod"); err == nil {
		t.Error("unmatched access should be denied for clients")
	}
	if err := v.CheckNodeAccess("OtherObject", "OtherMethod"); err == nil {
		t.Error("unmatched access should be denied for nodes")
	}
}

func TestPatternMatcher_Literal(t *testing.T) {
	m, err := parsePattern("ExactMatch")
	if err != nil {
		t.Fatalf("failed to parse literal pattern: %v", err)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"ExactMatch", true},
		{"exactmatch", false},
		{"ExactMatchExtra", false},
		{"PrefixExactMatch", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := m.Match(tt.input); got != tt.expected {
			t.Errorf("Match(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestPatternMatcher_Regex(t *testing.T) {
	m, err := parsePattern("/ChatRoom-[a-zA-Z0-9_-]+/")
	if err != nil {
		t.Fatalf("failed to parse regex pattern: %v", err)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"ChatRoom-General", true},
		{"ChatRoom-test_room", true},
		{"ChatRoom-123", true},
		{"ChatRoom-test-room", true},
		{"ChatRoom-", false},           // At least one character after dash
		{"chatroom-test", false},       // Case sensitive
		{"ChatRoomGeneral", false},     // Missing dash
		{"PrefixChatRoom-test", false}, // Prefix not allowed (anchored)
		{"ChatRoom-test-suffix", true}, // Valid pattern
	}

	for _, tt := range tests {
		if got := m.Match(tt.input); got != tt.expected {
			t.Errorf("Match(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestPatternMatcher_WildcardRegex(t *testing.T) {
	m, err := parsePattern("/.*/")
	if err != nil {
		t.Fatalf("failed to parse wildcard pattern: %v", err)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"anything", true},
		{"", true},
		{"multi-word-string", true},
		{"With Spaces", true},
	}

	for _, tt := range tests {
		if got := m.Match(tt.input); got != tt.expected {
			t.Errorf("Match(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestLoadConfigWithAccessRules(t *testing.T) {
	configContent := `
version: 1

cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "127.0.0.1:2379"
    prefix: "/goverse"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"

object_access_rules:
  - object: ChatRoomMgr0
    method: ListChatRooms
    access: ALLOW

  - object: ChatRoomMgr0
    method: /.*/
    access: INTERNAL

  - object: /ChatRoom-[a-zA-Z0-9_-]+/
    method: /(Join|Leave|SendMessage)/
    access: ALLOW

  - object: /ChatRoom-[a-zA-Z0-9_-]+/
    method: /.*/
    access: INTERNAL

  - object: /.*/
    method: /.*/
    access: REJECT
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify access rules were loaded
	if len(cfg.AccessRules) != 5 {
		t.Fatalf("expected 5 access rules, got %d", len(cfg.AccessRules))
	}

	// Verify first rule
	if cfg.AccessRules[0].Object != "ChatRoomMgr0" {
		t.Errorf("expected first rule object ChatRoomMgr0, got %s", cfg.AccessRules[0].Object)
	}
	if cfg.AccessRules[0].Method != "ListChatRooms" {
		t.Errorf("expected first rule method ListChatRooms, got %s", cfg.AccessRules[0].Method)
	}
	if cfg.AccessRules[0].Access != AccessAllow {
		t.Errorf("expected first rule access ALLOW, got %s", cfg.AccessRules[0].Access)
	}

	// Create access validator
	v, err := cfg.NewAccessValidator()
	if err != nil {
		t.Fatalf("failed to create access validator: %v", err)
	}
	if v == nil {
		t.Fatalf("expected non-nil access validator")
	}

	// Verify access control works
	if err := v.CheckClientAccess("ChatRoomMgr0", "ListChatRooms"); err != nil {
		t.Errorf("ChatRoomMgr0.ListChatRooms should be allowed for clients: %v", err)
	}
	if err := v.CheckClientAccess("ChatRoomMgr0", "CreateRoom"); err == nil {
		t.Error("ChatRoomMgr0.CreateRoom should be denied for clients (INTERNAL)")
	}
	if err := v.CheckClientAccess("ChatRoom-General", "Join"); err != nil {
		t.Errorf("ChatRoom-General.Join should be allowed for clients: %v", err)
	}
	if err := v.CheckClientAccess("ChatRoom-General", "NotifyMembers"); err == nil {
		t.Error("ChatRoom-General.NotifyMembers should be denied for clients (INTERNAL)")
	}
}

func TestConfigNewAccessValidator_EmptyRules(t *testing.T) {
	cfg := &Config{
		AccessRules: nil,
	}

	v, err := cfg.NewAccessValidator()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Error("expected nil validator for empty rules")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && contains(s, substr)))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
