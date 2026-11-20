package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// Action represents the outcome of the guard analysis.
type Action string

const (
	ActionBlock  Action = "BLOCK"
	ActionReview Action = "REVIEW"
	ActionInfo   Action = "INFO"
)

// Finding represents a single rule match.
type Finding struct {
	Severity string `json:"severity"` // "BLOCK" or "REVIEW"
	Rule     string `json:"rule"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
}

// Result is the structured evaluation result output by PR Guard.
type Result struct {
	Action       Action    `json:"action"`
	Reasons      []string  `json:"reasons,omitempty"`
	Findings     []Finding `json:"findings,omitempty"`
	Summary      string    `json:"summary"`
	Exempt       bool      `json:"exempt"`
	ExemptReason string    `json:"exempt_reason,omitempty"`
	Approved     bool      `json:"approved,omitempty"`
}

// PRFile matches GitHub's pull request file representation.
type PRFile struct {
	SHA         string `json:"sha"`
	Filename    string `json:"filename"`
	Status      string `json:"status"` // "added", "modified", "removed", "renamed"
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	Changes     int    `json:"changes"`
	BlobURL     string `json:"blob_url,omitempty"`
	RawURL      string `json:"raw_url,omitempty"`
	ContentsURL string `json:"contents_url,omitempty"`
	Patch       string `json:"patch,omitempty"`
}

// PRMetadata contains essential PR-level metadata.
type PRMetadata struct {
	Number       int      `json:"number"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Author       string   `json:"author"`
	Sender       string   `json:"sender"`
	EventAction  string   `json:"event_action"`
	EventLabel   string   `json:"event_label"`
	Labels       []string `json:"labels"`
	ChangedFiles int      `json:"changed_files"`
	Additions    int      `json:"additions"`
	Deletions    int      `json:"deletions"`
}

var (
	issueRefRegex = regexp.MustCompile(`(?i)\b(?:fixes|fixed|fix|closes|closed|close|resolves|resolved|resolve)\s+(?:#\d+|https://github\.com/[^/\s]+/[^/\s]+/issues/\d+)`)
	urlRegex      = regexp.MustCompile(`https?://([a-zA-Z0-9.-]+)(?::[0-9]+)?(?:/[^\s'"<>\\]*)?`)
	replaceRegex  = regexp.MustCompile(`(?m)^\+.*replace\s+.*=>|(?m)^\+\s*replace\b`)
)

// Evaluate applies policy rules to the PR files and metadata.
func Evaluate(policy *Policy, pr PRMetadata, files []PRFile) Result {
	var findings []Finding

	// Check author exemptions
	if isMaintainer(policy, pr.Author) {
		return Result{
			Action:       ActionInfo,
			Summary:      fmt.Sprintf("PR author '%s' is a repository maintainer; guard checks exempt.", pr.Author),
			Exempt:       true,
			ExemptReason: fmt.Sprintf("Author '%s' is in maintainers exempt list", pr.Author),
		}
	}

	if isDependabot(policy, pr.Author) {
		if allDependencyFiles(files) {
			return Result{
				Action:       ActionInfo,
				Summary:      fmt.Sprintf("Dependabot dependency update PR from '%s'; guard checks exempt.", pr.Author),
				Exempt:       true,
				ExemptReason: "Dependabot updating dependency files only",
			}
		}
	}

	// 1. BLOCK Rules (auto-close, non-maintainers only)
	for _, file := range files {
		// Protected paths
		if matchesProtectedPath(policy.ProtectedPaths, file.Filename) {
			findings = append(findings, Finding{
				Severity: string(ActionBlock),
				Rule:     "protected-path",
				File:     file.Filename,
				Message:  fmt.Sprintf("Protected path '%s' cannot be modified by non-maintainers.", file.Filename),
			})
		}

		// Added binary or executable files
		if file.Status == "added" {
			if isBinaryOrExecutable(policy, file) {
				findings = append(findings, Finding{
					Severity: string(ActionBlock),
					Rule:     "added-binary",
					File:     file.Filename,
					Message:  fmt.Sprintf("Adding binary or executable file '%s' is prohibited.", file.Filename),
				})
			}
		}
	}

	// 2. REVIEW Rules
	// Missing issue reference in PR title or body (demoted to INFO)
	combinedText := pr.Title + "\n" + pr.Body
	if !issueRefRegex.MatchString(combinedText) {
		findings = append(findings, Finding{
			Severity: string(ActionInfo),
			Rule:     "missing-issue-reference",
			Message:  "PR title or body does not reference an issue using 'Fixes #N' or 'Closes #N'.",
		})
	}

	// PR size limits
	totalChangedLines := pr.Additions + pr.Deletions
	if totalChangedLines == 0 {
		for _, f := range files {
			totalChangedLines += (f.Additions + f.Deletions)
		}
	}

	fileCount := len(files)
	if pr.ChangedFiles > fileCount {
		fileCount = pr.ChangedFiles
	}

	if policy.MaxFiles > 0 && fileCount > policy.MaxFiles {
		findings = append(findings, Finding{
			Severity: string(ActionReview),
			Rule:     "max-files-exceeded",
			Message:  fmt.Sprintf("PR touches %d files, exceeding policy limit of %d.", fileCount, policy.MaxFiles),
		})
	}

	if policy.MaxChangedLines > 0 && totalChangedLines > policy.MaxChangedLines {
		findings = append(findings, Finding{
			Severity: string(ActionReview),
			Rule:     "max-lines-exceeded",
			Message:  fmt.Sprintf("PR changes %d lines, exceeding policy limit of %d.", totalChangedLines, policy.MaxChangedLines),
		})
	}

	// Handle 3000-file API limit
	if len(files) >= 3000 || pr.ChangedFiles > 3000 {
		findings = append(findings, Finding{
			Severity: string(ActionReview),
			Rule:     "file-limit-3000",
			Message:  fmt.Sprintf("PR changes %d files, reaching or exceeding the 3000-file GitHub API diff limit. Manual diff inspection required.", fileCount),
		})
	}

	// Per-file checks (patches and dependencies)
	for _, file := range files {
		// Treat missing patch (binary or large) as REVIEW (unless already blocked as added binary)
		if file.Status != "removed" && file.Patch == "" {
			if file.Status != "added" || !isBinaryOrExecutable(policy, file) {
				findings = append(findings, Finding{
					Severity: string(ActionReview),
					Rule:     "missing-patch",
					File:     file.Filename,
					Message:  fmt.Sprintf("File '%s' has missing patch diff (binary or large file exceeding GitHub API limits); requires manual review.", file.Filename),
				})
			}
		}

		// Flag go.mod and go.sum changes
		if file.Filename == "go.mod" {
			if file.Patch != "" && replaceRegex.MatchString(file.Patch) {
				findings = append(findings, Finding{
					Severity: string(ActionReview),
					Rule:     "go-mod-replace",
					File:     file.Filename,
					Message:  "go.mod contains 'replace' directive; requires supply-chain security review.",
				})
			} else {
				findings = append(findings, Finding{
					Severity: string(ActionReview),
					Rule:     "go-mod-modified",
					File:     file.Filename,
					Message:  "go.mod was modified; requires dependency security review.",
				})
			}
		} else if file.Filename == "go.sum" {
			findings = append(findings, Finding{
				Severity: string(ActionReview),
				Rule:     "go-sum-modified",
				File:     file.Filename,
				Message:  "go.sum was modified; requires dependency checksum verification.",
			})
		}

		// Suspicious patterns in added lines
		if file.Patch != "" {
			addedLines := extractAddedLines(file.Patch)
			for _, cp := range policy.compiledPatterns {
				if cp.re.MatchString(addedLines) {
					findings = append(findings, Finding{
						Severity: cp.pattern.Severity,
						Rule:     cp.pattern.Name,
						File:     file.Filename,
						Message:  fmt.Sprintf("Suspicious pattern '%s' detected in patch: %s", cp.pattern.Name, cp.pattern.Notes),
					})
				}
			}

			// URLs outside allowlist (applied only to non-documentation files)
			if !isDocumentationFile(file.Filename) {
				foundURLs := urlRegex.FindAllString(addedLines, -1)
				for _, rawURL := range foundURLs {
					if !isAllowedURL(policy.AllowedHosts, rawURL) {
						findings = append(findings, Finding{
							Severity: string(ActionReview),
							Rule:     "unallowed-host-url",
							File:     file.Filename,
							Message:  fmt.Sprintf("URL '%s' points to host outside allowed hosts list.", rawURL),
						})
					}
				}
			}
		}
	}

	// 3. Determine Result Action
	var blockReasons []string
	var reviewReasons []string

	for _, f := range findings {
		if f.Severity == string(ActionBlock) {
			blockReasons = append(blockReasons, f.Message)
		} else if f.Severity == string(ActionReview) {
			reviewReasons = append(reviewReasons, f.Message)
		}
	}

	if len(blockReasons) > 0 {
		return Result{
			Action:   ActionBlock,
			Reasons:  blockReasons,
			Findings: findings,
			Summary:  fmt.Sprintf("PR triggers %d blocking security violation(s).", len(blockReasons)),
		}
	}

	if len(reviewReasons) > 0 {
		hasSecurityReviewed := false
		for _, l := range pr.Labels {
			if l == "security-reviewed" {
				hasSecurityReviewed = true
				break
			}
		}

		approved := false
		if hasSecurityReviewed {
			// A synchronize event or removal of security-reviewed label invalidates previous reviews
			if pr.EventAction == "synchronize" || (pr.EventAction == "unlabeled" && pr.EventLabel == "security-reviewed") {
				approved = false
			} else if pr.EventAction == "labeled" && pr.EventLabel == "security-reviewed" {
				// ONLY maintainers' label events may clear a REVIEW
				approved = isMaintainer(policy, pr.Sender)
			} else if pr.EventAction == "opened" {
				approved = isMaintainer(policy, pr.Sender)
			} else {
				// Maintain existing approval on other non-synchronize events (e.g. edited)
				approved = true
			}
		}

		summary := fmt.Sprintf("PR triggers %d item(s) requiring manual security review (pending 'security-reviewed' label).", len(reviewReasons))
		if approved {
			summary = fmt.Sprintf("PR triggers %d item(s) requiring review; approved by maintainer via 'security-reviewed' label.", len(reviewReasons))
		}

		return Result{
			Action:   ActionReview,
			Reasons:  reviewReasons,
			Findings: findings,
			Summary:  summary,
			Approved: approved,
		}
	}

	// INFO: Check if tests and docs only
	if areTestsAndDocsOnly(files) {
		return Result{
			Action:   ActionInfo,
			Findings: findings,
			Summary:  "Documentation and test changes only; PR Guard passed.",
		}
	}

	return Result{
		Action:   ActionInfo,
		Findings: findings,
		Summary:  "All PR Guard checks passed.",
	}
}

func isMaintainer(policy *Policy, author string) bool {
	if author == "" {
		return false
	}
	for _, m := range policy.ExemptAuthors.Maintainers {
		if strings.EqualFold(strings.TrimPrefix(m, "@"), strings.TrimPrefix(author, "@")) {
			return true
		}
	}
	return false
}

func isDependabot(policy *Policy, author string) bool {
	if author == "" {
		return false
	}
	for _, d := range policy.ExemptAuthors.Dependabot {
		if strings.EqualFold(d, author) {
			return true
		}
	}
	return false
}

func allDependencyFiles(files []PRFile) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		switch f.Filename {
		case "go.mod", "go.sum", ".github/dependabot.yml":
			// valid dependency files
		default:
			return false
		}
	}
	return true
}

func matchesProtectedPath(protected []string, filename string) bool {
	cleanName := filepath.Clean(filename)
	for _, pattern := range protected {
		pattern = filepath.Clean(pattern)
		// Pattern ending with /** or /*
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if strings.HasPrefix(cleanName, prefix+"/") || cleanName == prefix {
				return true
			}
		} else if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if filepath.Dir(cleanName) == prefix {
				return true
			}
		} else if cleanName == pattern || filepath.Base(cleanName) == pattern {
			return true
		}
	}
	return false
}

func isBinaryOrExecutable(policy *Policy, file PRFile) bool {
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != "" {
		for _, bExt := range policy.BinaryExtensions {
			if strings.EqualFold(ext, bExt) {
				return true
			}
		}
	}
	// Missing patch diff on added file with changes
	if file.Patch == "" && file.Status == "added" && file.Additions > 0 {
		return true
	}
	return false
}

func extractAddedLines(patch string) string {
	var sb strings.Builder
	lines := strings.Split(patch, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func isAllowedURL(allowedHosts []string, rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return true
	}

	// Loopback / local hosts
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return true
	}

	for _, allowed := range allowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if hostname == allowed || strings.HasSuffix(hostname, "."+allowed) {
			return true
		}
	}
	return false
}

func isDocumentationFile(filename string) bool {
	clean := filepath.Clean(filename)
	return strings.HasSuffix(clean, ".md") ||
		strings.HasPrefix(clean, "docs/") ||
		clean == "LICENSE" ||
		clean == "CODE_OF_CONDUCT.md"
}

func areTestsAndDocsOnly(files []PRFile) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		clean := filepath.Clean(f.Filename)
		if isDocumentationFile(clean) ||
			strings.HasSuffix(clean, "_test.go") ||
			strings.HasPrefix(clean, "test/") ||
			strings.HasPrefix(clean, "testdata/") {
			continue
		}
		return false
	}
	return true
}

// FormatComment formats a kind, professional GitHub PR comment explaining findings.
func FormatComment(res Result, pr PRMetadata, contributingURL string) string {
	const marker = "<!-- fairway-pr-guard -->"
	var sb strings.Builder

	sb.WriteString(marker)
	sb.WriteString("\n\n")

	author := pr.Author
	if author == "" {
		author = "contributor"
	}

	switch res.Action {
	case ActionBlock:
		sb.WriteString("### 🛡️ PR Guard Notice: Pull Request Automatically Closed\n\n")
		sb.WriteString(fmt.Sprintf("Hi @%s, thank you for your interest in contributing to Fairway!\n\n", author))
		sb.WriteString("Our automated PR Guard security check detected modifications to restricted files or disallowed assets:\n\n")
		for _, reason := range res.Reasons {
			sb.WriteString(fmt.Sprintf("- ⚠️ **Blocked:** %s\n", reason))
		}
		sb.WriteString("\nTo protect repository infrastructure and supply-chain security, changes to CI workflows, Docker configuration, security policies, and binary assets may only be submitted by repository maintainers. Consequently, this pull request has been automatically closed.\n\n")
		if contributingURL != "" {
			sb.WriteString(fmt.Sprintf("If this change was intended, please review our contribution guidelines:  \n👉 [%s](%s)\n\n", "Protected paths and automated checks", contributingURL))
		}
		sb.WriteString("You are welcome to open an issue to discuss this change with maintainers before resubmitting. Thank you for your understanding!\n")

	case ActionReview:
		if res.Approved {
			sb.WriteString("### 🛡️ PR Guard Notice: Security Review Approved\n\n")
			sb.WriteString(fmt.Sprintf("Hi @%s, the security review has been approved by a repository maintainer via the `security-reviewed` label.\n\n", author))
			sb.WriteString("All required automated checks are now passing. Thank you for your contribution!\n")
		} else {
			sb.WriteString("### 🛡️ PR Guard Notice: Security Review Required\n\n")
			sb.WriteString(fmt.Sprintf("Hi @%s, thank you for contributing to Fairway!\n\n", author))
			sb.WriteString("Our automated security guard reviewed this pull request and flagged the following item(s) that require maintainer review before merging:\n\n")
			for _, f := range res.Findings {
				if f.Severity == string(ActionReview) {
					if f.File != "" {
						sb.WriteString(fmt.Sprintf("- 🔍 **[%s]** `%s`: %s\n", f.Rule, f.File, f.Message))
					} else {
						sb.WriteString(fmt.Sprintf("- 🔍 **[%s]**: %s\n", f.Rule, f.Message))
					}
				}
			}
			sb.WriteString("\n**Next steps:**\n")
			sb.WriteString("1. A repository maintainer will review the flagged items and can approve by applying the `security-reviewed` label.\n")
			sb.WriteString("2. If any items can be addressed (such as adjusting URLs or dependencies), you can push updates directly to this branch and the check will automatically re-run (any pushed update will require fresh maintainer re-approval).\n")
			if contributingURL != "" {
				sb.WriteString(fmt.Sprintf("3. For more details on our security checks, see the [Protected paths and automated checks](%s) guide.\n\n", contributingURL))
			}
			sb.WriteString("Thank you for helping keep Fairway safe and reliable!\n")
		}

	case ActionInfo:
		sb.WriteString("### 🛡️ PR Guard: Checks Passed\n\n")
		sb.WriteString(fmt.Sprintf("All automated PR Guard security checks passed successfully for @%s.\n", author))
	}

	return sb.String()
}
