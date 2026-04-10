package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

const apiURL = "https://api.open-meteo.com/v1/forecast?latitude=31.1682&longitude=121.4728&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max,weather_code&hourly=temperature_2m,precipitation_probability,weather_code&timezone=Asia/Shanghai&forecast_days=10"

type DayForecast struct {
	Date        string
	TempMax     float64
	TempMin     float64
	PrecipProb  int
	WeatherCode int
}

type HourlyPeriod struct {
	Name        string
	Temp        int
	PrecipProb  int
	WeatherCode int
}

type Snapshot struct {
	Days      []DayForecast
	Today     []HourlyPeriod
	UpdatedAt time.Time
}

type Cache struct {
	mu       sync.RWMutex
	snapshot *Snapshot
	http     *http.Client
}

func NewCache() *Cache {
	return &Cache{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Cache) Start(ctx context.Context) {
	c.refresh()
	ticker := time.NewTicker(3 * time.Hour)
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

func (c *Cache) refresh() {
	snap, err := c.fetch()
	if err != nil {
		slog.Warn("weather fetch failed", "err", err)
		return
	}
	c.mu.Lock()
	c.snapshot = snap
	c.mu.Unlock()
	slog.Info("weather cache updated", "days", len(snap.Days), "today_periods", len(snap.Today))
}

func (c *Cache) fetch() (*Snapshot, error) {
	resp, err := c.http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var raw struct {
		Daily struct {
			Time        []string  `json:"time"`
			TempMax     []float64 `json:"temperature_2m_max"`
			TempMin     []float64 `json:"temperature_2m_min"`
			PrecipProb  []int     `json:"precipitation_probability_max"`
			WeatherCode []int     `json:"weather_code"`
		} `json:"daily"`
		Hourly struct {
			Time        []string  `json:"time"`
			Temp        []float64 `json:"temperature_2m"`
			PrecipProb  []int     `json:"precipitation_probability"`
			WeatherCode []int     `json:"weather_code"`
		} `json:"hourly"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	n := len(raw.Daily.Time)
	if n == 0 {
		return nil, fmt.Errorf("empty response")
	}
	if len(raw.Daily.TempMax) != n || len(raw.Daily.TempMin) != n ||
		len(raw.Daily.PrecipProb) != n || len(raw.Daily.WeatherCode) != n {
		return nil, fmt.Errorf("mismatched daily arrays")
	}

	days := make([]DayForecast, n)
	for i := 0; i < n; i++ {
		days[i] = DayForecast{
			Date:        raw.Daily.Time[i],
			TempMax:     raw.Daily.TempMax[i],
			TempMin:     raw.Daily.TempMin[i],
			PrecipProb:  raw.Daily.PrecipProb[i],
			WeatherCode: raw.Daily.WeatherCode[i],
		}
	}

	today := groupTodayPeriods(raw.Daily.Time[0], raw.Hourly.Time, raw.Hourly.Temp, raw.Hourly.PrecipProb, raw.Hourly.WeatherCode)

	return &Snapshot{
		Days:      days,
		Today:     today,
		UpdatedAt: time.Now(),
	}, nil
}

type periodDef struct {
	name  string
	start int
	end   int
}

var periodDefs = []periodDef{
	{"утро", 6, 12},
	{"день", 12, 18},
	{"вечер", 18, 24},
	{"ночь", 0, 6},
}

func groupTodayPeriods(todayDate string, times []string, temps []float64, precips []int, codes []int) []HourlyPeriod {
	prefix := todayDate + "T"
	n := len(times)

	type hourData struct {
		hour   int
		temp   float64
		precip int
		code   int
	}

	var hours []hourData
	for i := 0; i < n; i++ {
		if !strings.HasPrefix(times[i], prefix) {
			if len(hours) > 0 {
				break
			}
			continue
		}
		t, err := time.Parse("2006-01-02T15:04", times[i])
		if err != nil {
			continue
		}
		hours = append(hours, hourData{
			hour:   t.Hour(),
			temp:   temps[i],
			precip: precips[i],
			code:   codes[i],
		})
	}

	var result []HourlyPeriod
	for _, pd := range periodDefs {
		var sumTemp float64
		var count int
		maxPrecip := 0
		maxCode := 0
		for _, h := range hours {
			if h.hour >= pd.start && h.hour < pd.end {
				sumTemp += h.temp
				count++
				if h.precip > maxPrecip {
					maxPrecip = h.precip
				}
				if h.code > maxCode {
					maxCode = h.code
				}
			}
		}
		if count == 0 {
			continue
		}
		result = append(result, HourlyPeriod{
			Name:        pd.name,
			Temp:        int(math.Round(sumTemp / float64(count))),
			PrecipProb:  maxPrecip,
			WeatherCode: maxCode,
		})
	}
	return result
}
