package models

type SlackMessage struct {
	Channel string              `json:"channel"`
	Blocks  []SlackMessageBlock `json:"blocks"`
}

type SlackMessageBlock struct {
	Type     string                       `json:"type"`
	Text     *SlackMessageBlockText       `json:"text,omitempty"`
	Elements *[]SlackMessageBlockElements `json:"elements,omitempty"`
	Fields   *[]SlackMessageSectionFields `json:"fields,omitempty"`
}

type SlackMessageBlockText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackMessageSection struct {
	Type   string                      `json:"type"`
	Fields []SlackMessageSectionFields `json:"fields"`
}

type SlackMessageSectionFields struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackMessageBlockElements struct {
	Type  string                        `json:"type"`
	Text  SlackMessageBlockElementsText `json:"text"`
	Value string                        `json:"value"`
	Url   string                        `json:"url"`
}

type SlackMessageBlockElementsText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
