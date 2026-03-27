package monitor

const (
	EventTypeSale         = "sale"
	EventTypeAnnouncement = "announcement"
)

type Event struct {
	ID        string
	Title     string
	URL       string
	Source    string
	EventType string
}

type Monitor interface {
	Name() string
	Check() ([]Event, error)
}
