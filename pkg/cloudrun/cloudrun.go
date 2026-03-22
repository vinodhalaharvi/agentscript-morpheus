// Package cloudrun provides Cloud Run Job deployment and Cloud Scheduler
// integration for AgentScript DSL pipelines.
//
// Commands:
//
//	gcp_check                          — validate credentials + permissions
//	deploy "name" "script.as"          — build image, push to GCR, create Cloud Run Job
//	schedule "name" "0 9 * * *"        — create Cloud Scheduler trigger for a deployed job
//	undeploy "name"                    — delete Cloud Run Job + scheduler
//
// Environment variables required:
//
//	GCP_PROJECT          — GCP project ID
//	GCP_REGION           — GCP region (default: us-central1)
//	GCP_SERVICE_ACCOUNT  — service account email for the job
package cloudrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client handles GCP Cloud Run + Cloud Scheduler operations.
type Client struct {
	project        string
	region         string
	serviceAccount string
	verbose        bool
	httpClient     *http.Client
}

// NewClient creates a Cloud Run client from environment variables.
func NewClient(verbose bool) *Client {
	region := os.Getenv("GCP_REGION")
	if region == "" {
		region = "us-central1"
	}
	return &Client{
		project:        os.Getenv("GCP_PROJECT"),
		region:         region,
		serviceAccount: os.Getenv("GCP_SERVICE_ACCOUNT"),
		verbose:        verbose,
		httpClient:     &http.Client{Timeout: 60 * time.Second},
	}
}

// PermissionCheck represents a single prerequisite check.
type PermissionCheck struct {
	Name       string
	Check      func() error
	FixCommand string
	DocsURL    string
}

// CheckAll validates all prerequisites and returns a formatted report.
func (c *Client) CheckAll(ctx context.Context) (string, error) {
	checks := c.buildChecks(ctx)

	var sb strings.Builder
	sb.WriteString("GCP Prerequisites Check\n")
	sb.WriteString(strings.Repeat("─", 50) + "\n\n")

	allOK := true
	var fixes []string

	for _, check := range checks {
		err := check.Check()
		if err == nil {
			sb.WriteString(fmt.Sprintf("✅ %s\n", check.Name))
		} else {
			sb.WriteString(fmt.Sprintf("❌ %s — %s\n", check.Name, err.Error()))
			allOK = false
			if check.FixCommand != "" {
				fixes = append(fixes, fmt.Sprintf("# Fix: %s\n   %s", check.Name, check.FixCommand))
			}
			if check.DocsURL != "" {
				fixes = append(fixes, fmt.Sprintf("   Docs: %s", check.DocsURL))
			}
		}
	}

	sb.WriteString("\n")
	if allOK {
		sb.WriteString("✅ All checks passed! Ready to deploy.\n\n")
		sb.WriteString("Next steps:\n")
		sb.WriteString("  deploy \"my-job\" \"examples/ssl-monitor.as\"\n")
		sb.WriteString("  >=> schedule \"my-job\" \"0 9 * * *\"\n")
	} else {
		sb.WriteString("❌ Some checks failed. Fix the issues above:\n\n")
		for _, fix := range fixes {
			sb.WriteString(fix + "\n\n")
		}
	}

	return sb.String(), nil
}

// buildChecks returns all prerequisite checks in order.
func (c *Client) buildChecks(ctx context.Context) []PermissionCheck {
	return []PermissionCheck{
		{
			Name: "GCP_PROJECT env var set",
			Check: func() error {
				if c.project == "" {
					return fmt.Errorf("not set")
				}
				return nil
			},
			FixCommand: "export GCP_PROJECT=\"your-project-id\"",
		},
		{
			Name: "GCP_REGION env var set",
			Check: func() error {
				if c.region == "" {
					return fmt.Errorf("not set")
				}
				return nil
			},
			FixCommand: "export GCP_REGION=\"us-central1\"",
		},
		{
			Name: "GCP_SERVICE_ACCOUNT env var set",
			Check: func() error {
				if c.serviceAccount == "" {
					return fmt.Errorf("not set")
				}
				return nil
			},
			FixCommand: "export GCP_SERVICE_ACCOUNT=\"agentscript@your-project.iam.gserviceaccount.com\"",
		},
		{
			Name: "gcloud CLI installed",
			Check: func() error {
				_, err := exec.LookPath("gcloud")
				if err != nil {
					return fmt.Errorf("not found in PATH")
				}
				return nil
			},
			FixCommand: "brew install google-cloud-sdk  # or https://cloud.google.com/sdk/docs/install",
			DocsURL:    "https://cloud.google.com/sdk/docs/install",
		},
		{
			Name: "docker CLI installed",
			Check: func() error {
				_, err := exec.LookPath("docker")
				if err != nil {
					return fmt.Errorf("not found in PATH")
				}
				return nil
			},
			FixCommand: "brew install --cask docker",
		},
		{
			Name: "Application Default Credentials valid",
			Check: func() error {
				token, err := c.getAccessToken(ctx)
				if err != nil || token == "" {
					return fmt.Errorf("not authenticated")
				}
				return nil
			},
			FixCommand: "gcloud auth application-default login",
			DocsURL:    "https://cloud.google.com/docs/authentication/provide-credentials-adc",
		},
		{
			Name: "Cloud Run API enabled",
			Check: func() error {
				return c.checkAPIEnabled(ctx, "run.googleapis.com")
			},
			FixCommand: fmt.Sprintf("gcloud services enable run.googleapis.com --project=%s", c.project),
			DocsURL:    "https://cloud.google.com/run/docs/reference/iam/roles",
		},
		{
			Name: "Cloud Scheduler API enabled",
			Check: func() error {
				return c.checkAPIEnabled(ctx, "cloudscheduler.googleapis.com")
			},
			FixCommand: fmt.Sprintf("gcloud services enable cloudscheduler.googleapis.com --project=%s", c.project),
		},
		{
			Name: "Artifact Registry API enabled",
			Check: func() error {
				return c.checkAPIEnabled(ctx, "artifactregistry.googleapis.com")
			},
			FixCommand: fmt.Sprintf("gcloud services enable artifactregistry.googleapis.com --project=%s", c.project),
		},
		{
			Name: "Service account has roles/run.admin",
			Check: func() error {
				return c.checkIAMRole(ctx, "roles/run.admin")
			},
			FixCommand: fmt.Sprintf(
				"gcloud projects add-iam-policy-binding %s \\\n     --member=\"serviceAccount:%s\" \\\n     --role=\"roles/run.admin\"",
				c.project, c.serviceAccount,
			),
			DocsURL: "https://cloud.google.com/run/docs/reference/iam/roles",
		},
		{
			Name: "Service account has roles/storage.admin",
			Check: func() error {
				return c.checkIAMRole(ctx, "roles/storage.admin")
			},
			FixCommand: fmt.Sprintf(
				"gcloud projects add-iam-policy-binding %s \\\n     --member=\"serviceAccount:%s\" \\\n     --role=\"roles/storage.admin\"",
				c.project, c.serviceAccount,
			),
		},
		{
			Name: "Service account has roles/cloudscheduler.admin",
			Check: func() error {
				return c.checkIAMRole(ctx, "roles/cloudscheduler.admin")
			},
			FixCommand: fmt.Sprintf(
				"gcloud projects add-iam-policy-binding %s \\\n     --member=\"serviceAccount:%s\" \\\n     --role=\"roles/cloudscheduler.admin\"",
				c.project, c.serviceAccount,
			),
		},
	}
}

// Deploy builds a Docker image, pushes to GCR, and creates a Cloud Run Job.
func (c *Client) Deploy(ctx context.Context, jobName, scriptPath string) (string, error) {
	// Fast-fail permission check first
	if err := c.quickPermissionCheck(ctx); err != nil {
		return "", err
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return "", fmt.Errorf("script file not found: %s\n\n  Make sure the file exists:\n  ls -la %s", scriptPath, scriptPath)
	}

	imageTag := fmt.Sprintf("gcr.io/%s/agentscript-%s:latest", c.project, jobName)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🚀 Deploying %q to Cloud Run...\n\n", jobName))

	// Step 1: Build Docker image
	sb.WriteString("📦 Step 1/4: Building Docker image...\n")
	if err := c.buildDockerImage(ctx, imageTag, scriptPath); err != nil {
		return "", c.wrapDeployError("docker build", err)
	}
	sb.WriteString(fmt.Sprintf("   ✅ Image built: %s\n\n", imageTag))

	// Step 2: Push to GCR
	sb.WriteString("📤 Step 2/4: Pushing image to GCR...\n")
	if err := c.pushDockerImage(ctx, imageTag); err != nil {
		return "", c.wrapDeployError("docker push", err)
	}
	sb.WriteString(fmt.Sprintf("   ✅ Image pushed: %s\n\n", imageTag))

	// Step 3: Create/update Cloud Run Job
	sb.WriteString("☁️  Step 3/4: Creating Cloud Run Job...\n")
	if err := c.createCloudRunJob(ctx, jobName, imageTag, scriptPath); err != nil {
		return "", c.wrapDeployError("create cloud run job", err)
	}
	sb.WriteString(fmt.Sprintf("   ✅ Job created: %s\n\n", jobName))

	// Step 4: Summary
	sb.WriteString("✅ Step 4/4: Deploy complete!\n\n")
	sb.WriteString(fmt.Sprintf("Job name:   %s\n", jobName))
	sb.WriteString(fmt.Sprintf("Image:      %s\n", imageTag))
	sb.WriteString(fmt.Sprintf("Region:     %s\n", c.region))
	sb.WriteString(fmt.Sprintf("Project:    %s\n\n", c.project))
	sb.WriteString("Next step — schedule it:\n")
	sb.WriteString(fmt.Sprintf("  schedule \"%s\" \"0 9 * * *\"\n", jobName))
	sb.WriteString("\nOr run it manually:\n")
	sb.WriteString(fmt.Sprintf("  gcloud run jobs execute %s --region=%s\n", jobName, c.region))

	return sb.String(), nil
}

// Schedule creates a Cloud Scheduler job that triggers the Cloud Run Job.
func (c *Client) Schedule(ctx context.Context, jobName, cronExpr string) (string, error) {
	if err := c.quickPermissionCheck(ctx); err != nil {
		return "", err
	}

	schedulerName := fmt.Sprintf("agentscript-%s", jobName)
	jobURI := fmt.Sprintf(
		"https://%s-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/%s/jobs/%s:run",
		c.region, c.project, jobName,
	)

	token, err := c.getAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	payload := map[string]interface{}{
		"name":     fmt.Sprintf("projects/%s/locations/%s/jobs/%s", c.project, c.region, schedulerName),
		"schedule": cronExpr,
		"timeZone": "UTC",
		"httpTarget": map[string]interface{}{
			"uri":        jobURI,
			"httpMethod": "POST",
			"oauthToken": map[string]string{
				"serviceAccountEmail": c.serviceAccount,
			},
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf(
		"https://cloudscheduler.googleapis.com/v1/projects/%s/locations/%s/jobs",
		c.project, c.region,
	)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to create scheduler job: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 409 {
		// Already exists — update it
		return c.updateSchedule(ctx, schedulerName, cronExpr, jobURI, token)
	}

	if resp.StatusCode >= 400 {
		return "", c.wrapSchedulerError(resp.StatusCode, string(respBody))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✅ Scheduled %q\n\n", jobName))
	sb.WriteString(fmt.Sprintf("Schedule:   %s (UTC)\n", cronExpr))
	sb.WriteString(fmt.Sprintf("Scheduler:  %s\n", schedulerName))
	sb.WriteString(fmt.Sprintf("Job:        %s\n", jobName))
	sb.WriteString(fmt.Sprintf("Region:     %s\n\n", c.region))
	sb.WriteString(humanReadableCron(cronExpr) + "\n\n")
	sb.WriteString("Manage in console:\n")
	sb.WriteString(fmt.Sprintf("  https://console.cloud.google.com/cloudscheduler?project=%s\n", c.project))

	return sb.String(), nil
}

// Undeploy deletes the Cloud Run Job and its scheduler.
func (c *Client) Undeploy(ctx context.Context, jobName string) (string, error) {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🗑️  Undeploying %q...\n\n", jobName))

	// Delete scheduler
	schedulerURL := fmt.Sprintf(
		"https://cloudscheduler.googleapis.com/v1/projects/%s/locations/%s/jobs/agentscript-%s",
		c.project, c.region, jobName,
	)
	req, _ := http.NewRequestWithContext(ctx, "DELETE", schedulerURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err == nil && (resp.StatusCode == 200 || resp.StatusCode == 404) {
		sb.WriteString(fmt.Sprintf("✅ Scheduler deleted: agentscript-%s\n", jobName))
	}

	// Delete Cloud Run Job
	runURL := fmt.Sprintf(
		"https://%s-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/%s/jobs/%s",
		c.region, c.project, jobName,
	)
	req2, _ := http.NewRequestWithContext(ctx, "DELETE", runURL, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err2 := c.httpClient.Do(req2)
	if err2 == nil && (resp2.StatusCode == 200 || resp2.StatusCode == 404) {
		sb.WriteString(fmt.Sprintf("✅ Cloud Run Job deleted: %s\n", jobName))
	}

	sb.WriteString("\n✅ Undeploy complete.\n")
	return sb.String(), nil
}

// buildDockerImage builds a minimal Docker image for the job.
func (c *Client) buildDockerImage(ctx context.Context, imageTag, scriptPath string) error {
	// Create temp build context
	tmpDir, err := os.MkdirTemp("", "agentscript-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Copy script file
	scriptDest := filepath.Join(tmpDir, "script.as")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("failed to read script: %w", err)
	}
	if err := os.WriteFile(scriptDest, data, 0644); err != nil {
		return fmt.Errorf("failed to write script: %w", err)
	}

	// Find the agentscript binary
	binary, err := os.Executable()
	if err != nil {
		binary = "./agentscript"
	}

	// Copy binary
	binDest := filepath.Join(tmpDir, "agentscript")
	binData, err := os.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("failed to read binary %s: %w", binary, err)
	}
	if err := os.WriteFile(binDest, binData, 0755); err != nil {
		return fmt.Errorf("failed to write binary: %w", err)
	}

	// Write Dockerfile
	dockerfile := `FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY agentscript .
COPY script.as .
ENTRYPOINT ["./agentscript", "-f", "script.as"]
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageTag, tmpDir)
	cmd.Stdout = os.Stderr // progress to stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// pushDockerImage pushes the image to GCR.
func (c *Client) pushDockerImage(ctx context.Context, imageTag string) error {
	// Configure docker for GCR
	authCmd := exec.CommandContext(ctx, "gcloud", "auth", "configure-docker", "--quiet")
	authCmd.Stderr = os.Stderr
	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("gcloud auth configure-docker failed: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "push", imageTag)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// createCloudRunJob creates or updates a Cloud Run Job via REST API.
func (c *Client) createCloudRunJob(ctx context.Context, jobName, imageTag, scriptPath string) error {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}

	// Build env vars to pass through — all current env vars that look like API keys
	envVars := c.collectEnvVars()

	payload := map[string]interface{}{
		"apiVersion": "run.googleapis.com/v1",
		"kind":       "Job",
		"metadata": map[string]interface{}{
			"name":      jobName,
			"namespace": c.project,
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"serviceAccountName": c.serviceAccount,
							"containers": []map[string]interface{}{
								{
									"image": imageTag,
									"env":   envVars,
									"resources": map[string]interface{}{
										"limits": map[string]string{
											"cpu":    "1",
											"memory": "512Mi",
										},
									},
								},
							},
							"restartPolicy": "Never",
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf(
		"https://%s-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/%s/jobs",
		c.region, c.project,
	)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 409 {
		// Already exists — replace it
		return c.updateCloudRunJob(ctx, jobName, imageTag, envVars, token)
	}

	if resp.StatusCode >= 400 {
		return c.wrapJobError(resp.StatusCode, string(respBody))
	}

	return nil
}

// updateCloudRunJob replaces an existing Cloud Run Job.
func (c *Client) updateCloudRunJob(ctx context.Context, jobName, imageTag string, envVars []map[string]interface{}, token string) error {
	payload := map[string]interface{}{
		"apiVersion": "run.googleapis.com/v1",
		"kind":       "Job",
		"metadata": map[string]interface{}{
			"name":      jobName,
			"namespace": c.project,
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"serviceAccountName": c.serviceAccount,
							"containers": []map[string]interface{}{
								{
									"image": imageTag,
									"env":   envVars,
								},
							},
							"restartPolicy": "Never",
						},
					},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf(
		"https://%s-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/%s/jobs/%s",
		c.region, c.project, jobName,
	)

	req, _ := http.NewRequestWithContext(context.Background(), "PUT", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return c.wrapJobError(resp.StatusCode, string(respBody))
	}
	return nil
}

// updateSchedule updates an existing Cloud Scheduler job.
func (c *Client) updateSchedule(ctx context.Context, schedulerName, cronExpr, jobURI, token string) (string, error) {
	payload := map[string]interface{}{
		"schedule": cronExpr,
		"timeZone": "UTC",
		"httpTarget": map[string]interface{}{
			"uri":        jobURI,
			"httpMethod": "POST",
			"oauthToken": map[string]string{
				"serviceAccountEmail": c.serviceAccount,
			},
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf(
		"https://cloudscheduler.googleapis.com/v1/projects/%s/locations/%s/jobs/%s?updateMask=schedule,httpTarget",
		c.project, c.region, schedulerName,
	)

	req, _ := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return fmt.Sprintf("✅ Schedule updated: %s (%s UTC)\n%s\n",
		schedulerName, cronExpr, humanReadableCron(cronExpr)), nil
}

// collectEnvVars collects relevant env vars to pass into the Cloud Run Job.
func (c *Client) collectEnvVars() []map[string]interface{} {
	keys := []string{
		"GEMINI_API_KEY", "CLAUDE_API_KEY", "OPENAI_API_KEY",
		"PERPLEXITY_API_KEY", "SLACK_WEBHOOK_URL", "HASS_TOKEN",
		"HASS_HOST", "GITHUB_TOKEN", "FINNHUB_API_KEY", "GNEWS_API_KEY",
		"JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN",
		"KAFKA_BROKERS", "SEARCH_API_KEY",
	}
	var envVars []map[string]interface{}
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			envVars = append(envVars, map[string]interface{}{
				"name":  key,
				"value": val,
			})
		}
	}
	return envVars
}

// getAccessToken retrieves an access token via gcloud.
func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "print-access-token")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not authenticated — run: gcloud auth application-default login")
	}
	return strings.TrimSpace(string(out)), nil
}

// checkAPIEnabled checks if a GCP API is enabled.
func (c *Client) checkAPIEnabled(ctx context.Context, apiName string) error {
	if c.project == "" {
		return fmt.Errorf("GCP_PROJECT not set")
	}
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://serviceusage.googleapis.com/v1/projects/%s/services/%s", c.project, apiName)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	state, _ := result["state"].(string)
	if state != "ENABLED" {
		return fmt.Errorf("not enabled")
	}
	return nil
}

// checkIAMRole checks if the service account has a specific IAM role.
func (c *Client) checkIAMRole(ctx context.Context, role string) error {
	if c.project == "" || c.serviceAccount == "" {
		return fmt.Errorf("GCP_PROJECT or GCP_SERVICE_ACCOUNT not set")
	}
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://cloudresourcemanager.googleapis.com/v1/projects/%s:getIamPolicy", c.project)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var policy struct {
		Bindings []struct {
			Role    string   `json:"role"`
			Members []string `json:"members"`
		} `json:"bindings"`
	}
	json.NewDecoder(resp.Body).Decode(&policy)

	member := "serviceAccount:" + c.serviceAccount
	for _, binding := range policy.Bindings {
		if binding.Role == role {
			for _, m := range binding.Members {
				if m == member {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("not granted")
}

// quickPermissionCheck does a fast env var check before any API call.
func (c *Client) quickPermissionCheck(ctx context.Context) error {
	var missing []string
	if c.project == "" {
		missing = append(missing, "GCP_PROJECT")
	}
	if c.serviceAccount == "" {
		missing = append(missing, "GCP_SERVICE_ACCOUNT")
	}
	if len(missing) > 0 {
		return fmt.Errorf(`missing required environment variables: %s

  Fix:
    export GCP_PROJECT="your-project-id"
    export GCP_SERVICE_ACCOUNT="agentscript@your-project.iam.gserviceaccount.com"
    export GCP_REGION="us-central1"  # optional, defaults to us-central1

  Then run gcp_check to validate all prerequisites.`, strings.Join(missing, ", "))
	}
	return nil
}

// wrapDeployError wraps API errors with actionable messages.
func (c *Client) wrapDeployError(step string, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "403") || strings.Contains(msg, "permission") {
		return fmt.Errorf(`%s failed: permission denied

  Run gcp_check to see which permissions are missing:
    gcp_check

  Or grant all required roles at once:
    gcloud projects add-iam-policy-binding %s \
      --member="serviceAccount:%s" \
      --role="roles/editor"`, step, c.project, c.serviceAccount)
	}
	if strings.Contains(msg, "not found") || strings.Contains(msg, "404") {
		return fmt.Errorf(`%s failed: resource not found

  Make sure the Cloud Run API is enabled:
    gcloud services enable run.googleapis.com --project=%s`, step, c.project)
	}
	return fmt.Errorf("%s failed: %w", step, err)
}

// wrapSchedulerError wraps scheduler API errors with actionable messages.
func (c *Client) wrapSchedulerError(statusCode int, body string) error {
	if statusCode == 403 {
		return fmt.Errorf(`Cloud Scheduler permission denied

  Grant the required role:
    gcloud projects add-iam-policy-binding %s \
      --member="serviceAccount:%s" \
      --role="roles/cloudscheduler.admin"`, c.project, c.serviceAccount)
	}
	return fmt.Errorf("Cloud Scheduler API error %d: %s", statusCode, body)
}

// wrapJobError wraps Cloud Run Job API errors with actionable messages.
func (c *Client) wrapJobError(statusCode int, body string) error {
	if statusCode == 403 {
		return fmt.Errorf(`Cloud Run permission denied

  Grant the required role:
    gcloud projects add-iam-policy-binding %s \
      --member="serviceAccount:%s" \
      --role="roles/run.admin"`, c.project, c.serviceAccount)
	}
	return fmt.Errorf("Cloud Run API error %d: %s", statusCode, body)
}

// humanReadableCron converts a cron expression to a human-readable string.
func humanReadableCron(cron string) string {
	known := map[string]string{
		"0 9 * * *":    "Runs daily at 9:00 AM UTC",
		"*/5 * * * *":  "Runs every 5 minutes",
		"*/15 * * * *": "Runs every 15 minutes",
		"0 * * * *":    "Runs every hour",
		"0 9 * * MON":  "Runs every Monday at 9:00 AM UTC",
		"0 9 1 * *":    "Runs on the 1st of every month at 9:00 AM UTC",
		"0 0 * * *":    "Runs daily at midnight UTC",
		"*/30 * * * *": "Runs every 30 minutes",
	}
	if desc, ok := known[cron]; ok {
		return "⏰ " + desc
	}
	return "⏰ Schedule: " + cron + " (UTC)"
}
