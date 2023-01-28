package notifications

type Notification interface {
	Init(channel string)
	Send(app string, message string) (bool, error)
	SendToCustomChannel(app string, channel string, message string) (bool, error)
}
