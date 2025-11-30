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
				{Type: "ChatRoomMgr", ID: "ChatRoomMgr0", Method: "ListChatRooms", Access: "ALLOW"},
				{Type: "ConfigManager", ID: "ConfigManager0", Method: "GetConfig", Access: "INTERNAL"},
			},
			wantErr: false,
		},
		{
			name: "valid regex patterns",
			rules: []AccessRule{
				{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]+/", Method: "/(Join|Leave|SendMessage)/", Access: "ALLOW"},
				{Type: "/.*Scheduler.*/", Method: "/.*/", Access: "INTERNAL"},
			},
			wantErr: false,
		},
		{
			name: "optional id and method fields",
			rules: []AccessRule{
				{Type: "InternalScheduler", Access: "INTERNAL"},                           // ID and Method omitted
				{Type: "ChatRoomMgr", ID: "ChatRoomMgr0", Access: "INTERNAL"},             // Method omitted
				{Type: "Counter", Method: "/(Increment|Decrement|Get)/", Access: "ALLOW"}, // ID omitted
			},
			wantErr: false,
		},
		{
			name: "missing type",
			rules: []AccessRule{
				{ID: "SomeID", Method: "Method", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "missing type",
		},
		{
			name: "invalid type regex pattern",
			rules: []AccessRule{
				{Type: "/ChatRoom-[invalid/", Method: "Method", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "invalid type pattern",
		},
		{
			name: "invalid id regex pattern",
			rules: []AccessRule{
				{Type: "ChatRoom", ID: "/[invalid/", Method: "Method", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "invalid id pattern",
		},
		{
			name: "invalid method regex pattern",
			rules: []AccessRule{
				{Type: "Object", Method: "/(invalid[/", Access: "ALLOW"},
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
				{Type: "Obj1", Method: "M1", Access: "REJECT"},
				{Type: "Obj2", Method: "M2", Access: "INTERNAL"},
				{Type: "Obj3", Method: "M3", Access: "EXTERNAL"},
				{Type: "Obj4", Method: "M4", Access: "ALLOW"},
			},
			wantErr: false,
		},
		{
			name: "case insensitive access level",
			rules: []AccessRule{
				{Type: "Obj1", Method: "M1", Access: "reject"},
				{Type: "Obj2", Method: "M2", Access: "Internal"},
				{Type: "Obj3", Method: "M3", Access: "EXTERNAL"},
				{Type: "Obj4", Method: "M4", Access: "allow"},
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
		{Type: "ChatRoomMgr", ID: "ChatRoomMgr0", Method: "ListChatRooms", Access: "ALLOW"},
		// ChatRoomMgr: other methods internal only
		{Type: "ChatRoomMgr", Method: "/.*/", Access: "INTERNAL"},
		// ChatRoom: clients can interact with these methods
		{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]+/", Method: "/(Join|Leave|SendMessage)/", Access: "ALLOW"},
		// ChatRoom: other methods internal only
		{Type: "ChatRoom", Method: "/.*/", Access: "INTERNAL"},
		// Counter: external only method
		{Type: "Counter", Method: "ExternalOnly", Access: "EXTERNAL"},
		// Default: deny everything else
		{Type: "/.*/", Access: "REJECT"},
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
		{Type: "ChatRoom", Method: "/.*/", Access: "INTERNAL"},
		// Counter: external only method
		{Type: "Counter", Method: "ExternalOnly", Access: "EXTERNAL"},
		// Counter: other methods allowed
		{Type: "Counter", Method: "/.*/", Access: "ALLOW"},
		// Default: reject
		{Type: "/.*/", Access: "REJECT"},
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
		{Type: "ChatRoom", Method: "SpecificMethod", Access: "ALLOW"}, // Specific method first
		{Type: "ChatRoom", Method: "/.*/", Access: "INTERNAL"},        // Catch-all second
		{Type: "/.*/", Access: "REJECT"},                              // Default deny last
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
		{Type: "SpecificType", ID: "SpecificID", Method: "SpecificMethod", Access: "ALLOW"},
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
		{Type: "InternalScheduler", Access: "INTERNAL"},
		// Counter: only type and method specified - matches all IDs
		{Type: "Counter", Method: "/(Increment|Decrement)/", Access: "ALLOW"},
		// Default deny
		{Type: "/.*/", Access: "REJECT"},
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
		{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]{1,50}/", Method: "Join", Access: "ALLOW"},
		// UserSession with specific user ID pattern
		{Type: "UserSession", ID: "/user-[0-9]+/", Access: "ALLOW"},
		// Default deny
		{Type: "/.*/", Access: "REJECT"},
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
	if cfg.AccessRules[0].Access != "ALLOW" {
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

func TestParseAccessLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected AccessLevel
		wantErr  bool
	}{
		{"REJECT", AccessReject, false},
		{"INTERNAL", AccessInternal, false},
		{"EXTERNAL", AccessExternal, false},
		{"ALLOW", AccessAllow, false},
		// Case insensitive
		{"reject", AccessReject, false},
		{"internal", AccessInternal, false},
		{"external", AccessExternal, false},
		{"allow", AccessAllow, false},
		{"Reject", AccessReject, false},
		{"Internal", AccessInternal, false},
		// Invalid
		{"INVALID", AccessReject, true},
		{"", AccessReject, true},
		{"PERMITTED", AccessReject, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAccessLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, but got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if got != tt.expected {
					t.Errorf("ParseAccessLevel(%q) = %v, want %v", tt.input, got, tt.expected)
				}
			}
		})
	}
}

func TestAccessLevel_String(t *testing.T) {
	tests := []struct {
		level    AccessLevel
		expected string
	}{
		{AccessReject, "REJECT"},
		{AccessInternal, "INTERNAL"},
		{AccessExternal, "EXTERNAL"},
		{AccessAllow, "ALLOW"},
		{AccessLevel(99), "UNKNOWN"}, // Invalid enum value
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("AccessLevel(%d).String() = %q, want %q", tt.level, got, tt.expected)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// Tests for LifecycleValidator

func TestNewLifecycleValidator(t *testing.T) {
	tests := []struct {
		name    string
		rules   []LifecycleRule
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty rules",
			rules:   []LifecycleRule{},
			wantErr: false,
		},
		{
			name: "valid CREATE rule",
			rules: []LifecycleRule{
				{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]+/", Lifecycle: "CREATE", Access: "ALLOW"},
			},
			wantErr: false,
		},
		{
			name: "valid DELETE rule",
			rules: []LifecycleRule{
				{Type: "ChatRoom", Lifecycle: "DELETE", Access: "INTERNAL"},
			},
			wantErr: false,
		},
		{
			name: "valid ALL rule",
			rules: []LifecycleRule{
				{Type: "InternalScheduler", Lifecycle: "ALL", Access: "INTERNAL"},
			},
			wantErr: false,
		},
		{
			name: "missing type",
			rules: []LifecycleRule{
				{ID: "SomeID", Lifecycle: "CREATE", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "missing type",
		},
		{
			name: "missing lifecycle",
			rules: []LifecycleRule{
				{Type: "Object", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "missing lifecycle",
		},
		{
			name: "invalid lifecycle",
			rules: []LifecycleRule{
				{Type: "Object", Lifecycle: "INVALID", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "invalid lifecycle",
		},
		{
			name: "missing access",
			rules: []LifecycleRule{
				{Type: "Object", Lifecycle: "CREATE"},
			},
			wantErr: true,
			errMsg:  "missing access",
		},
		{
			name: "invalid access level",
			rules: []LifecycleRule{
				{Type: "Object", Lifecycle: "CREATE", Access: "INVALID"},
			},
			wantErr: true,
			errMsg:  "invalid access level",
		},
		{
			name: "invalid type regex pattern",
			rules: []LifecycleRule{
				{Type: "/[invalid/", Lifecycle: "CREATE", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "invalid type pattern",
		},
		{
			name: "invalid id regex pattern",
			rules: []LifecycleRule{
				{Type: "Object", ID: "/[invalid/", Lifecycle: "CREATE", Access: "ALLOW"},
			},
			wantErr: true,
			errMsg:  "invalid id pattern",
		},
		{
			name: "case insensitive lifecycle and access",
			rules: []LifecycleRule{
				{Type: "Obj1", Lifecycle: "create", Access: "allow"},
				{Type: "Obj2", Lifecycle: "Delete", Access: "Internal"},
				{Type: "Obj3", Lifecycle: "ALL", Access: "EXTERNAL"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := NewLifecycleValidator(tt.rules)
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

func TestLifecycleValidator_CheckClientCreate(t *testing.T) {
	rules := []LifecycleRule{
		// ChatRoom: clients can create with valid IDs
		{Type: "ChatRoom", ID: "/[a-zA-Z0-9_-]{1,50}/", Lifecycle: "CREATE", Access: "ALLOW"},
		// ChatRoom: reject other IDs (empty, too long, invalid chars)
		{Type: "ChatRoom", Lifecycle: "CREATE", Access: "REJECT"},
		// InternalScheduler: only nodes can create
		{Type: "InternalScheduler", Lifecycle: "CREATE", Access: "INTERNAL"},
		// TempSession: only clients can create (via EXTERNAL)
		{Type: "TempSession", Lifecycle: "CREATE", Access: "EXTERNAL"},
		// Singleton: cannot be created dynamically
		{Type: "ConfigManager", Lifecycle: "ALL", Access: "REJECT"},
	}

	v, err := NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name       string
		objectType string
		objectID   string
		allowed    bool
	}{
		// ChatRoom tests
		{name: "ChatRoom valid ID allowed", objectType: "ChatRoom", objectID: "General", allowed: true},
		{name: "ChatRoom invalid ID denied (empty)", objectType: "ChatRoom", objectID: "", allowed: false},
		// InternalScheduler tests
		{name: "InternalScheduler denied for clients", objectType: "InternalScheduler", objectID: "sched-1", allowed: false},
		// TempSession tests
		{name: "TempSession allowed for clients", objectType: "TempSession", objectID: "temp-123", allowed: true},
		// ConfigManager tests
		{name: "ConfigManager creation rejected", objectType: "ConfigManager", objectID: "ConfigManager0", allowed: false},
		// Unknown type (default: ALLOW for CREATE)
		{name: "Unknown type allowed by default", objectType: "UnknownType", objectID: "any", allowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckClientCreate(tt.objectType, tt.objectID)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected denied, got allowed")
			}
		})
	}
}

func TestLifecycleValidator_CheckNodeCreate(t *testing.T) {
	rules := []LifecycleRule{
		// ChatRoom: clients can create (ALLOW works for both)
		{Type: "ChatRoom", Lifecycle: "CREATE", Access: "ALLOW"},
		// InternalScheduler: only nodes can create
		{Type: "InternalScheduler", Lifecycle: "CREATE", Access: "INTERNAL"},
		// TempSession: only clients can create
		{Type: "TempSession", Lifecycle: "CREATE", Access: "EXTERNAL"},
		// Singleton: cannot be created dynamically
		{Type: "ConfigManager", Lifecycle: "ALL", Access: "REJECT"},
	}

	v, err := NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name       string
		objectType string
		objectID   string
		allowed    bool
	}{
		// ChatRoom tests
		{name: "ChatRoom allowed for nodes", objectType: "ChatRoom", objectID: "General", allowed: true},
		// InternalScheduler tests
		{name: "InternalScheduler allowed for nodes", objectType: "InternalScheduler", objectID: "sched-1", allowed: true},
		// TempSession tests (EXTERNAL - node cannot create)
		{name: "TempSession denied for nodes", objectType: "TempSession", objectID: "temp-123", allowed: false},
		// ConfigManager tests
		{name: "ConfigManager creation rejected", objectType: "ConfigManager", objectID: "ConfigManager0", allowed: false},
		// Unknown type (default: ALLOW for CREATE)
		{name: "Unknown type allowed by default", objectType: "UnknownType", objectID: "any", allowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckNodeCreate(tt.objectType, tt.objectID)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected denied, got allowed")
			}
		})
	}
}

func TestLifecycleValidator_CheckClientDelete(t *testing.T) {
	rules := []LifecycleRule{
		// ChatRoom: only nodes can delete
		{Type: "ChatRoom", Lifecycle: "DELETE", Access: "INTERNAL"},
		// TempSession: clients can delete
		{Type: "TempSession", Lifecycle: "DELETE", Access: "ALLOW"},
		// Singleton: cannot be deleted
		{Type: "ConfigManager", Lifecycle: "ALL", Access: "REJECT"},
	}

	v, err := NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name       string
		objectType string
		objectID   string
		allowed    bool
	}{
		// ChatRoom tests
		{name: "ChatRoom delete denied for clients", objectType: "ChatRoom", objectID: "General", allowed: false},
		// TempSession tests
		{name: "TempSession delete allowed for clients", objectType: "TempSession", objectID: "temp-123", allowed: true},
		// ConfigManager tests
		{name: "ConfigManager deletion rejected", objectType: "ConfigManager", objectID: "ConfigManager0", allowed: false},
		// Unknown type (default: INTERNAL for DELETE - clients denied)
		{name: "Unknown type denied by default", objectType: "UnknownType", objectID: "any", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckClientDelete(tt.objectType, tt.objectID)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected denied, got allowed")
			}
		})
	}
}

func TestLifecycleValidator_CheckNodeDelete(t *testing.T) {
	rules := []LifecycleRule{
		// ChatRoom: only nodes can delete
		{Type: "ChatRoom", Lifecycle: "DELETE", Access: "INTERNAL"},
		// TempSession: clients can delete (ALLOW works for both)
		{Type: "TempSession", Lifecycle: "DELETE", Access: "ALLOW"},
		// ExternalOnlyDelete: only clients can delete
		{Type: "ExternalOnlyDelete", Lifecycle: "DELETE", Access: "EXTERNAL"},
		// Singleton: cannot be deleted
		{Type: "ConfigManager", Lifecycle: "ALL", Access: "REJECT"},
	}

	v, err := NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name       string
		objectType string
		objectID   string
		allowed    bool
	}{
		// ChatRoom tests
		{name: "ChatRoom delete allowed for nodes", objectType: "ChatRoom", objectID: "General", allowed: true},
		// TempSession tests
		{name: "TempSession delete allowed for nodes", objectType: "TempSession", objectID: "temp-123", allowed: true},
		// ExternalOnlyDelete tests (EXTERNAL - nodes cannot delete)
		{name: "ExternalOnlyDelete denied for nodes", objectType: "ExternalOnlyDelete", objectID: "ext-1", allowed: false},
		// ConfigManager tests
		{name: "ConfigManager deletion rejected", objectType: "ConfigManager", objectID: "ConfigManager0", allowed: false},
		// Unknown type (default: INTERNAL for DELETE - nodes allowed)
		{name: "Unknown type allowed by default", objectType: "UnknownType", objectID: "any", allowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.CheckNodeDelete(tt.objectType, tt.objectID)
			if tt.allowed && err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("expected denied, got allowed")
			}
		})
	}
}

func TestLifecycleValidator_AllMatchesBothOperations(t *testing.T) {
	rules := []LifecycleRule{
		// InternalScheduler: only nodes can create AND delete (using ALL)
		{Type: "InternalScheduler", Lifecycle: "ALL", Access: "INTERNAL"},
	}

	v, err := NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// CREATE checks
	if err := v.CheckClientCreate("InternalScheduler", "sched-1"); err == nil {
		t.Error("expected client CREATE to be denied for InternalScheduler")
	}
	if err := v.CheckNodeCreate("InternalScheduler", "sched-1"); err != nil {
		t.Errorf("expected node CREATE to be allowed for InternalScheduler: %v", err)
	}

	// DELETE checks
	if err := v.CheckClientDelete("InternalScheduler", "sched-1"); err == nil {
		t.Error("expected client DELETE to be denied for InternalScheduler")
	}
	if err := v.CheckNodeDelete("InternalScheduler", "sched-1"); err != nil {
		t.Errorf("expected node DELETE to be allowed for InternalScheduler: %v", err)
	}
}

func TestLifecycleValidator_FirstMatchWins(t *testing.T) {
	rules := []LifecycleRule{
		// Specific ID first
		{Type: "ChatRoom", ID: "VIP-room", Lifecycle: "CREATE", Access: "INTERNAL"},
		// General rule second
		{Type: "ChatRoom", Lifecycle: "CREATE", Access: "ALLOW"},
	}

	v, err := NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// VIP-room should be denied for clients (INTERNAL)
	if err := v.CheckClientCreate("ChatRoom", "VIP-room"); err == nil {
		t.Error("expected VIP-room to be denied for clients")
	}

	// Other rooms should be allowed (ALLOW)
	if err := v.CheckClientCreate("ChatRoom", "General"); err != nil {
		t.Errorf("expected General room to be allowed: %v", err)
	}
}

func TestLifecycleValidator_DefaultBehavior(t *testing.T) {
	// Empty rules = use defaults
	v, err := NewLifecycleValidator([]LifecycleRule{})
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// CREATE default: ALLOW (both clients and nodes)
	if err := v.CheckClientCreate("AnyType", "any-id"); err != nil {
		t.Errorf("expected CREATE to be allowed for clients by default: %v", err)
	}
	if err := v.CheckNodeCreate("AnyType", "any-id"); err != nil {
		t.Errorf("expected CREATE to be allowed for nodes by default: %v", err)
	}

	// DELETE default: INTERNAL (only nodes)
	if err := v.CheckClientDelete("AnyType", "any-id"); err == nil {
		t.Error("expected DELETE to be denied for clients by default")
	}
	if err := v.CheckNodeDelete("AnyType", "any-id"); err != nil {
		t.Errorf("expected DELETE to be allowed for nodes by default: %v", err)
	}
}

func TestParseLifecycleOperation(t *testing.T) {
	tests := []struct {
		input    string
		expected LifecycleOperation
		wantErr  bool
	}{
		{"CREATE", LifecycleCreate, false},
		{"DELETE", LifecycleDelete, false},
		{"ALL", LifecycleAll, false},
		// Case insensitive
		{"create", LifecycleCreate, false},
		{"delete", LifecycleDelete, false},
		{"all", LifecycleAll, false},
		{"Create", LifecycleCreate, false},
		// Invalid
		{"INVALID", LifecycleCreate, true},
		{"", LifecycleCreate, true},
		{"UPDATE", LifecycleCreate, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLifecycleOperation(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, but got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if got != tt.expected {
					t.Errorf("ParseLifecycleOperation(%q) = %v, want %v", tt.input, got, tt.expected)
				}
			}
		})
	}
}

func TestLifecycleOperation_String(t *testing.T) {
	tests := []struct {
		op       LifecycleOperation
		expected string
	}{
		{LifecycleCreate, "CREATE"},
		{LifecycleDelete, "DELETE"},
		{LifecycleAll, "ALL"},
		{LifecycleOperation(99), "UNKNOWN"}, // Invalid enum value
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.op.String(); got != tt.expected {
				t.Errorf("LifecycleOperation(%d).String() = %q, want %q", tt.op, got, tt.expected)
			}
		})
	}
}

func TestLoadConfigWithLifecycleRules(t *testing.T) {
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

object_lifecycle_rules:
  - type: ChatRoom
    id: /[a-zA-Z0-9_-]+/
    lifecycle: CREATE
    access: ALLOW

  - type: ChatRoom
    lifecycle: DELETE
    access: INTERNAL

  - type: InternalScheduler
    lifecycle: ALL
    access: INTERNAL
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

	// Verify lifecycle rules were loaded
	if len(cfg.LifecycleRules) != 3 {
		t.Fatalf("expected 3 lifecycle rules, got %d", len(cfg.LifecycleRules))
	}

	// Verify first rule
	if cfg.LifecycleRules[0].Type != "ChatRoom" {
		t.Errorf("expected first rule type ChatRoom, got %s", cfg.LifecycleRules[0].Type)
	}
	if cfg.LifecycleRules[0].Lifecycle != "CREATE" {
		t.Errorf("expected first rule lifecycle CREATE, got %s", cfg.LifecycleRules[0].Lifecycle)
	}
	if cfg.LifecycleRules[0].Access != "ALLOW" {
		t.Errorf("expected first rule access ALLOW, got %s", cfg.LifecycleRules[0].Access)
	}

	// Create lifecycle validator
	v, err := cfg.NewLifecycleValidator()
	if err != nil {
		t.Fatalf("failed to create lifecycle validator: %v", err)
	}
	if v == nil {
		t.Fatalf("expected non-nil lifecycle validator")
	}

	// Verify lifecycle control works
	if err := v.CheckClientCreate("ChatRoom", "General"); err != nil {
		t.Errorf("ChatRoom/General CREATE should be allowed for clients: %v", err)
	}
	if err := v.CheckClientDelete("ChatRoom", "General"); err == nil {
		t.Error("ChatRoom/General DELETE should be denied for clients (INTERNAL)")
	}
	if err := v.CheckNodeDelete("ChatRoom", "General"); err != nil {
		t.Errorf("ChatRoom/General DELETE should be allowed for nodes: %v", err)
	}
	if err := v.CheckClientCreate("InternalScheduler", "sched-1"); err == nil {
		t.Error("InternalScheduler CREATE should be denied for clients")
	}
	if err := v.CheckNodeCreate("InternalScheduler", "sched-1"); err != nil {
		t.Errorf("InternalScheduler CREATE should be allowed for nodes: %v", err)
	}
}

func TestConfigNewLifecycleValidator_EmptyRules(t *testing.T) {
	cfg := &Config{
		LifecycleRules: nil,
	}

	v, err := cfg.NewLifecycleValidator()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Error("expected nil validator for empty rules")
	}
}
