# Fairway Security Model: PR Guard and Safe Review Architecture

## 1. Threat Model & Trust Boundaries

Fairway monitors Stellar payment corridors, aggregating and calculating critical financial health indicators. Maintaining repository security, build pipeline integrity, and supply-chain trust is essential.

### 1.1 Untrusted Assets and Actors

In the context of community contributions:
* **Contributor Identities:** Anyone with a GitHub account can fork the repository and open a pull request. Contributor accounts may be newly created, unverified, compromised, or pseudonymous.
* **Pull Request Code and Commits:** Code submitted in pull requests originates from external forks and must be treated as untrusted, potentially malicious, or vulnerable until inspected and approved.
* **PR Metadata (Title, Description, Branch Names):** Text entered into pull request forms is user-controlled. It can contain shell injection payloads, formatting exploits, or misleading issue references.
* **Submitted Dependencies:** Any changes to module declarations (`go.mod`, `go.sum`), vendored assets, or third-party package references are untrusted until verified against official checksum databases.

### 1.2 Trusted Assets

* **Base Repository (`main` branch):** The code, workflows, and policies committed to the base branch of `gofairway/fairway` by authenticated maintainers.
* **Repository Maintainers:** Individuals explicitly listed in `CODEOWNERS` and the PR Guard maintainer exemption policy.
* **Repository Secrets & Elevated Tokens:** Secrets stored in repository settings (e.g., deployment tokens, API credentials) and the elevated `GITHUB_TOKEN` provided to base branch workflows.

---

## 2. PR Guard Defensive Architecture

PR Guard is an automated first line of defense running on GitHub Actions. It is triggered by the `pull_request_target` event (`opened`, `synchronize`, `reopened`, `edited`, `labeled`) to allow triage and labeling while isolating untrusted PR code from the build environment.

```
+-----------------------------------------------------------------------------------------+
|                               GitHub Actions Runner                                     |
|                                                                                         |
|  [ pull_request_target event: opened, synchronize, reopened, edited, labeled ]          |
|               |                                                                         |
|               v                                                                         |
|  1. Checkout BASE Branch (main) ONLY ---> Trusted Workflow & Tools                      |
|  2. Compile tools/prguard (Go stdlib only)                                              |
|               |                                                                         |
|               v                                                                         |
|  3. Fetch PR Metadata & File List via GitHub API                                        |
|     (Filename, status, additions, deletions, patch diff)                                |
|     * NEVER checks out PR Head commit                                                   |
|     * NEVER executes PR code                                                            |
|               |                                                                         |
|               v                                                                         |
|  4. Evaluate .github/prguard.yml Policy                                                 |
|     +-------------------------+-------------------------+                               |
|     |                         |                         |                               |
|     v                         v                         v                               |
|  [ BLOCK ]                [ REVIEW ]                 [ INFO ]                           |
|  - Protected path         - Suspicious patterns      - Docs & tests only                |
|  - Binary added           - Supply chain replace     - Missing issue ref                |
|  - Auto-close PR          - External URLs (code)     - Pass cleanly                     |
|  - Post friendly notice   - Apply label              - Exit code 0                      |
|                           - Single updating comment                                     |
|                           - Fails UNTIL maintainer applies 'security-reviewed'          |
|                           - Passes when 'security-reviewed' present (from maintainer)   |
|                           - 'security-reviewed' removed on every synchronize event      |
+-----------------------------------------------------------------------------------------+
```

### 2.1 Key Design Principles

1. **Zero Execution of Untrusted Code:**
   The workflow strictly checks out the target base branch (`main`). It never runs `git checkout` on the contributor's branch, never builds untrusted PR Go packages, and never executes contributor scripts.
2. **Metadata-Only Analysis:**
   The guard inspects the diff returned by the GitHub REST API (`/repos/{owner}/{repo}/pulls/{number}/files`). Code is treated strictly as static text data.
3. **Script Injection Immunity:**
   No pull request fields (such as `github.event.pull_request.title`, `body`, or `head.ref`) are ever interpolated into shell commands. All metadata is ingested from `$GITHUB_EVENT_PATH` via Go standard library JSON decoders.
4. **API Pagination & File Limits:**
   GitHub API limits diff output to 3000 files per PR. PR Guard handles pagination up to 3000 files; any PR reaching or exceeding 3000 changed files is flagged for mandatory security review because complete metadata cannot be guaranteed via the API.
5. **Review Gate & Invalidation:**
   When review items are flagged, the check fails until a maintainer reviews the code and applies the `security-reviewed` label. To ensure ongoing security, any new commit pushed by the contributor (`synchronize` event) automatically strips the `security-reviewed` label and re-evaluates the diff. Only maintainer label events may clear a review.

---

## 3. Policy Enforcement Tiers

### 3.1 BLOCK (Automatic PR Closure)

Applies only to non-maintainer contributors. PRs triggering a BLOCK rule are commented on with the exact rule violation and a link to `CONTRIBUTING.md`, then immediately closed:

* **Protected Paths:** Changes under `.github/**`, `Dockerfile`, `.dockerignore`, `.gitleaksignore`, `LICENSE`, `SECURITY.md`, and `CODEOWNERS`.
* **Binary or Executable Files:** Any file added with an executable or archive extension (`.exe`, `.so`, `.bin`, `.dylib`, `.dll`, `.a`, `.o`, `.class`, `.wasm`, `.elf`, `.tar`, `.gz`, `.zip`, `.png`, etc.) or lacking text diff representation.

### 3.2 REVIEW (Failing Status Check Until Maintainer Approval)

Applies the `needs-security-review` label, posts or updates a single markdown comment (preventing spam on subsequent pushes), and fails the status check until approved:

* **Approval Mechanism:** A repository maintainer clears the review by applying the `security-reviewed` label. Only maintainers' label events can clear a review.
* **Automatic Invalidation:** Pushing new commits (`synchronize` event) immediately strips `security-reviewed`, requiring maintainers to re-verify any new changes.
* **Flagged Conditions:**
  * **Dependency Redirection:** Changes to `go.mod` or `go.sum`, specifically flagging `replace` directives.
  * **Low-Level & Execution Primitives:** Use of `os/exec`, `syscall`, `golang.org/x/sys`, `unsafe`, or `plugin`.
  * **Build Directives:** `//go:generate` annotations.
  * **Remote Shell Execution:** Downloads or piped execution via `curl`, `wget`, `sh`, or `bash -c`.
  * **Obfuscated Payloads:** Long contiguous base64 or hexadecimal strings (>= 80 characters).
  * **Credential & System Access:** References to `~/.ssh`, `.aws`, `.npmrc`, `/etc/passwd`, or `os.Environ()` dumps.
  * **Unapproved Hosts (Non-Documentation Files Only):** URLs in added source code diff lines whose host is not in the approved allowlist (`discord.com`, `hooks.slack.com`, `api.frankfurter.dev`, `horizon.stellar.org`, `github.com`, `go.dev`, `pkg.go.dev`, `shields.io`, `stellar.org`, `stellar.expert`). Documentation files are exempt from URL restrictions.
  * **Cryptomining Signatures:** Keywords including `stratum+tcp`, `stratum+ssl`, `cryptonight`, `xmrig`, `minergate`, `coinhive`, `monero`, or `hashrate`.
  * **PR Scope Limits:** Exceeding `max_files` (50) or `max_changed_lines` (1000).

### 3.3 INFO (Automated Pass)

Informational findings that do not fail the check or require maintainer intervention:
* **Missing Issue Reference:** Not linking `Fixes #N` or `Closes #N` is recorded as an informational notice (`INFO`), encouraging good hygiene without failing the build.
* **Documentation and Test Changes:** PRs modifying documentation (`*.md`, `docs/**`) or test suites (`*_test.go`, `test/**`), containing no suspicious patterns, pass with `INFO` status and exit code 0.

---

## 4. Residual Risks and Scope Limitations

PR Guard provides automated defense against common automated attacks, accidental misconfigurations, and supply-chain injection. However, maintainers must recognize its inherent limitations:

1. **Contributor Workstation Invisibility:**
   PR Guard cannot inspect or verify the security posture of a contributor's local machine, IDE extensions, or build environment. Compromised developer machines may generate commits that appear syntactically benign.
2. **No Deep Semantic Execution:**
   PR Guard performs regex and structural metadata inspection; it is not an abstract interpretation engine, formal verification tool, or runtime behavioral sandbox.
3. **Complex Logic Obfuscation:**
   Subtle algorithmic backdoors, off-by-one errors in rate limiters, or timing attacks in cryptographic logic cannot be detected by static regex patterns alone.
4. **Third-Party Transitive Vulnerabilities:**
   While `go.mod` modifications are flagged for review, vulnerability tracking in existing dependencies relies on complementary CI tooling (`govulncheck`, `dependency-review`, and `CodeQL`).

---

## 5. Safe Review Guidelines for Maintainers

Maintainers reviewing PRs—especially those flagged with `needs-security-review`—must adhere to the following safety protocols:

### 5.1 Never Test Untrusted Code on Local Workstations
* **Do not run `git checkout <pr-branch>` and `go run` or `go test` on your bare host machine.**
* Untrusted Go tests or build tags can execute arbitrary code during `go test` or package initialization (`init()` functions).

### 5.2 Safe Inspection and Sandboxing
* **Inspect Diff in GitHub UI:** Review the diff directly in the GitHub web interface or using read-only patch review tools.
* **Use Ephemeral Sandboxed Containers:** If local execution or debugging is required:
  ```bash
  # Example: Run untrusted test suite in an isolated container without host network or sensitive volume mounts
  docker run --rm -v $(pwd):/workspace:ro --network none golang:1.22-alpine sh -c "cd /workspace && go test -v ./..."
  ```
* **Verify Dependency Changes:**
  Check `go.mod` diffs against official upstream repositories. Ensure no unexpected packages or replaced module versions are introduced.
* **Audit External Endpoints:**
  Confirm that all new URLs connect to documented, legitimate Stellar endpoints or official APIs.

### 5.3 Maintainer Review Checklist Before Applying 'security-reviewed'
- [ ] Automated CI checks pass (`PR Guard`, `CI / test`, `govulncheck`, `gosec`, `gitleaks`, `go mod verify`, `CodeQL`).
- [ ] If `needs-security-review` was flagged: inspect the specific rule findings reported in the PR Guard comment.
- [ ] For `go.mod` changes: verify package sources and ensure no unauthorized `replace` directives point to unvetted forks.
- [ ] For source code URLs: verify that external endpoints connect to documented, official services.
- [ ] For low-level packages (`os/exec`, `syscall`, `unsafe`): ensure implementation is strictly necessary and thoroughly vetted.
- [ ] Apply the `security-reviewed` label only after completing manual inspection.

---

## 6. Security and Quality Review Checklist (Tool Capabilities & Boundaries)

The following table factualizes the division of responsibilities across PR Guard, CI workflows, and manual maintainer reviews, explicitly noting where automated tools do not apply:

| Scope Item | Automated Tool Status | Scope and Boundaries |
|---|---|---|
| **Hiding API keys and secrets** | **Enforced in CI (Gitleaks)** | CI scans PR diffs via Gitleaks; PR Guard consumes tokens via environment variables without printing them. Neither tool scans contributor workstations. |
| **Protecting admin routes** | **N/A to PR Guard** | PR Guard does not host web routes or administrative endpoints; it runs as an ephemeral CI step. |
| **Checking CORS** | **N/A to PR Guard** | PR Guard does not serve web APIs. CORS configuration belongs to the Fairway application server (`internal/api/api.go`). |
| **Checking user access** | **Enforced in PR Guard** | Verifies PR author against maintainer allowlist and ensures only maintainer events can apply `security-reviewed` or modify protected paths. |
| **Checking env variables** | **Enforced in PR Guard** | Avoids shell string interpolation; inputs are safely parsed via JSON from disk. Tool does not inspect application runtime env variables. |
| **Sanitizing forms** | **N/A to PR Guard** | PR Guard has no interactive web forms; untrusted PR strings are ingested as static data. |
| **XSS protection** | **N/A to PR Guard** | Outputs Markdown comments on GitHub; does not render HTML in a browser client. |
| **Rate limiting** | **Enforced in PR Guard** | Uses paginated GitHub API requests (`per_page=100`) and enforces a 3000-file ceiling to avoid API rate limit exhaustion. Does not rate limit HTTP application requests. |
| **Proper authentication** | **Enforced in PR Guard** | Authenticates GitHub API requests via `Bearer` token (`GITHUB_TOKEN`). Tool does not manage application user authentication. |
| **Securing API endpoints and database access** | **N/A to PR Guard** | PR Guard does not expose public endpoints or query databases. |
| **Debug mode off** | **Enforced in Build** | Production binaries are compiled without test debug servers or memory dump flags enabled. |
| **Git history scanned for leaked secrets** | **Enforced in CI (Gitleaks)** | CI executes Gitleaks on the PR diff; PR Guard itself does not perform secret pattern scanning. |
| **Unused packages removed** | **Enforced in PR Guard** | `tools/prguard` is implemented strictly with the Go standard library, with zero external dependencies. |
| **Exposed files** | **Enforced in PR Guard** | Blocks modifications to repository configuration, workflows, licensing, and security policies from non-maintainers. |
| **Security headers** | **N/A to PR Guard** | PR Guard is a CI process, not an HTTP web server. |
| **Password hashing** | **N/A to PR Guard** | PR Guard handles no credentials or user password databases. |
| **Dependency updates** | **Enforced in CI & Guard** | PR Guard flags `go.mod` modifications and `replace` directives; `govulncheck` and `dependency-review` track dependency CVEs. |
| **Security audit** | **Enforced in CI (Gosec, CodeQL)** | Static AST analysis is performed by Gosec and CodeQL; PR Guard performs regex diff pattern matching only. |
| **UI/UX psychology** | **Enforced in PR Guard** | Automated bot comments are phrased respectfully, constructively, and kindly for good-faith contributors, providing clear instructions on requesting review or exceptions. |
