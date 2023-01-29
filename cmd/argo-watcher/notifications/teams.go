package notifications

import (
	"fmt"
	goteamsnotify "github.com/atc0005/go-teams-notify/v2"
	"github.com/atc0005/go-teams-notify/v2/adaptivecard"
	"github.com/romana/rlog"
	m "github.com/shini4i/argo-watcher/internal/models"
)

type Teams struct {
	WebhookUrl string
	client     *goteamsnotify.TeamsClient
}

func (t *Teams) Init(channel string) {
	t.WebhookUrl = channel
	t.client = goteamsnotify.NewTeamsClient()
}

func (t *Teams) Send(task m.Task, status string) (bool, error) {
	msgTitle := fmt.Sprintf("Argo Watcher: %s status update", task.App)

	msg, err := adaptivecard.NewSimpleMessage(status, msgTitle, true)
	if err != nil {
		rlog.Warnf("Failed to create message: %s", err.Error())
		return false, err
	}

	if err := t.client.Send(t.WebhookUrl, msg); err != nil {
		rlog.Warnf("Failed to send message: %s", err.Error())
		return false, err
	}

	return true, nil
}
