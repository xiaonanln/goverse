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
	Object string `yaml:"object"` // Pattern: literal string or /regexp/
	Method string `yaml:"method"` // Pattern: literal string or /regexp/
	Access string `yaml:"access"` // REJECT, INTERNAL, EXTERNAL, or ALLOW
}

// PatternMatcher matches strings either exactly or via regexp
type PatternMatcher interface {
	Match(s string) bool
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
// Regexp patterns are auto-anchored to match the full string.
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

// CompiledRule is a pre-compiled access control rule for runtime use
type CompiledRule struct {
	objectMatcher PatternMatcher
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

		// Parse object pattern
		compiled.objectMatcher, err = parsePattern(rule.Object)
		if err != nil {
			return nil, fmt.Errorf("invalid object pattern in rule %d: %w", i, err)
		}

		// Parse method pattern
		compiled.methodMatcher, err = parsePattern(rule.Method)
		if err != nil {
			return nil, fmt.Errorf("invalid method pattern in rule %d: %w", i, err)
		}

		// Validate access level
		switch rule.Access {
		case AccessReject, AccessInternal, AccessExternal, AccessAllow:
			// OK
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
func (v *AccessValidator) CheckClientAccess(objectID, method string) error {
	access := v.findAccess(objectID, method)

	if access != AccessAllow && access != AccessExternal {
		return fmt.Errorf("access denied for object %q method %q", objectID, method)
	}
	return nil
}

// CheckNodeAccess checks if a node (object-to-object) can access the object/method.
// Returns nil if allowed, error if denied.
// Node access is allowed for ALLOW and INTERNAL access levels.
func (v *AccessValidator) CheckNodeAccess(objectID, method string) error {
	access := v.findAccess(objectID, method)

	if access == AccessReject || access == AccessExternal {
		return fmt.Errorf("access denied for object %q method %q", objectID, method)
	}
	return nil
}

// findAccess evaluates rules top-to-bottom and returns the access level.
// Returns REJECT if no rule matches (default deny).
func (v *AccessValidator) findAccess(objectID, method string) string {
	for _, rule := range v.rules {
		if rule.objectMatcher.Match(objectID) &&
			rule.methodMatcher.Match(method) {
			return rule.access
		}
	}
	// Default: deny
	return AccessReject
}
