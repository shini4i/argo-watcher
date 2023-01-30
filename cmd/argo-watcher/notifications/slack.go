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
						Text: fmt.Sprintf("*Application:*\n%s", task.App),
					},
					{
						Type: "mrkdwn",
						Text: fmt.Sprintf("*Version:*\n%s", "placeholder"),
					},
					{
						Type: "mrkdwn",
						Text: fmt.Sprintf("*Status:*\n%s", status),
					},
					{
						Type: "mrkdwn",
						Text: fmt.Sprintf("*Duration:*\n%s", "placeholder"),
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
