package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Policy defines PR Guard security policies.
type Policy struct {
	ProtectedPaths     []string            `json:"protected_paths"`
	MaxFiles           int                 `json:"max_files"`
	MaxChangedLines    int                 `json:"max_changed_lines"`
	BinaryExtensions   []string            `json:"binary_extensions"`
	AllowedHosts       []string            `json:"allowed_hosts"`
	ExemptAuthors      ExemptAuthors       `json:"exempt_authors"`
	SuspiciousPatterns []SuspiciousPattern `json:"suspicious_patterns"`

	compiledPatterns []compiledPattern
}

// ExemptAuthors lists author logins exempt from checks.
type ExemptAuthors struct {
	Maintainers []string `json:"maintainers"`
	Dependabot  []string `json:"dependabot"`
}

// SuspiciousPattern specifies a regular expression check for diffs.
type SuspiciousPattern struct {
	Name     string `json:"name"`
	Regex    string `json:"regex"`
	Severity string `json:"severity"`
	Notes    string `json:"notes"`
}

type compiledPattern struct {
	pattern SuspiciousPattern
	re      *regexp.Regexp
}

// LoadPolicy reads and parses a policy file in YAML or JSON format using Go standard library only.
func LoadPolicy(path string) (*Policy, error) {
	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath) // #nosec G304 - policy path provided via CLI or default configuration
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	policy := &Policy{}

	// If file starts with '{', attempt standard JSON unmarshal.
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal(data, policy); err != nil {
			return nil, fmt.Errorf("parse policy JSON: %w", err)
		}
	} else {
		// Parse YAML using standard library parser.
		if err := parseYAML(trimmed, policy); err != nil {
			return nil, fmt.Errorf("parse policy YAML: %w", err)
		}
	}

	if err := policy.compile(); err != nil {
		return nil, fmt.Errorf("compile policy patterns: %w", err)
	}

	return policy, nil
}

func (p *Policy) compile() error {
	p.compiledPatterns = make([]compiledPattern, 0, len(p.SuspiciousPatterns))
	for _, pat := range p.SuspiciousPatterns {
		if pat.Regex == "" {
			continue
		}
		re, err := regexp.Compile(pat.Regex)
		if err != nil {
			return fmt.Errorf("invalid regex for pattern '%s' (%q): %w", pat.Name, pat.Regex, err)
		}
		p.compiledPatterns = append(p.compiledPatterns, compiledPattern{
			pattern: pat,
			re:      re,
		})
	}
	return nil
}

// parseYAML parses the YAML structure supported by .github/prguard.yml using standard library scanner.
func parseYAML(content string, p *Policy) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var currentSection string
	var subSection string
	var currentPattern *SuspiciousPattern

	for scanner.Scan() {
		rawLine := scanner.Text()
		trimmedLine := strings.TrimSpace(rawLine)

		// Skip comments and empty lines
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
			continue
		}

		// Top-level keys (no leading whitespace)
		if !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(rawLine, "\t") && strings.Contains(trimmedLine, ":") {
			if currentPattern != nil {
				p.SuspiciousPatterns = append(p.SuspiciousPatterns, *currentPattern)
				currentPattern = nil
			}
			parts := strings.SplitN(trimmedLine, ":", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			currentSection = key
			subSection = ""

			switch key {
			case "max_files":
				v, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid max_files value %q: %w", val, err)
				}
				p.MaxFiles = v
			case "max_changed_lines":
				v, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid max_changed_lines value %q: %w", val, err)
				}
				p.MaxChangedLines = v
			}
			continue
		}

		// Indented lines
		indent := len(rawLine) - len(strings.TrimLeft(rawLine, " \t"))

		// Subsection header (e.g. maintainers:, dependabot:)
		if indent >= 2 && strings.HasSuffix(trimmedLine, ":") && !strings.HasPrefix(trimmedLine, "-") {
			subSection = strings.TrimSuffix(trimmedLine, ":")
			continue
		}

		// List items
		if strings.HasPrefix(trimmedLine, "-") {
			itemVal := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "-"))

			switch currentSection {
			case "protected_paths":
				p.ProtectedPaths = append(p.ProtectedPaths, cleanYAMLString(itemVal))
			case "binary_extensions":
				p.BinaryExtensions = append(p.BinaryExtensions, cleanYAMLString(itemVal))
			case "allowed_hosts":
				p.AllowedHosts = append(p.AllowedHosts, cleanYAMLString(itemVal))
			case "exempt_authors":
				cleanVal := cleanYAMLString(itemVal)
				switch subSection {
				case "maintainers":
					p.ExemptAuthors.Maintainers = append(p.ExemptAuthors.Maintainers, cleanVal)
				case "dependabot":
					p.ExemptAuthors.Dependabot = append(p.ExemptAuthors.Dependabot, cleanVal)
				}
			case "suspicious_patterns":
				if currentPattern != nil {
					p.SuspiciousPatterns = append(p.SuspiciousPatterns, *currentPattern)
				}
				currentPattern = &SuspiciousPattern{}
				if strings.Contains(itemVal, ":") {
					parsePatternField(itemVal, currentPattern)
				}
			}
			continue
		}

		// Key-value pairs inside a list item or subsection
		if strings.Contains(trimmedLine, ":") {
			parts := strings.SplitN(trimmedLine, ":", 2)
			fieldKey := strings.TrimSpace(parts[0])
			fieldVal := strings.TrimSpace(parts[1])

			if currentSection == "suspicious_patterns" && currentPattern != nil {
				switch fieldKey {
				case "name":
					currentPattern.Name = cleanYAMLString(fieldVal)
				case "regex":
					currentPattern.Regex = cleanYAMLString(fieldVal)
				case "severity":
					currentPattern.Severity = cleanYAMLString(fieldVal)
				case "notes":
					currentPattern.Notes = cleanYAMLString(fieldVal)
				}
			}
		}
	}

	if currentPattern != nil {
		p.SuspiciousPatterns = append(p.SuspiciousPatterns, *currentPattern)
	}

	return scanner.Err()
}

func parsePatternField(line string, pat *SuspiciousPattern) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	val := cleanYAMLString(parts[1])
	switch key {
	case "name":
		pat.Name = val
	case "regex":
		pat.Regex = val
	case "severity":
		pat.Severity = val
	case "notes":
		pat.Notes = val
	}
}

func cleanYAMLString(s string) string {
	s = strings.TrimSpace(s)
	if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
		(strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`)) {
		if len(s) >= 2 {
			unquoted := s[1 : len(s)-1]
			// Unescape common sequences if double-quoted
			if strings.HasPrefix(s, `"`) {
				unquoted = strings.ReplaceAll(unquoted, `\"`, `"`)
				unquoted = strings.ReplaceAll(unquoted, `\\`, `\`)
			}
			return unquoted
		}
	}
	return s
}
