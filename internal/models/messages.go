package models

type SlackMessage struct {
	Channel     string                    `json:"channel"`
	Blocks      []SlackMessageBlock       `json:"blocks"`
	Attachments *[]SlackMessageAttachment `json:"attachments,omitempty"`
}

type SlackMessageBlock struct {
	Type string                 `json:"type"`
	Text *SlackMessageBlockText `json:"text,omitempty"`
}

type SlackMessageBlockText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackMessageAttachment struct {
	Color string `json:"color"`
	Text  string `json:"text"`
}
