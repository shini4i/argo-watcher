package notifications

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"text/template"

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
	receiverURL, err := validateReceiverURL("mattermost", cfg.Url)
	if err != nil {
		return nil, err
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
		baseURL:       strings.TrimSuffix(receiverURL, "/"),
		token:         cfg.Token,
		channelID:     cfg.ChannelId,
		mentionAuthor: cfg.MentionAuthor,
		client:        client,
		template:      tmpl,
		rootPosts:     make(map[string]string),
	}, nil
}

// Send delivers the Mattermost notification for the provided task.
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

func (s *MattermostStrategy) createPost(post mattermostPostRequest) (string, error) {
	payload, err := json.Marshal(post)
	if err != nil {
		return "", fmt.Errorf("failed to marshal mattermost post: %w", err)
	}

	slog.Debug("Sending mattermost post", "payload", string(payload))

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + s.token,
	}

	body, err := deliverPost(s.client, "mattermost", s.baseURL+"/api/v4/posts", headers, payload, []int{http.StatusCreated})
	if err != nil {
		return "", err
	}

	var created mattermostPostResponse
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("failed to decode mattermost response: %w", err)
	}
	if created.Id == "" {
		return "", errors.New("mattermost response is missing post id")
	}

	return created.Id, nil
}
