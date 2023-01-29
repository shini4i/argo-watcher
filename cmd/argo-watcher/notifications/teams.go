package notifications

import (
	"fmt"
	goteamsnotify "github.com/atc0005/go-teams-notify/v2"
	"github.com/atc0005/go-teams-notify/v2/adaptivecard"
	"github.com/romana/rlog"
)

type Teams struct {
	WebhookUrl string
	client     *goteamsnotify.TeamsClient
}

func (t *Teams) Init(channel string) {
	t.WebhookUrl = channel
	t.client = goteamsnotify.NewTeamsClient()
}

func (t *Teams) Send(app string, message string) (bool, error) {
	msgTitle := fmt.Sprintf("Argo Watcher: %s status update", app)

	msg, err := adaptivecard.NewSimpleMessage(message, msgTitle, true)
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

func (t *Teams) SendToCustomChannel(app string, channel string, message string) (bool, error) {
	msgTitle := fmt.Sprintf("Argo Watcher: %s status update", app)

	msg, err := adaptivecard.NewSimpleMessage(message, msgTitle, true)
	if err != nil {
		rlog.Warnf("Failed to create message: %s", err.Error())
		return false, err
	}

	if err := t.client.Send(channel, msg); err != nil {
		rlog.Printf("Failed to send message: %s", err.Error())
		return false, err
	}

	return true, nil
}
