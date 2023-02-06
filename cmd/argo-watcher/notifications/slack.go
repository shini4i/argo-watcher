package notifications

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/romana/rlog"
	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
	"net/http"
	"os"
)

type Slack struct {
	Channel        string
	token          string
	argoWatcherUrl string
	client         *http.Client
}

const (
	slackApiUrl = "https://slack.com/api/chat.postMessage"
)

func (s *Slack) Init(channel string) {
	s.client = &http.Client{}
	s.Channel = channel
	s.argoWatcherUrl = h.GetEnv("ARGO_WATCHER_URL", "http://localhost:8080")
	s.token = os.Getenv("SLACK_TOKEN")
	if s.token == "" {
		panic("SLACK_TOKEN is not set")
	}
}

func (s *Slack) Send(task m.Task, status string) (bool, error) {
	// Making an educated guess here that we don't expect different tags for different images in the same task
	// If you do, please open an issue and describe your use case
	imageTag := task.Images[0].Tag

	statusIcons := map[string]string{
		"success": ":white_check_mark:",
		"failed":  ":x:",
	}

	msg := m.SlackMessage{
		Channel: s.Channel,
		Blocks: []m.SlackMessageBlock{
			{
				Type: "header",
				Text: &m.SlackMessageBlockText{
					Type: "plain_text",
					Text: "Deployment Status Notification",
				},
			},
			{
				Type: "section",
				Fields: &[]m.SlackMessageSectionFields{
					{
						Type: "mrkdwn",
						Text: fmt.Sprintf("*Application:*\n%s", ":mega: "+task.App),
					},
					{
						Type: "mrkdwn",
						Text: fmt.Sprintf("*Version:*\n%s", ":clap: "+imageTag),
					},
					{
						Type: "mrkdwn",
						Text: fmt.Sprintf("*Status:*\n%s", statusIcons[status]+" "+status),
					},
					{
						Type: "mrkdwn",
						Text: fmt.Sprintf("*Duration:*\n%s", ":clock1: placeholder"),
					},
				},
			},
			{
				Type: "actions",
				Elements: &[]m.SlackMessageBlockElements{
					{
						Type: "button",
						Text: m.SlackMessageBlockElementsText{
							Type: "plain_text",
							Text: "View Task",
						},
						Value: "view_task",
						Url:   s.argoWatcherUrl + "/task/" + task.Id,
					},
					{
						Type: "button",
						Text: m.SlackMessageBlockElementsText{
							Type: "plain_text",
							Text: "View Application",
						},
						Value: "view_app",
						Url:   s.argoWatcherUrl + "/app/" + task.App,
					},
				},
			},
		},
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return false, err
	}

	rlog.Debugf("Sending the following payload: %s", string(body))
	req, err := http.NewRequest("POST", slackApiUrl, bytes.NewBuffer(body))
	if err != nil {
		return false, err
	}

	req.Header.Add("Authorization", "Bearer "+s.token)
	req.Header.Add("Content-Type", "application/json; charset=utf-8")

	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}

	if resp.StatusCode != 200 {
		rlog.Info(resp.StatusCode)
		return false, fmt.Errorf("slack api returned status code %d", resp.StatusCode)
	}
	return true, nil
}
