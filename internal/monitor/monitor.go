package monitor

type Event struct {
	ID      string
	Title   string
	URL     string
	Source  string
}

type Monitor interface {
	Name() string
	Check() ([]Event, error)
}
