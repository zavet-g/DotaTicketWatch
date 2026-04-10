package yuan

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	rateURL    = "https://iss.moex.com/iss/engines/currency/markets/selt/boards/CETS/securities/CNYRUB_TOM.json?iss.meta=off&iss.only=marketdata,securities&marketdata.columns=SECID,LAST,HIGH,LOW,UPDATETIME,TRADINGSTATUS&securities.columns=SECID,PREVPRICE,PREVDATE"
	candlesURL = "https://iss.moex.com/iss/engines/currency/markets/selt/boards/CETS/securities/CNYRUB_TOM/candles.json?iss.meta=off&interval=24"
)

type Rate struct {
	Last       float64
	High       float64
	Low        float64
	UpdateTime string
	Trading    bool
	PrevPrice  float64
	PrevDate   string
}

type Candle struct {
	Date  string
	Close float64
}

type Snapshot struct {
	Rate      Rate
	Candles   []Candle
	UpdatedAt time.Time
}

type Cache struct {
	mu       sync.RWMutex
	snapshot *Snapshot
	moscow   *time.Location
	http     *http.Client
}

func NewCache(moscow *time.Location) *Cache {
	return &Cache{
		moscow: moscow,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Cache) Start(ctx context.Context) {
	c.refresh()
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.isTradingHours() {
				c.refresh()
			}
		}
	}
}

func (c *Cache) Get() *Snapshot {
	c.mu.RLock()
	snap := c.snapshot
	c.mu.RUnlock()
	if snap != nil {
		return snap
	}
	c.refresh()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *Cache) isTradingHours() bool {
	now := time.Now().In(c.moscow)
	wd := now.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	hour := now.Hour()
	return hour >= 10 && hour < 19
}

func (c *Cache) refresh() {
	rate, err := c.fetchRate()
	if err != nil {
		slog.Warn("yuan rate fetch failed", "err", err)
		return
	}

	candles, err := c.fetchCandles()
	if err != nil {
		slog.Warn("yuan candles fetch failed", "err", err)
	}

	snap := &Snapshot{
		Rate:      *rate,
		Candles:   candles,
		UpdatedAt: time.Now(),
	}
	c.mu.Lock()
	c.snapshot = snap
	c.mu.Unlock()
	slog.Info("yuan cache updated", "last", rate.Last, "prev", rate.PrevPrice, "trading", rate.Trading)
}

func (c *Cache) fetchRate() (*Rate, error) {
	resp, err := c.http.Get(rateURL)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var raw struct {
		Marketdata struct {
			Data [][]interface{} `json:"data"`
		} `json:"marketdata"`
		Securities struct {
			Data [][]interface{} `json:"data"`
		} `json:"securities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	if len(raw.Marketdata.Data) == 0 || len(raw.Securities.Data) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	md := raw.Marketdata.Data[0]
	sec := raw.Securities.Data[0]
	rate := &Rate{}

	if len(md) >= 6 {
		rate.Last, _ = toFloat64(md[1])
		rate.High, _ = toFloat64(md[2])
		rate.Low, _ = toFloat64(md[3])
		rate.UpdateTime, _ = md[4].(string)
		if status, ok := md[5].(string); ok {
			rate.Trading = status == "T"
		}
	}

	if len(sec) >= 3 {
		rate.PrevPrice, _ = toFloat64(sec[1])
		rate.PrevDate, _ = sec[2].(string)
	}

	return rate, nil
}

func (c *Cache) fetchCandles() ([]Candle, error) {
	now := time.Now().In(c.moscow)
	from := now.AddDate(0, 0, -10).Format("2006-01-02")
	till := now.Format("2006-01-02")
	url := fmt.Sprintf("%s&from=%s&till=%s", candlesURL, from, till)

	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var raw struct {
		Candles struct {
			Data [][]interface{} `json:"data"`
		} `json:"candles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	var candles []Candle
	for _, row := range raw.Candles.Data {
		if len(row) < 7 {
			continue
		}
		closePrice, ok := toFloat64(row[1])
		if !ok {
			continue
		}
		begin, ok := row[6].(string)
		if !ok {
			continue
		}
		if len(begin) >= 10 {
			begin = begin[:10]
		}
		candles = append(candles, Candle{Date: begin, Close: closePrice})
	}
	return candles, nil
}

func toFloat64(v interface{}) (float64, bool) {
	if v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}
