package flights

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	DefaultDeparture = "2026-08-12"
	DefaultReturn    = "2026-08-24"
)

type Cache struct {
	client   *Client
	origins  []string
	interval time.Duration

	mu       sync.RWMutex
	snapshot *Snapshot
}

func NewCache(client *Client, origins []string, intervalMin int) *Cache {
	return &Cache{
		client:   client,
		origins:  origins,
		interval: time.Duration(intervalMin) * time.Minute,
	}
}

func (c *Cache) Start(ctx context.Context) {
	c.refresh()
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh()
		}
	}
}

func (c *Cache) Get() *Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *Cache) FetchCustom(departure, ret string) *Snapshot {
	routes := c.fetchAll(departure, ret)
	return &Snapshot{
		Routes:    routes,
		UpdatedAt: time.Now(),
		Departure: departure,
		Return:    ret,
	}
}

func (c *Cache) refresh() {
	routes := c.fetchAll(DefaultDeparture, DefaultReturn)
	snap := &Snapshot{
		Routes:    routes,
		UpdatedAt: time.Now(),
		Departure: DefaultDeparture,
		Return:    DefaultReturn,
	}
	c.mu.Lock()
	c.snapshot = snap
	c.mu.Unlock()
	slog.Info("flights cache updated", "routes", len(routes))
}

func (c *Cache) fetchAll(departure, ret string) []Route {
	var routes []Route
	for _, origin := range c.origins {
		fetched, err := c.client.FetchDirect(origin, departure, ret)
		if err != nil {
			slog.Warn("flights fetch failed", "origin", origin, "err", err)
			continue
		}
		if len(fetched) > 0 {
			routes = append(routes, fetched...)
		} else {
			routes = append(routes, Route{
				Origin: origin,
				Link:   c.client.FallbackLink(origin, departure, ret),
			})
		}
	}
	return routes
}
