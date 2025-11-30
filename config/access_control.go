package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Access level constants
const (
	AccessReject   = "REJECT"
	AccessInternal = "INTERNAL"
	AccessExternal = "EXTERNAL"
	AccessAllow    = "ALLOW"
)

// AccessRule defines a single access control rule
type AccessRule struct {
	Type   string `yaml:"type"`   // Required: object type pattern (literal or /regexp/)
	ID     string `yaml:"id"`     // Optional: object ID pattern (omit to match all)
	Method string `yaml:"method"` // Optional: method pattern (omit to match all)
	Access string `yaml:"access"` // Required: REJECT, INTERNAL, EXTERNAL, or ALLOW
}

// PatternMatcher matches strings either exactly or via regexp
type PatternMatcher interface {
	Match(s string) bool
}

// matchAllMatcher always returns true (for omitted optional fields)
type matchAllMatcher struct{}

func (m matchAllMatcher) Match(s string) bool {
	return true
}

// literalMatcher performs exact string matching
type literalMatcher string

func (m literalMatcher) Match(s string) bool {
	return string(m) == s
}

// regexpMatcher performs regex matching
type regexpMatcher struct {
	re *regexp.Regexp
}

func (m *regexpMatcher) Match(s string) bool {
	return m.re.MatchString(s)
}

// parsePattern returns a matcher for literal strings or /regexp/ patterns.
// Regexp patterns are auto-anchored to match the full string (^ and $ are added).
// Note: The pattern inside /.../ should NOT already contain ^ or $ anchors,
// as they will be added automatically. For example:
//   - "/ChatRoom-.*/" will match "ChatRoom-123" but not "PrefixChatRoom-123"
//   - "ChatRoomMgr" (literal) will match exactly "ChatRoomMgr"
func parsePattern(pattern string) (PatternMatcher, error) {
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 1 {
		// Regexp pattern: /.../ - auto-anchor to match full string
		regexStr := pattern[1 : len(pattern)-1]
		regexStr = "^(?:" + regexStr + ")$" // Auto-anchor for full match
		re, err := regexp.Compile(regexStr)
		if err != nil {
			return nil, err
		}
		return &regexpMatcher{re: re}, nil
	}
	// Literal string: exact match
	return literalMatcher(pattern), nil
}

// parsePatternOrMatchAll returns a matcher. Empty string matches all.
func parsePatternOrMatchAll(pattern string) (PatternMatcher, error) {
	if pattern == "" {
		return matchAllMatcher{}, nil
	}
	return parsePattern(pattern)
}

// CompiledRule is a pre-compiled access control rule for runtime use
type CompiledRule struct {
	typeMatcher   PatternMatcher
	idMatcher     PatternMatcher
	methodMatcher PatternMatcher
	access        string
}

// AccessValidator validates access to objects and methods
type AccessValidator struct {
	rules []*CompiledRule
}

// NewAccessValidator creates a new AccessValidator from a list of AccessRules.
// Returns an error if any rule has an invalid pattern or access level.
func NewAccessValidator(rules []AccessRule) (*AccessValidator, error) {
	v := &AccessValidator{
		rules: make([]*CompiledRule, 0, len(rules)),
	}

	for i, rule := range rules {
		compiled := &CompiledRule{access: rule.Access}
		var err error

		// Type is required
		if rule.Type == "" {
			return nil, fmt.Errorf("missing type in rule %d", i)
		}
		compiled.typeMatcher, err = parsePattern(rule.Type)
		if err != nil {
			return nil, fmt.Errorf("invalid type pattern in rule %d: %w", i, err)
		}

		// ID is optional (empty = match all)
		compiled.idMatcher, err = parsePatternOrMatchAll(rule.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid id pattern in rule %d: %w", i, err)
		}

		// Method is optional (empty = match all)
		compiled.methodMatcher, err = parsePatternOrMatchAll(rule.Method)
		if err != nil {
			return nil, fmt.Errorf("invalid method pattern in rule %d: %w", i, err)
		}

		// Validate access level
		switch rule.Access {
		case AccessReject, AccessInternal, AccessExternal, AccessAllow:
			// OK
		case "":
			return nil, fmt.Errorf("missing access in rule %d", i)
		default:
			return nil, fmt.Errorf("invalid access level in rule %d: %q", i, rule.Access)
		}

		v.rules = append(v.rules, compiled)
	}

	return v, nil
}

// CheckClientAccess checks if a client (via Gate) can access the object/method.
// Returns nil if allowed, error if denied.
// Client access is allowed for ALLOW and EXTERNAL access levels.
func (v *AccessValidator) CheckClientAccess(objectType, objectID, method string) error {
	access := v.findAccess(objectType, objectID, method)

	if access == AccessAllow || access == AccessExternal {
		return nil
	}
	return fmt.Errorf("access denied for %s/%s method %q", objectType, objectID, method)
}

// CheckNodeAccess checks if a node (object-to-object) can access the object/method.
// Returns nil if allowed, error if denied.
// Node access is allowed for ALLOW and INTERNAL access levels.
func (v *AccessValidator) CheckNodeAccess(objectType, objectID, method string) error {
	access := v.findAccess(objectType, objectID, method)

	if access == AccessAllow || access == AccessInternal {
		return nil
	}
	return fmt.Errorf("access denied for %s/%s method %q", objectType, objectID, method)
}

// findAccess evaluates rules top-to-bottom and returns the access level.
// Returns REJECT if no rule matches (default deny).
func (v *AccessValidator) findAccess(objectType, objectID, method string) string {
	for _, rule := range v.rules {
		if rule.typeMatcher.Match(objectType) &&
			rule.idMatcher.Match(objectID) &&
			rule.methodMatcher.Match(method) {
			return rule.access
		}
	}
	// Default: deny
	return AccessReject
}
