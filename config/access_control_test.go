package config

import (
	"os"
	"path/filepath"
	"strings"
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
				{Type: "ChatRoomMgr", ID: "ChatRoomMgr0", Method: "ListChatRooms", Access: AccessAllow},
				{Type: "ConfigManager", ID: "ConfigManager0", Method: "GetConfig", Access: AccessInternal},
			},
			wantErr: false,
		},
		{
			name: "valid regex patterns",
			rules: []AccessRule{
				{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]+/", Method: "/(Join|Leave|SendMessage)/", Access: AccessAllow},
				{Type: "/.*Scheduler.*/", Method: "/.*/", Access: AccessInternal},
			},
			wantErr: false,
		},
		{
			name: "optional id and method fields",
			rules: []AccessRule{
				{Type: "InternalScheduler", Access: AccessInternal},                           // ID and Method omitted
				{Type: "ChatRoomMgr", ID: "ChatRoomMgr0", Access: AccessInternal},             // Method omitted
				{Type: "Counter", Method: "/(Increment|Decrement|Get)/", Access: AccessAllow}, // ID omitted
			},
			wantErr: false,
		},
		{
			name: "missing type",
			rules: []AccessRule{
				{ID: "SomeID", Method: "Method", Access: AccessAllow},
			},
			wantErr: true,
			errMsg:  "missing type",
		},
		{
			name: "invalid type regex pattern",
			rules: []AccessRule{
				{Type: "/ChatRoom-[invalid/", Method: "Method", Access: AccessAllow},
			},
			wantErr: true,
			errMsg:  "invalid type pattern",
		},
		{
			name: "invalid id regex pattern",
			rules: []AccessRule{
				{Type: "ChatRoom", ID: "/[invalid/", Method: "Method", Access: AccessAllow},
			},
			wantErr: true,
			errMsg:  "invalid id pattern",
		},
		{
			name: "invalid method regex pattern",
			rules: []AccessRule{
				{Type: "Object", Method: "/(invalid[/", Access: AccessAllow},
			},
			wantErr: true,
			errMsg:  "invalid method pattern",
		},
		{
			name: "missing access",
			rules: []AccessRule{
				{Type: "Object", Method: "Method"},
			},
			wantErr: true,
			errMsg:  "missing access",
		},
		{
			name: "invalid access level",
			rules: []AccessRule{
				{Type: "Object", Method: "Method", Access: "INVALID"},
			},
			wantErr: true,
			errMsg:  "invalid access level",
		},
		{
			name: "all valid access levels",
			rules: []AccessRule{
				{Type: "Obj1", Method: "M1", Access: AccessReject},
				{Type: "Obj2", Method: "M2", Access: AccessInternal},
				{Type: "Obj3", Method: "M3", Access: AccessExternal},
				{Type: "Obj4", Method: "M4", Access: AccessAllow},
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
		{Type: "ChatRoomMgr", ID: "ChatRoomMgr0", Method: "ListChatRooms", Access: AccessAllow},
		// ChatRoomMgr: other methods internal only
		{Type: "ChatRoomMgr", Method: "/.*/", Access: AccessInternal},
		// ChatRoom: clients can interact with these methods
		{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]+/", Method: "/(Join|Leave|SendMessage)/", Access: AccessAllow},
		// ChatRoom: other methods internal only
		{Type: "ChatRoom", Method: "/.*/", Access: AccessInternal},
		// Counter: external only method
		{Type: "Counter", Method: "ExternalOnly", Access: AccessExternal},
		// Default: deny everything else
		{Type: "/.*/", Access: AccessReject},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name       string
		objectType string
		objectID   string
		method     string
		allowed    bool
	}{
		// ChatRoomMgr tests
		{name: "ChatRoomMgr ListChatRooms allowed", objectType: "ChatRoomMgr", objectID: "ChatRoomMgr0", method: "ListChatRooms", allowed: true},
		{name: "ChatRoomMgr InternalMethod denied", objectType: "ChatRoomMgr", objectID: "ChatRoomMgr0", method: "InternalMethod", allowed: false},
		{name: "ChatRoomMgr CreateRoom denied", objectType: "ChatRoomMgr", objectID: "ChatRoomMgr0", method: "CreateRoom", allowed: false},

		// ChatRoom tests
		{name: "ChatRoom Join allowed", objectType: "ChatRoom", objectID: "General", method: "Join", allowed: true},
		{name: "ChatRoom Leave allowed", objectType: "ChatRoom", objectID: "General", method: "Leave", allowed: true},
		{name: "ChatRoom SendMessage allowed", objectType: "ChatRoom", objectID: "test_room", method: "SendMessage", allowed: true},
		{name: "ChatRoom NotifyMembers denied", objectType: "ChatRoom", objectID: "General", method: "NotifyMembers", allowed: false},

		// Counter external-only tests
		{name: "Counter ExternalOnly allowed", objectType: "Counter", objectID: "test", method: "ExternalOnly", allowed: true},

		// Unmatched objects denied
		{name: "Unknown type denied", objectType: "UnknownType", objectID: "any", method: "AnyMethod", allowed: false},
		{name: "InternalScheduler denied", objectType: "InternalScheduler", objectID: "sched-1", method: "Run", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckClientAccess(tt.objectType, tt.objectID, tt.method)
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
		{Type: "ChatRoom", Method: "/.*/", Access: AccessInternal},
		// Counter: external only method
		{Type: "Counter", Method: "ExternalOnly", Access: AccessExternal},
		// Counter: other methods allowed
		{Type: "Counter", Method: "/.*/", Access: AccessAllow},
		// Default: reject
		{Type: "/.*/", Access: AccessReject},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name       string
		objectType string
		objectID   string
		method     string
		allowed    bool
	}{
		// ChatRoom INTERNAL - nodes can access
		{name: "ChatRoom internal access allowed", objectType: "ChatRoom", objectID: "test", method: "NotifyMembers", allowed: true},
		{name: "ChatRoom internal SendMessage allowed", objectType: "ChatRoom", objectID: "test", method: "SendMessage", allowed: true},

		// Counter external-only - nodes cannot access
		{name: "Counter ExternalOnly denied for nodes", objectType: "Counter", objectID: "test", method: "ExternalOnly", allowed: false},
		{name: "Counter Increment allowed for nodes", objectType: "Counter", objectID: "test", method: "Increment", allowed: true},

		// Rejected objects
		{name: "Unknown type denied", objectType: "UnknownType", objectID: "any", method: "Method", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckNodeAccess(tt.objectType, tt.objectID, tt.method)
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
		{Type: "ChatRoom", Method: "SpecificMethod", Access: AccessAllow}, // Specific method first
		{Type: "ChatRoom", Method: "/.*/", Access: AccessInternal},        // Catch-all second
		{Type: "/.*/", Access: AccessReject},                              // Default deny last
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// SpecificMethod should match first rule (ALLOW) - clients allowed
	if err := v.CheckClientAccess("ChatRoom", "test", "SpecificMethod"); err != nil {
		t.Errorf("SpecificMethod should be allowed for clients: %v", err)
	}

	// OtherMethod should match second rule (INTERNAL) - clients denied
	if err := v.CheckClientAccess("ChatRoom", "test", "OtherMethod"); err == nil {
		t.Error("OtherMethod should be denied for clients (INTERNAL)")
	}

	// OtherMethod should match second rule (INTERNAL) - nodes allowed
	if err := v.CheckNodeAccess("ChatRoom", "test", "OtherMethod"); err != nil {
		t.Errorf("OtherMethod should be allowed for nodes: %v", err)
	}
}

func TestAccessValidator_DefaultDeny(t *testing.T) {
	// Test that no matching rule results in REJECT
	rules := []AccessRule{
		{Type: "SpecificType", ID: "SpecificID", Method: "SpecificMethod", Access: AccessAllow},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// Unmatched objects should be denied
	if err := v.CheckClientAccess("OtherType", "OtherID", "OtherMethod"); err == nil {
		t.Error("unmatched access should be denied for clients")
	}
	if err := v.CheckNodeAccess("OtherType", "OtherID", "OtherMethod"); err == nil {
		t.Error("unmatched access should be denied for nodes")
	}
}

func TestAccessValidator_OptionalFields(t *testing.T) {
	// Test that omitted ID and Method fields match all
	rules := []AccessRule{
		// Internal scheduler: only type specified - matches all IDs and methods
		{Type: "InternalScheduler", Access: AccessInternal},
		// Counter: only type and method specified - matches all IDs
		{Type: "Counter", Method: "/(Increment|Decrement)/", Access: AccessAllow},
		// Default deny
		{Type: "/.*/", Access: AccessReject},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// InternalScheduler - any ID, any method should be INTERNAL
	if err := v.CheckNodeAccess("InternalScheduler", "sched-1", "Run"); err != nil {
		t.Errorf("InternalScheduler should be allowed for nodes: %v", err)
	}
	if err := v.CheckNodeAccess("InternalScheduler", "any-id", "AnyMethod"); err != nil {
		t.Errorf("InternalScheduler with any ID should be allowed for nodes: %v", err)
	}
	if err := v.CheckClientAccess("InternalScheduler", "sched-1", "Run"); err == nil {
		t.Error("InternalScheduler should be denied for clients")
	}

	// Counter - any ID, specific methods should be ALLOW
	if err := v.CheckClientAccess("Counter", "counter-1", "Increment"); err != nil {
		t.Errorf("Counter.Increment should be allowed for clients: %v", err)
	}
	if err := v.CheckClientAccess("Counter", "any-id", "Decrement"); err != nil {
		t.Errorf("Counter.Decrement should be allowed for clients: %v", err)
	}
	// Other methods should be denied (falls through to default)
	if err := v.CheckClientAccess("Counter", "counter-1", "Reset"); err == nil {
		t.Error("Counter.Reset should be denied for clients")
	}
}

func TestAccessValidator_IDPatternMatching(t *testing.T) {
	// Test ID pattern matching specifically
	rules := []AccessRule{
		// ChatRoom with specific ID pattern
		{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]{1,50}/", Method: "Join", Access: AccessAllow},
		// UserSession with specific user ID pattern
		{Type: "UserSession", ID: "/user-[0-9]+/", Access: AccessAllow},
		// Default deny
		{Type: "/.*/", Access: AccessReject},
	}

	v, err := NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name       string
		objectType string
		objectID   string
		method     string
		allowed    bool
	}{
		// Valid ChatRoom IDs
		{name: "ChatRoom valid ID", objectType: "ChatRoom", objectID: "General", method: "Join", allowed: true},
		{name: "ChatRoom valid ID with dash", objectType: "ChatRoom", objectID: "test-room", method: "Join", allowed: true},
		// Invalid ChatRoom IDs (too long or empty)
		{name: "ChatRoom empty ID", objectType: "ChatRoom", objectID: "", method: "Join", allowed: false},
		// Valid UserSession IDs
		{name: "UserSession valid ID", objectType: "UserSession", objectID: "user-123", method: "GetData", allowed: true},
		{name: "UserSession valid ID with many digits", objectType: "UserSession", objectID: "user-9999999", method: "GetData", allowed: true},
		// Invalid UserSession IDs
		{name: "UserSession invalid ID format", objectType: "UserSession", objectID: "admin-123", method: "GetData", allowed: false},
		{name: "UserSession no digits", objectType: "UserSession", objectID: "user-abc", method: "GetData", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckClientAccess(tt.objectType, tt.objectID, tt.method)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected denied, got allowed")
			}
		})
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

func TestPatternMatcher_MatchAll(t *testing.T) {
	m, err := parsePatternOrMatchAll("")
	if err != nil {
		t.Fatalf("failed to parse empty pattern: %v", err)
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
  - type: ChatRoomMgr
    id: ChatRoomMgr0
    method: ListChatRooms
    access: ALLOW

  - type: ChatRoomMgr
    method: /.*/
    access: INTERNAL

  - type: ChatRoom
    id: /[a-zA-Z0-9_-]+/
    method: /(Join|Leave|SendMessage)/
    access: ALLOW

  - type: ChatRoom
    method: /.*/
    access: INTERNAL

  - type: /.*/
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
	if cfg.AccessRules[0].Type != "ChatRoomMgr" {
		t.Errorf("expected first rule type ChatRoomMgr, got %s", cfg.AccessRules[0].Type)
	}
	if cfg.AccessRules[0].ID != "ChatRoomMgr0" {
		t.Errorf("expected first rule ID ChatRoomMgr0, got %s", cfg.AccessRules[0].ID)
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
	if err := v.CheckClientAccess("ChatRoomMgr", "ChatRoomMgr0", "ListChatRooms"); err != nil {
		t.Errorf("ChatRoomMgr/ChatRoomMgr0.ListChatRooms should be allowed for clients: %v", err)
	}
	if err := v.CheckClientAccess("ChatRoomMgr", "ChatRoomMgr0", "CreateRoom"); err == nil {
		t.Error("ChatRoomMgr/ChatRoomMgr0.CreateRoom should be denied for clients (INTERNAL)")
	}
	if err := v.CheckClientAccess("ChatRoom", "General", "Join"); err != nil {
		t.Errorf("ChatRoom/General.Join should be allowed for clients: %v", err)
	}
	if err := v.CheckClientAccess("ChatRoom", "General", "NotifyMembers"); err == nil {
		t.Error("ChatRoom/General.NotifyMembers should be denied for clients (INTERNAL)")
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
	return strings.Contains(s, substr)
}
