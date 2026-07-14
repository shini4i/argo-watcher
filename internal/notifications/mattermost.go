package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

// MattermostStrategy posts deployment notifications via the Mattermost REST API.
// The deployment start produces a root channel post; the deployment result is
// posted as a thread reply to it, mentioning the task author.
type MattermostStrategy struct {
	baseURL       string
	token         string
	channelID     string
	mentionAuthor bool
	client        HTTPClient
	template      *template.Template

	mu        sync.Mutex
	rootPosts map[string]string // task.Id -> mattermost post id
}

type mattermostPostRequest struct {
	ChannelId string `json:"channel_id"`
	Message   string `json:"message"`
	RootId    string `json:"root_id,omitempty"`
}

type mattermostPostResponse struct {
	Id string `json:"id"`
}

// NewMattermostStrategy creates and initializes the Mattermost strategy.
func NewMattermostStrategy(cfg *config.MattermostConfig, client HTTPClient) (*MattermostStrategy, error) {
	if cfg == nil {
		return nil, errors.New("mattermost configuration cannot be nil")
	}
	if !cfg.Enabled {
		return nil, errors.New("mattermost strategy disabled")
	}
	if strings.TrimSpace(cfg.Url) == "" {
		return nil, errors.New("mattermost url cannot be empty")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("mattermost token cannot be empty")
	}
	if strings.TrimSpace(cfg.ChannelId) == "" {
		return nil, errors.New("mattermost channel id cannot be empty")
	}
	if strings.TrimSpace(cfg.Format) == "" {
		return nil, errors.New("mattermost format cannot be empty")
	}
	if client == nil {
		return nil, errors.New("HTTPClient cannot be nil")
	}

	tmpl, err := template.New("mattermost").Parse(cfg.Format)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mattermost template: %w", err)
	}

	return &MattermostStrategy{
		baseURL:       strings.TrimSuffix(cfg.Url, "/"),
		token:         cfg.Token,
		channelID:     cfg.ChannelId,
		mentionAuthor: cfg.MentionAuthor,
		client:        client,
		template:      tmpl,
		rootPosts:     make(map[string]string),
	}, nil
}

// Send delivers the Mattermost notification for the provided task.
// A task with the "in progress" status creates a root post whose id is
// remembered; any other status replies in that post's thread and forgets it.
func (s *MattermostStrategy) Send(task models.Task) error {
	var message bytes.Buffer
	if err := s.template.Execute(&message, task); err != nil {
		return fmt.Errorf("failed to execute mattermost template: %w", err)
	}

	text := message.String()
	if s.mentionAuthor && task.Author != "" {
		// mention must live in the message field: mentions inside attachments do not notify
		text = "@" + task.Author + " " + text
	}

	if task.Status == models.StatusInProgressMessage {
		postId, err := s.createPost(mattermostPostRequest{
			ChannelId: s.channelID,
			Message:   text,
		})
		if err != nil {
			return err
		}

		s.mu.Lock()
		s.rootPosts[task.Id] = postId
		s.mu.Unlock()

		return nil
	}

	s.mu.Lock()
	rootId := s.rootPosts[task.Id]
	delete(s.rootPosts, task.Id)
	s.mu.Unlock()

	// empty rootId (e.g. watcher restarted mid-deployment) degrades to a regular channel post
	_, err := s.createPost(mattermostPostRequest{
		ChannelId: s.channelID,
		Message:   text,
		RootId:    rootId,
	})
	return err
}

// createPost sends POST /api/v4/posts and returns the created post id.
func (s *MattermostStrategy) createPost(post mattermostPostRequest) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	payload, err := json.Marshal(post)
	if err != nil {
		return "", fmt.Errorf("failed to marshal mattermost post: %w", err)
	}

	slog.Debug(fmt.Sprintf("Sending mattermost post: %s", string(payload)))

	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/api/v4/posts", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create mattermost request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send mattermost post: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Failed to close mattermost response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusCreated {
		lr := io.LimitReader(resp.Body, maxErrorBodySize)
		body, readErr := io.ReadAll(lr)
		if readErr != nil {
			return "", fmt.Errorf("mattermost returned status code %d, and failed to read response body: %w", resp.StatusCode, readErr)
		}
		return "", fmt.Errorf("mattermost returned status code %d: %s", resp.StatusCode, string(body))
	}

	var created mattermostPostResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("failed to decode mattermost response: %w", err)
	}
	if created.Id == "" {
		return "", errors.New("mattermost response is missing post id")
	}

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		slog.Warn("Failed to discard mattermost response body", "error", err)
	}

	return created.Id, nil
}
