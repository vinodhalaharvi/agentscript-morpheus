//go:build !nogoogle

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/docs/v1"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/forms/v1"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/people/v1"
	"google.golang.org/api/sheets/v4"
	"google.golang.org/api/tasks/v1"
	"google.golang.org/api/youtube/v3"
)

// GoogleClient handles all Google APIs
type GoogleClient struct {
	gmail    *gmail.Service
	calendar *calendar.Service
	drive    *drive.Service
	docs     *docs.Service
	sheets   *sheets.Service
	tasks    *tasks.Service
	people   *people.Service
	youtube  *youtube.Service
	forms    *forms.Service
	timezone string // User's timezone from calendar settings
}

// NewGoogleClient creates a new Google API client with OAuth2
func NewGoogleClient(ctx context.Context, credentialsFile, tokenFile string) (*GoogleClient, error) {
	// Read credentials
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file: %w", err)
	}

	// Configure OAuth2 with all required scopes
	config, err := google.ConfigFromJSON(b,
		// Gmail - send and read profile
		gmail.GmailSendScope,
		gmail.GmailReadonlyScope,
		// Calendar - full access for Meet links
		calendar.CalendarEventsScope,
		calendar.CalendarScope,
		// Drive - file creation
		drive.DriveFileScope,
		drive.DriveScope,
		// Docs - full access
		docs.DocumentsScope,
		// Sheets - full access
		sheets.SpreadsheetsScope,
		// Tasks - full access
		tasks.TasksScope,
		// Contacts - read
		people.ContactsReadonlyScope,
		// YouTube - read and upload
		youtube.YoutubeReadonlyScope,
		youtube.YoutubeUploadScope,
		// Forms - full access
		forms.FormsBodyScope,
		forms.FormsResponsesReadonlyScope,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %w", err)
	}

	// Get token
	token, err := getToken(ctx, config, tokenFile)
	if err != nil {
		return nil, fmt.Errorf("unable to get token: %w", err)
	}

	// Create HTTP client
	client := config.Client(ctx, token)

	// Create all services
	gmailSvc, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Gmail service: %w", err)
	}

	calendarSvc, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Calendar service: %w", err)
	}

	driveSvc, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Drive service: %w", err)
	}

	docsSvc, err := docs.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Docs service: %w", err)
	}

	sheetsSvc, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Sheets service: %w", err)
	}

	tasksSvc, err := tasks.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Tasks service: %w", err)
	}

	peopleSvc, err := people.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create People service: %w", err)
	}

	youtubeSvc, err := youtube.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create YouTube service: %w", err)
	}

	formsSvc, err := forms.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create Forms service: %w", err)
	}

	// Get user's timezone from calendar settings
	tz := "America/Los_Angeles" // default
	calSettings, err := calendarSvc.Settings.Get("timezone").Do()
	if err == nil && calSettings.Value != "" {
		tz = calSettings.Value
	}

	return &GoogleClient{
		gmail:    gmailSvc,
		calendar: calendarSvc,
		drive:    driveSvc,
		docs:     docsSvc,
		sheets:   sheetsSvc,
		tasks:    tasksSvc,
		people:   peopleSvc,
		youtube:  youtubeSvc,
		forms:    formsSvc,
		timezone: tz,
	}, nil
}

// getToken retrieves token from file or initiates OAuth flow
func getToken(ctx context.Context, config *oauth2.Config, tokenFile string) (*oauth2.Token, error) {
	// Try to load existing token
	token, err := loadToken(tokenFile)
	if err == nil {
		return token, nil
	}

	// No token, need to do OAuth dance
	token, err = getTokenFromWeb(ctx, config)
	if err != nil {
		return nil, err
	}

	// Save token for future use
	saveToken(tokenFile, token)
	return token, nil
}

// getTokenFromWeb starts OAuth flow
func getTokenFromWeb(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	// Use localhost redirect for desktop app
	config.RedirectURL = "http://localhost:8085/callback"

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("\n🔐 Google OAuth2 Authorization Required\n")
	fmt.Printf("1. Open this URL in your browser:\n\n%s\n\n", authURL)

	// Start local server to receive callback
	codeChan := make(chan string)
	errChan := make(chan error)

	server := &http.Server{Addr: ":8085"}
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no code in callback")
			return
		}
		fmt.Fprintf(w, "<h1>✅ Authorization successful!</h1><p>You can close this window.</p>")
		codeChan <- code
	})

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	fmt.Println("2. Waiting for authorization...")

	var code string
	select {
	case code = <-codeChan:
	case err := <-errChan:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authorization timeout")
	}

	server.Shutdown(ctx)

	// Exchange code for token
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("unable to exchange code: %w", err)
	}

	fmt.Println("✅ Authorization successful!")
	return token, nil
}

func loadToken(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	token := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(token)
	return token, err
}

func saveToken(file string, token *oauth2.Token) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(token)
}

// ============================================================================
// Gmail
// ============================================================================

// SendEmail sends an email via Gmail API
func (g *GoogleClient) SendEmail(ctx context.Context, to, subject, body string) error {
	// Get user's email address
	profile, err := g.gmail.Users.GetProfile("me").Do()
	if err != nil {
		return fmt.Errorf("unable to get user profile: %w", err)
	}
	from := profile.EmailAddress

	// Create email message
	msgStr := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)

	msg := &gmail.Message{
		Raw: base64.URLEncoding.EncodeToString([]byte(msgStr)),
	}

	// Send
	_, err = g.gmail.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return fmt.Errorf("unable to send email: %w", err)
	}

	return nil
}

// SendHTMLEmail sends an HTML email via Gmail API
func (g *GoogleClient) SendHTMLEmail(ctx context.Context, to, subject, htmlBody string) error {
	// Get user's email address
	profile, err := g.gmail.Users.GetProfile("me").Do()
	if err != nil {
		return fmt.Errorf("unable to get user profile: %w", err)
	}
	from := profile.EmailAddress

	// Create email message with HTML content type
	msgStr := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		from, to, subject, htmlBody)

	msg := &gmail.Message{
		Raw: base64.URLEncoding.EncodeToString([]byte(msgStr)),
	}

	// Send
	_, err = g.gmail.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return fmt.Errorf("unable to send email: %w", err)
	}

	return nil
}

// SendEmailWithAttachment sends an email with file attachment via Gmail API
func (g *GoogleClient) SendEmailWithAttachment(ctx context.Context, to, subject, body, attachmentPath string) error {
	// Get user's email address
	profile, err := g.gmail.Users.GetProfile("me").Do()
	if err != nil {
		return fmt.Errorf("unable to get user profile: %w", err)
	}
	from := profile.EmailAddress

	// Read attachment file
	attachmentData, err := os.ReadFile(attachmentPath)
	if err != nil {
		return fmt.Errorf("unable to read attachment: %w", err)
	}

	// Get filename and mime type
	filename := filepath.Base(attachmentPath)
	mimeType := "application/octet-stream"
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		mimeType = "image/png"
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".gif":
		mimeType = "image/gif"
	case ".pdf":
		mimeType = "application/pdf"
	case ".txt":
		mimeType = "text/plain"
	case ".mp4":
		mimeType = "video/mp4"
	case ".wav":
		mimeType = "audio/wav"
	case ".mp3":
		mimeType = "audio/mpeg"
	}

	// Create boundary
	boundary := "boundary_agentscript_" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Build multipart message
	var msgBuf bytes.Buffer
	msgBuf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msgBuf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msgBuf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msgBuf.WriteString("MIME-Version: 1.0\r\n")
	msgBuf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))

	// Body part
	msgBuf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msgBuf.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	msgBuf.WriteString(body)
	msgBuf.WriteString("\r\n")

	// Attachment part
	msgBuf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msgBuf.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", mimeType, filename))
	msgBuf.WriteString("Content-Transfer-Encoding: base64\r\n")
	msgBuf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", filename))
	msgBuf.WriteString(base64.StdEncoding.EncodeToString(attachmentData))
	msgBuf.WriteString("\r\n")

	// End boundary
	msgBuf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	msg := &gmail.Message{
		Raw: base64.URLEncoding.EncodeToString(msgBuf.Bytes()),
	}

	// Send
	_, err = g.gmail.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return fmt.Errorf("unable to send email: %w", err)
	}

	return nil
}

// ============================================================================
// Calendar
// ============================================================================

// GetTimezone returns the user's timezone from calendar settings
func (g *GoogleClient) GetTimezone() string {
	return g.timezone
}

// CreateCalendarEvent creates an event in Google Calendar
func (g *GoogleClient) CreateCalendarEvent(ctx context.Context, summary, description, startTime, endTime string) (*calendar.Event, error) {
	event := &calendar.Event{
		Summary:     summary,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: startTime,
			TimeZone: "America/Los_Angeles",
		},
		End: &calendar.EventDateTime{
			DateTime: endTime,
			TimeZone: "America/Los_Angeles",
		},
	}

	event, err := g.calendar.Events.Insert("primary", event).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create event: %w", err)
	}

	return event, nil
}

// CreateMeetEvent creates a Google Calendar event with Meet link
func (g *GoogleClient) CreateMeetEvent(ctx context.Context, summary, description, startTime, endTime string) (*calendar.Event, error) {
	event := &calendar.Event{
		Summary:     summary,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: startTime,
			TimeZone: "America/Los_Angeles",
		},
		End: &calendar.EventDateTime{
			DateTime: endTime,
			TimeZone: "America/Los_Angeles",
		},
		ConferenceData: &calendar.ConferenceData{
			CreateRequest: &calendar.CreateConferenceRequest{
				RequestId: fmt.Sprintf("meet-%d", time.Now().UnixNano()),
				ConferenceSolutionKey: &calendar.ConferenceSolutionKey{
					Type: "hangoutsMeet",
				},
			},
		},
	}

	event, err := g.calendar.Events.Insert("primary", event).ConferenceDataVersion(1).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create Meet event: %w", err)
	}

	return event, nil
}

// ============================================================================
// Google Drive
// ============================================================================

// SaveToDrive saves content to Google Drive
func (g *GoogleClient) SaveToDrive(ctx context.Context, path, content string) (*drive.File, error) {
	// Parse path to get folder and filename
	parts := strings.Split(path, "/")
	filename := parts[len(parts)-1]
	folderPath := parts[:len(parts)-1]

	// Find or create folder hierarchy
	parentID := "root"
	for _, folderName := range folderPath {
		if folderName == "" {
			continue
		}
		// Search for existing folder
		query := fmt.Sprintf("name='%s' and '%s' in parents and mimeType='application/vnd.google-apps.folder' and trashed=false", folderName, parentID)
		result, err := g.drive.Files.List().Q(query).Fields("files(id, name)").Do()
		if err != nil {
			return nil, fmt.Errorf("unable to search for folder: %w", err)
		}

		if len(result.Files) > 0 {
			parentID = result.Files[0].Id
		} else {
			// Create folder
			folder := &drive.File{
				Name:     folderName,
				MimeType: "application/vnd.google-apps.folder",
				Parents:  []string{parentID},
			}
			created, err := g.drive.Files.Create(folder).Do()
			if err != nil {
				return nil, fmt.Errorf("unable to create folder: %w", err)
			}
			parentID = created.Id
		}
	}

	// Create file
	file := &drive.File{
		Name:    filename,
		Parents: []string{parentID},
	}

	created, err := g.drive.Files.Create(file).Media(strings.NewReader(content)).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create file: %w", err)
	}

	return created, nil
}

// ============================================================================
// Google Docs
// ============================================================================

// CreateDoc creates a Google Doc with content
func (g *GoogleClient) CreateDoc(ctx context.Context, title, content string) (*docs.Document, error) {
	// Create the document
	doc := &docs.Document{
		Title: title,
	}

	created, err := g.docs.Documents.Create(doc).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create document: %w", err)
	}

	// Insert content
	requests := []*docs.Request{
		{
			InsertText: &docs.InsertTextRequest{
				Location: &docs.Location{
					Index: 1,
				},
				Text: content,
			},
		},
	}

	_, err = g.docs.Documents.BatchUpdate(created.DocumentId, &docs.BatchUpdateDocumentRequest{
		Requests: requests,
	}).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to insert content: %w", err)
	}

	return created, nil
}

// ============================================================================
// Google Sheets
// ============================================================================

// AppendToSheet appends data to a Google Sheet
func (g *GoogleClient) AppendToSheet(ctx context.Context, spreadsheetID, sheetName, content string) error {
	// Parse content into rows (split by newlines, columns by tabs or |)
	lines := strings.Split(content, "\n")
	var values [][]interface{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Try tab first, then pipe
		var cells []string
		if strings.Contains(line, "\t") {
			cells = strings.Split(line, "\t")
		} else if strings.Contains(line, "|") {
			cells = strings.Split(line, "|")
		} else {
			cells = []string{line}
		}
		var row []interface{}
		for _, cell := range cells {
			row = append(row, strings.TrimSpace(cell))
		}
		values = append(values, row)
	}

	// If no values, just add the content as a single cell
	if len(values) == 0 {
		values = [][]interface{}{{content}}
	}

	rangeStr := sheetName
	if sheetName == "" {
		rangeStr = "Sheet1"
	}

	vr := &sheets.ValueRange{
		Values: values,
	}

	_, err := g.sheets.Spreadsheets.Values.Append(spreadsheetID, rangeStr, vr).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("INSERT_ROWS").
		Do()
	if err != nil {
		return fmt.Errorf("unable to append to sheet: %w", err)
	}

	return nil
}

// CreateSheet creates a new Google Sheet
func (g *GoogleClient) CreateSheet(ctx context.Context, title string) (*sheets.Spreadsheet, error) {
	spreadsheet := &sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{
			Title: title,
		},
	}

	created, err := g.sheets.Spreadsheets.Create(spreadsheet).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create spreadsheet: %w", err)
	}

	return created, nil
}

// ============================================================================
// Google Tasks
// ============================================================================

// CreateTask creates a task in Google Tasks
func (g *GoogleClient) CreateTask(ctx context.Context, title, notes string) (*tasks.Task, error) {
	// Get default task list
	taskLists, err := g.tasks.Tasklists.List().Do()
	if err != nil {
		return nil, fmt.Errorf("unable to get task lists: %w", err)
	}

	var taskListID string
	if len(taskLists.Items) > 0 {
		taskListID = taskLists.Items[0].Id
	} else {
		// Create a task list
		newList := &tasks.TaskList{Title: "AgentScript Tasks"}
		created, err := g.tasks.Tasklists.Insert(newList).Do()
		if err != nil {
			return nil, fmt.Errorf("unable to create task list: %w", err)
		}
		taskListID = created.Id
	}

	// Create task
	task := &tasks.Task{
		Title: title,
		Notes: notes,
	}

	created, err := g.tasks.Tasks.Insert(taskListID, task).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to create task: %w", err)
	}

	return created, nil
}

// ============================================================================
// Google Contacts (People API)
// ============================================================================

// FindContact finds a contact by name
func (g *GoogleClient) FindContact(ctx context.Context, name string) ([]*people.Person, error) {
	// Search contacts
	result, err := g.people.People.SearchContacts().Query(name).ReadMask("names,emailAddresses").Do()
	if err != nil {
		return nil, fmt.Errorf("unable to search contacts: %w", err)
	}

	var contacts []*people.Person
	for _, res := range result.Results {
		contacts = append(contacts, res.Person)
	}

	return contacts, nil
}

// ============================================================================
// YouTube
// ============================================================================

// SearchYouTube searches for videos
func (g *GoogleClient) SearchYouTube(ctx context.Context, query string, maxResults int64) ([]*youtube.SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	call := g.youtube.Search.List([]string{"snippet"}).
		Q(query).
		Type("video").
		MaxResults(maxResults)

	response, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("unable to search YouTube: %w", err)
	}

	return response.Items, nil
}

// UploadToYouTube uploads a video to YouTube
func (g *GoogleClient) UploadToYouTube(ctx context.Context, videoPath, title, description string) (string, error) {
	// Open video file
	file, err := os.Open(videoPath)
	if err != nil {
		return "", fmt.Errorf("unable to open video file: %w", err)
	}
	defer file.Close()

	// Create video metadata
	video := &youtube.Video{
		Snippet: &youtube.VideoSnippet{
			Title:       title,
			Description: description,
			CategoryId:  "22", // People & Blogs category
		},
		Status: &youtube.VideoStatus{
			PrivacyStatus: "unlisted", // Start as unlisted for safety
		},
	}

	// Upload the video
	call := g.youtube.Videos.Insert([]string{"snippet", "status"}, video)
	call = call.Media(file)

	response, err := call.Do()
	if err != nil {
		return "", fmt.Errorf("unable to upload video: %w", err)
	}

	videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", response.Id)
	return videoURL, nil
}

// ============================================================================
// Forms
// ============================================================================

// FormQuestion represents a question to add to a form
type FormQuestion struct {
	Title    string   `json:"title"`
	Type     string   `json:"type"` // text, paragraph, multiple_choice, checkbox, dropdown
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"` // for multiple choice/checkbox/dropdown
}

// CreateForm creates a Google Form with the given title and questions
func (g *GoogleClient) CreateForm(ctx context.Context, title string, description string, questions []FormQuestion) (string, string, error) {
	// First create an empty form
	form := &forms.Form{
		Info: &forms.Info{
			Title:         title,
			DocumentTitle: title,
		},
	}

	createdForm, err := g.forms.Forms.Create(form).Do()
	if err != nil {
		return "", "", fmt.Errorf("unable to create form: %w", err)
	}

	// Add description if provided (separate call)
	if description != "" {
		_, err = g.forms.Forms.BatchUpdate(createdForm.FormId, &forms.BatchUpdateFormRequest{
			Requests: []*forms.Request{{
				UpdateFormInfo: &forms.UpdateFormInfoRequest{
					Info: &forms.Info{
						Description: description,
					},
					UpdateMask: "description",
				},
			}},
		}).Do()
		// Ignore error for description, continue with questions
	}

	// Add each question - add one at a time to handle index properly
	for i, q := range questions {
		item := &forms.Item{
			Title: q.Title,
		}

		switch q.Type {
		case "text", "short_answer":
			item.QuestionItem = &forms.QuestionItem{
				Question: &forms.Question{
					Required: q.Required,
					TextQuestion: &forms.TextQuestion{
						Paragraph: false,
					},
				},
			}
		case "paragraph", "long_answer":
			item.QuestionItem = &forms.QuestionItem{
				Question: &forms.Question{
					Required: q.Required,
					TextQuestion: &forms.TextQuestion{
						Paragraph: true,
					},
				},
			}
		case "multiple_choice", "radio":
			var options []*forms.Option
			for _, opt := range q.Options {
				options = append(options, &forms.Option{Value: opt})
			}
			item.QuestionItem = &forms.QuestionItem{
				Question: &forms.Question{
					Required: q.Required,
					ChoiceQuestion: &forms.ChoiceQuestion{
						Type:    "RADIO",
						Options: options,
					},
				},
			}
		case "checkbox", "checkboxes":
			var options []*forms.Option
			for _, opt := range q.Options {
				options = append(options, &forms.Option{Value: opt})
			}
			item.QuestionItem = &forms.QuestionItem{
				Question: &forms.Question{
					Required: q.Required,
					ChoiceQuestion: &forms.ChoiceQuestion{
						Type:    "CHECKBOX",
						Options: options,
					},
				},
			}
		case "dropdown":
			var options []*forms.Option
			for _, opt := range q.Options {
				options = append(options, &forms.Option{Value: opt})
			}
			item.QuestionItem = &forms.QuestionItem{
				Question: &forms.Question{
					Required: q.Required,
					ChoiceQuestion: &forms.ChoiceQuestion{
						Type:    "DROP_DOWN",
						Options: options,
					},
				},
			}
		default:
			// Default to text
			item.QuestionItem = &forms.QuestionItem{
				Question: &forms.Question{
					Required: q.Required,
					TextQuestion: &forms.TextQuestion{
						Paragraph: false,
					},
				},
			}
		}

		// Add each item one at a time
		_, err = g.forms.Forms.BatchUpdate(createdForm.FormId, &forms.BatchUpdateFormRequest{
			Requests: []*forms.Request{{
				CreateItem: &forms.CreateItemRequest{
					Item: item,
					Location: &forms.Location{
						Index:           int64(i),
						ForceSendFields: []string{"Index"},
					},
				},
			}},
		}).Do()
		if err != nil {
			return "", "", fmt.Errorf("unable to add question %d: %w", i+1, err)
		}
	}

	formURL := createdForm.ResponderUri
	editURL := fmt.Sprintf("https://docs.google.com/forms/d/%s/edit", createdForm.FormId)

	return formURL, editURL, nil
}

// GetFormResponses retrieves all responses from a Google Form
func (g *GoogleClient) GetFormResponses(ctx context.Context, formId string) ([]map[string]interface{}, error) {
	// Get form to understand questions
	form, err := g.forms.Forms.Get(formId).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to get form: %w", err)
	}

	// Build question ID to title map
	questionMap := make(map[string]string)
	for _, item := range form.Items {
		if item.QuestionItem != nil && item.QuestionItem.Question != nil {
			questionMap[item.QuestionItem.Question.QuestionId] = item.Title
		}
	}

	// Get responses
	responses, err := g.forms.Forms.Responses.List(formId).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to get responses: %w", err)
	}

	var results []map[string]interface{}
	for _, resp := range responses.Responses {
		entry := make(map[string]interface{})
		entry["respondent"] = resp.RespondentEmail
		entry["submitted"] = resp.LastSubmittedTime

		answers := make(map[string]string)
		for qID, answer := range resp.Answers {
			questionTitle := questionMap[qID]
			if questionTitle == "" {
				questionTitle = qID
			}
			if answer.TextAnswers != nil && len(answer.TextAnswers.Answers) > 0 {
				var vals []string
				for _, a := range answer.TextAnswers.Answers {
					vals = append(vals, a.Value)
				}
				answers[questionTitle] = strings.Join(vals, ", ")
			}
		}
		entry["answers"] = answers
		results = append(results, entry)
	}

	return results, nil
}
