package notifier

import "github.com/artem/dotaticketwatch/internal/monitor"

type Notifier interface {
	Notify(event monitor.Event) error
	NotifyText(text string) error
}
