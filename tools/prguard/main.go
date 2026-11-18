package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type githubEventPayload struct {
	PullRequest struct {
		Number       int    `json:"number"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		ChangedFiles int    `json:"changed_files"`
		Additions    int    `json:"additions"`
		Deletions    int    `json:"deletions"`
		User         struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func main() {
	var (
		policyPath      string
		filesPath       string
		eventPath       string
		repoName        string
		prNumber        int
		githubToken     string
		authorOverride  string
		titleOverride   string
		bodyOverride    string
		outputPath      string
		commentOutPath  string
		applyActions    bool
		exitCode        bool
		contributingURL string
	)

	flag.StringVar(&policyPath, "policy", ".github/prguard.yml", "Path to prguard policy YAML/JSON file")
	flag.StringVar(&filesPath, "files", "", "Path to PR files JSON fixture or pre-fetched files")
	flag.StringVar(&eventPath, "event", "", "Path to GitHub event JSON file")
	flag.StringVar(&repoName, "repo", os.Getenv("GITHUB_REPOSITORY"), "GitHub repository (owner/repo)")
	flag.IntVar(&prNumber, "pr-number", 0, "Pull request number")
	flag.StringVar(&githubToken, "token", os.Getenv("GITHUB_TOKEN"), "GitHub access token")
	flag.StringVar(&authorOverride, "author", "", "Override PR author login")
	flag.StringVar(&titleOverride, "title", "", "Override PR title")
	flag.StringVar(&bodyOverride, "body", "", "Override PR body")
	flag.StringVar(&outputPath, "output", "", "Optional path to write structured result JSON")
	flag.StringVar(&commentOutPath, "comment-out", "", "Optional path to write formatted comment markdown")
	flag.BoolVar(&applyActions, "apply", false, "Apply GitHub API actions (comment, label, close)")
	flag.BoolVar(&exitCode, "exit-code", true, "Exit with non-zero code on BLOCK or REVIEW")
	flag.StringVar(&contributingURL, "contributing-url", "https://github.com/gofairway/fairway/blob/main/CONTRIBUTING.md#protected-paths-and-automated-checks", "URL to contributing section")
	flag.Parse()

	// 1. Load Policy
	policy, err := LoadPolicy(policyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading policy from %s: %v\n", policyPath, err)
		os.Exit(2)
	}

	// 2. Parse PR Metadata
	var pr PRMetadata
	if eventPath != "" {
		eventData, err := os.ReadFile(filepath.Clean(eventPath)) // #nosec G304 - GitHub event file path from environment
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading event file %s: %v\n", eventPath, err)
			os.Exit(2)
		}
		var payload githubEventPayload
		if err := json.Unmarshal(eventData, &payload); err == nil && payload.PullRequest.Number > 0 {
			pr.Number = payload.PullRequest.Number
			pr.Title = payload.PullRequest.Title
			pr.Body = payload.PullRequest.Body
			pr.Author = payload.PullRequest.User.Login
			pr.ChangedFiles = payload.PullRequest.ChangedFiles
			pr.Additions = payload.PullRequest.Additions
			pr.Deletions = payload.PullRequest.Deletions
			if repoName == "" && payload.Repository.FullName != "" {
				repoName = payload.Repository.FullName
			}
		} else {
			// Try parsing as raw PRMetadata
			_ = json.Unmarshal(eventData, &pr)
		}
	}

	if prNumber > 0 {
		pr.Number = prNumber
	}
	if authorOverride != "" {
		pr.Author = authorOverride
	}
	if titleOverride != "" {
		pr.Title = titleOverride
	}
	if bodyOverride != "" {
		pr.Body = bodyOverride
	}

	// 3. Obtain PR Files
	var files []PRFile
	if filesPath != "" {
		filesData, err := os.ReadFile(filepath.Clean(filesPath)) // #nosec G304 - local fixture path specified via CLI
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading files JSON %s: %v\n", filesPath, err)
			os.Exit(2)
		}
		if err := json.Unmarshal(filesData, &files); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing files JSON: %v\n", err)
			os.Exit(2)
		}
	} else if githubToken != "" && repoName != "" && pr.Number > 0 {
		parts := strings.Split(repoName, "/")
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Invalid repository name %q, expected 'owner/repo'\n", repoName)
			os.Exit(2)
		}
		client := NewGitHubClient("", githubToken)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		fetchedFiles, err := client.FetchPRFiles(ctx, parts[0], parts[1], pr.Number)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching PR files from GitHub API: %v\n", err)
			os.Exit(2)
		}
		files = fetchedFiles
	} else {
		fmt.Fprintln(os.Stderr, "Error: must specify either -files <path> or -token with -repo and -pr-number")
		os.Exit(2)
	}

	// 4. Run Guard Evaluation
	result := Evaluate(policy, pr, files)

	// Format output JSON
	outBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling result JSON: %v\n", err)
		os.Exit(2)
	}

	// Print JSON to stdout
	fmt.Println(string(outBytes))

	// Optionally save output to file
	if outputPath != "" {
		if err := os.WriteFile(filepath.Clean(outputPath), outBytes, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output to %s: %v\n", outputPath, err)
		}
	}

	// Format comment markdown
	commentText := FormatComment(result, pr, contributingURL)
	if commentOutPath != "" {
		if err := os.WriteFile(filepath.Clean(commentOutPath), []byte(commentText), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing comment markdown to %s: %v\n", commentOutPath, err)
		}
	}

	// 5. Apply GitHub Actions if requested
	if applyActions && githubToken != "" && repoName != "" && pr.Number > 0 {
		parts := strings.Split(repoName, "/")
		if len(parts) == 2 {
			owner, repo := parts[0], parts[1]
			client := NewGitHubClient("", githubToken)
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()

			switch result.Action {
			case ActionBlock:
				// Post comment and close PR
				if err := client.PostOrUpdateComment(ctx, owner, repo, pr.Number, commentText); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to post comment: %v\n", err)
				}
				if err := client.ClosePR(ctx, owner, repo, pr.Number); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to close PR: %v\n", err)
				}

			case ActionReview:
				// Post one updating comment and apply needs-security-review label
				if err := client.PostOrUpdateComment(ctx, owner, repo, pr.Number, commentText); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to post/update comment: %v\n", err)
				}
				if err := client.ApplyLabel(ctx, owner, repo, pr.Number, "needs-security-review"); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to apply label: %v\n", err)
				}

			case ActionInfo:
				// Do nothing on INFO as specified in requirement
			}
		}
	}

	// 6. Exit code
	if exitCode && (result.Action == ActionBlock || result.Action == ActionReview) {
		os.Exit(1)
	}
}
