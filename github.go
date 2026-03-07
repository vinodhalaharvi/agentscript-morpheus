package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// GitHubClient handles GitHub API operations
type GitHubClient struct {
	httpClient *http.Client
	username   string
	token      *oauth2.Token
}

// NewGitHubClient creates a new GitHub client with OAuth2
func NewGitHubClient(ctx context.Context, clientID, clientSecret, tokenFile string) (*GitHubClient, error) {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"repo", "read:user"},
		Endpoint:     github.Endpoint,
	}

	// Try to load existing token
	token, err := loadGitHubToken(tokenFile)
	if err != nil {
		// Need to get new token via device flow (easier than web redirect)
		token, err = getGitHubDeviceToken(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to get GitHub token: %w", err)
		}
		// Save token for future use
		if err := saveGitHubToken(tokenFile, token); err != nil {
			fmt.Printf("Warning: could not save token: %v\n", err)
		}
	}

	// Create HTTP client with token
	client := config.Client(ctx, token)

	// Get username
	username, err := getGitHubUsername(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to get username: %w", err)
	}

	return &GitHubClient{
		httpClient: client,
		username:   username,
		token:      token,
	}, nil
}

// getGitHubDeviceToken uses device flow for authentication
func getGitHubDeviceToken(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	// Device flow request
	reqBody := fmt.Sprintf("client_id=%s&scope=%s", config.ClientID, strings.Join(config.Scopes, " "))
	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/device/code", strings.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var deviceResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return nil, err
	}

	fmt.Printf("\n🔐 GitHub Authorization Required\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("1. Go to: %s\n", deviceResp.VerificationURI)
	fmt.Printf("2. Enter code: %s\n", deviceResp.UserCode)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Waiting for authorization...\n")

	// Poll for token
	interval := deviceResp.Interval
	if interval < 5 {
		interval = 5
	}

	for {
		time.Sleep(time.Duration(interval) * time.Second)

		tokenReq := fmt.Sprintf("client_id=%s&device_code=%s&grant_type=urn:ietf:params:oauth:grant-type:device_code",
			config.ClientID, deviceResp.DeviceCode)

		req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(tokenReq))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		var tokenResp struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			Scope       string `json:"scope"`
			Error       string `json:"error"`
		}

		json.NewDecoder(resp.Body).Decode(&tokenResp)
		resp.Body.Close()

		if tokenResp.AccessToken != "" {
			fmt.Printf("✅ GitHub authorized!\n\n")
			return &oauth2.Token{
				AccessToken: tokenResp.AccessToken,
				TokenType:   tokenResp.TokenType,
			}, nil
		}

		if tokenResp.Error == "authorization_pending" {
			continue
		}

		if tokenResp.Error != "" {
			return nil, fmt.Errorf("authorization failed: %s", tokenResp.Error)
		}
	}
}

func loadGitHubToken(filename string) (*oauth2.Token, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func saveGitHubToken(filename string, token *oauth2.Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0600)
}

func getGitHubUsername(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}
	return user.Login, nil
}

// CreateRepo creates a new GitHub repository
func (g *GitHubClient) CreateRepo(ctx context.Context, name, description string, private bool) (string, error) {
	reqBody := map[string]interface{}{
		"name":        name,
		"description": description,
		"private":     private,
		"auto_init":   true, // Create with README
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/user/repos", bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 422 {
		// Repo might already exist
		return fmt.Sprintf("https://github.com/%s/%s", g.username, name), nil
	}

	if resp.StatusCode != 201 {
		return "", fmt.Errorf("failed to create repo: %s", string(body))
	}

	var repo struct {
		HTMLURL string `json:"html_url"`
	}
	json.Unmarshal(body, &repo)
	return repo.HTMLURL, nil
}

// DeployToPages deploys content to GitHub Pages
func (g *GitHubClient) DeployToPages(ctx context.Context, repoName, title, content string) (string, error) {
	// 1. Create or use existing repo
	fmt.Printf("📁 Creating/updating repository: %s...\n", repoName)
	_, err := g.CreateRepo(ctx, repoName, "Generated by AgentScript", false)
	if err != nil {
		return "", fmt.Errorf("failed to create repo: %w", err)
	}

	// 2. Generate HTML content
	html := generateHTML(title, content)

	// 3. Upload index.html to the repo
	fmt.Printf("📄 Uploading index.html...\n")
	err = g.uploadFile(ctx, repoName, "index.html", html)
	if err != nil {
		return "", fmt.Errorf("failed to upload index.html: %w", err)
	}

	// 4. Enable GitHub Pages
	fmt.Printf("🌐 Enabling GitHub Pages...\n")
	err = g.enablePages(ctx, repoName)
	if err != nil {
		// Pages might already be enabled, continue
		fmt.Printf("Note: %v\n", err)
	}

	// 5. Return the Pages URL
	pagesURL := fmt.Sprintf("https://%s.github.io/%s", g.username, repoName)
	return pagesURL, nil
}

// DeployReactSPA deploys a React SPA to GitHub Pages
func (g *GitHubClient) DeployReactSPA(ctx context.Context, repoName, title, reactCode string) (string, error) {
	// 1. Create or use existing repo
	fmt.Printf("📁 Creating/updating repository: %s...\n", repoName)
	_, err := g.CreateRepo(ctx, repoName, fmt.Sprintf("%s - Generated by AgentScript", title), false)
	if err != nil {
		return "", fmt.Errorf("failed to create repo: %w", err)
	}

	// 2. Upload index.html (the React SPA)
	fmt.Printf("📄 Uploading React SPA...\n")
	err = g.uploadFile(ctx, repoName, "index.html", reactCode)
	if err != nil {
		return "", fmt.Errorf("failed to upload index.html: %w", err)
	}

	// 3. Create a simple README
	readme := fmt.Sprintf(`# %s

Generated by [AgentScript](https://github.com/vinodhalaharvi/agentscript) - A DSL for AI Agents

## Live Demo
🌐 [View Live](%s)

## Built With
- React (via Babel Standalone)
- Tailwind CSS
- Gemini AI for content generation
- GitHub Pages for hosting

---
*Created with AgentScript for the Gemini 3 Hackathon*
`, title, fmt.Sprintf("https://%s.github.io/%s", g.username, repoName))

	err = g.uploadFile(ctx, repoName, "README.md", readme)
	if err != nil {
		// Non-fatal, continue
		fmt.Printf("Note: Could not create README: %v\n", err)
	}

	// 4. Enable GitHub Pages
	fmt.Printf("🌐 Enabling GitHub Pages...\n")
	err = g.enablePages(ctx, repoName)
	if err != nil {
		// Pages might already be enabled, continue
		fmt.Printf("Note: %v\n", err)
	}

	// 5. Return the Pages URL
	pagesURL := fmt.Sprintf("https://%s.github.io/%s", g.username, repoName)
	return pagesURL, nil
}

// uploadFile uploads or updates a file in a repository
func (g *GitHubClient) uploadFile(ctx context.Context, repo, path, content string) error {
	// First, try to get existing file SHA (needed for updates)
	var sha string
	getURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", g.username, repo, path)

	req, _ := http.NewRequestWithContext(ctx, "GET", getURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.httpClient.Do(req)
	if err == nil && resp.StatusCode == 200 {
		var existing struct {
			SHA string `json:"sha"`
		}
		json.NewDecoder(resp.Body).Decode(&existing)
		sha = existing.SHA
		resp.Body.Close()
	}

	// Create/update file
	reqBody := map[string]interface{}{
		"message": "Update via AgentScript",
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
	}
	if sha != "" {
		reqBody["sha"] = sha
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err = http.NewRequestWithContext(ctx, "PUT", getURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err = g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed: %s", string(body))
	}

	return nil
}

// enablePages enables GitHub Pages for a repository
func (g *GitHubClient) enablePages(ctx context.Context, repo string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pages", g.username, repo)

	reqBody := map[string]interface{}{
		"source": map[string]string{
			"branch": "main",
			"path":   "/",
		},
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 201 = created, 409 = already exists (both OK)
	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("enable pages failed: %s", string(body))
	}

	return nil
}

// UploadAssets uploads multiple files (images, css, etc.) to a repo
func (g *GitHubClient) UploadAssets(ctx context.Context, repo string, files map[string]string) error {
	for path, content := range files {
		if err := g.uploadFile(ctx, repo, path, content); err != nil {
			return fmt.Errorf("failed to upload %s: %w", path, err)
		}
	}
	return nil
}

// UploadBinaryFile uploads a binary file (like images) to a repo
func (g *GitHubClient) UploadBinaryFile(ctx context.Context, repo, remotePath, localPath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}

	// Get existing SHA if file exists
	var sha string
	getURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", g.username, repo, remotePath)

	req, _ := http.NewRequestWithContext(ctx, "GET", getURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.httpClient.Do(req)
	if err == nil && resp.StatusCode == 200 {
		var existing struct {
			SHA string `json:"sha"`
		}
		json.NewDecoder(resp.Body).Decode(&existing)
		sha = existing.SHA
		resp.Body.Close()
	}

	reqBody := map[string]interface{}{
		"message": "Upload " + filepath.Base(localPath),
		"content": base64.StdEncoding.EncodeToString(data),
	}
	if sha != "" {
		reqBody["sha"] = sha
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err = http.NewRequestWithContext(ctx, "PUT", getURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err = g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed: %s", string(body))
	}

	return nil
}

// generateHTML creates a beautiful HTML page from content
func generateHTML(title, content string) string {
	// Convert markdown-like content to HTML paragraphs
	paragraphs := strings.Split(content, "\n\n")
	var htmlContent strings.Builder
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Check for headers
		if strings.HasPrefix(p, "# ") {
			htmlContent.WriteString(fmt.Sprintf("<h1>%s</h1>\n", strings.TrimPrefix(p, "# ")))
		} else if strings.HasPrefix(p, "## ") {
			htmlContent.WriteString(fmt.Sprintf("<h2>%s</h2>\n", strings.TrimPrefix(p, "## ")))
		} else if strings.HasPrefix(p, "### ") {
			htmlContent.WriteString(fmt.Sprintf("<h3>%s</h3>\n", strings.TrimPrefix(p, "### ")))
		} else if strings.HasPrefix(p, "- ") || strings.HasPrefix(p, "* ") {
			// List items
			items := strings.Split(p, "\n")
			htmlContent.WriteString("<ul>\n")
			for _, item := range items {
				item = strings.TrimPrefix(strings.TrimPrefix(item, "- "), "* ")
				htmlContent.WriteString(fmt.Sprintf("  <li>%s</li>\n", item))
			}
			htmlContent.WriteString("</ul>\n")
		} else {
			htmlContent.WriteString(fmt.Sprintf("<p>%s</p>\n", p))
		}
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <style>
        :root {
            --primary: #6366f1;
            --primary-dark: #4f46e5;
            --bg: #0f172a;
            --bg-card: #1e293b;
            --text: #e2e8f0;
            --text-muted: #94a3b8;
        }
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: var(--bg);
            color: var(--text);
            line-height: 1.7;
            min-height: 100vh;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
            padding: 4rem 2rem;
        }
        header {
            text-align: center;
            margin-bottom: 3rem;
            padding-bottom: 2rem;
            border-bottom: 1px solid rgba(255,255,255,0.1);
        }
        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            background: linear-gradient(135deg, var(--primary), #a855f7);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 0.5rem;
        }
        .subtitle {
            color: var(--text-muted);
            font-size: 0.9rem;
        }
        article {
            background: var(--bg-card);
            border-radius: 1rem;
            padding: 2rem;
            box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.3);
        }
        h2 {
            font-size: 1.5rem;
            color: var(--primary);
            margin: 2rem 0 1rem;
        }
        h3 {
            font-size: 1.25rem;
            color: var(--text);
            margin: 1.5rem 0 0.75rem;
        }
        p {
            margin-bottom: 1rem;
            color: var(--text);
        }
        ul, ol {
            margin: 1rem 0 1rem 1.5rem;
        }
        li {
            margin-bottom: 0.5rem;
            color: var(--text-muted);
        }
        a {
            color: var(--primary);
            text-decoration: none;
        }
        a:hover {
            text-decoration: underline;
        }
        footer {
            text-align: center;
            margin-top: 3rem;
            padding-top: 2rem;
            border-top: 1px solid rgba(255,255,255,0.1);
            color: var(--text-muted);
            font-size: 0.85rem;
        }
        .badge {
            display: inline-block;
            background: var(--primary);
            color: white;
            padding: 0.25rem 0.75rem;
            border-radius: 9999px;
            font-size: 0.75rem;
            font-weight: 600;
        }
        @media (max-width: 640px) {
            .container {
                padding: 2rem 1rem;
            }
            h1 {
                font-size: 1.75rem;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <span class="badge">AgentScript</span>
            <h1>%s</h1>
            <p class="subtitle">Generated on %s</p>
        </header>
        <article>
            %s
        </article>
        <footer>
            <p>Generated with ❤️ by <a href="https://github.com/vinodhalaharvi/agentscript">AgentScript</a></p>
        </footer>
    </div>
</body>
</html>`, title, title, time.Now().Format("January 2, 2006"), htmlContent.String())
}
