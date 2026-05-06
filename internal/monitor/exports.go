package monitor

func IsTicketNewsExported(title, contents string) bool {
	return isTicketNews(title, contents)
}

func ExtractAXSEventsExported(html string) ([]Event, error) {
	return extractAXSEvents(html)
}

func PruneAXSSnapshotExported(html string) ([]byte, error) {
	return pruneAXSSnapshot(html)
}

func (m *SteamNewsMonitor) FetchItems() ([]SteamNewsItem, error) {
	items, err := m.fetch()
	if err != nil {
		return nil, err
	}
	out := make([]SteamNewsItem, len(items))
	for i, it := range items {
		out[i] = SteamNewsItem{
			GID:      it.GID,
			Title:    it.Title,
			URL:      it.URL,
			Contents: it.Contents,
		}
	}
	return out, nil
}

type SteamNewsItem struct {
	GID      string
	Title    string
	URL      string
	Contents string
}
