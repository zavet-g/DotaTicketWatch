package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/artem/dotaticketwatch/internal/fetcher"
	"github.com/artem/dotaticketwatch/internal/monitor"
	"github.com/artem/dotaticketwatch/internal/storage"
)

var nextDataRe = regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">`)

const (
	steamNewsURL = "https://api.steampowered.com/ISteamNews/GetNewsForApp/v0002/?appid=570&count=10&format=json"
	axsHubURL    = "https://www.axs.com/teams/1119906/the-international-dota-2-tickets"
	dbPath       = "/tmp/dotaticketwatch-check.db"
)

var flareSolverrURL = os.Getenv("FLARESOLVERR_URL")

func main() {
	passed, failed := 0, 0

	run := func(name string, fn func() error) {
		fmt.Printf("\n── %s\n", name)
		start := time.Now()
		if err := fn(); err != nil {
			fmt.Printf("   FAIL  %v (%.2fs)\n", err, time.Since(start).Seconds())
			failed++
		} else {
			fmt.Printf("   PASS  (%.2fs)\n", time.Since(start).Seconds())
			passed++
		}
	}

	info := func(name string, fn func()) {
		fmt.Printf("\n── %s\n", name)
		start := time.Now()
		fn()
		fmt.Printf("   done  (%.2fs)\n", time.Since(start).Seconds())
	}

	fmt.Println("=== DotaTicketWatch backend check ===")

	run("Storage: add/remove subscriber + deduplication", checkStorage)
	run("Steam News API: real HTTP GET", checkSteamNews)
	info("AXS Level 1 (tls-client) — Cloudflare probe", probeAXSTLSClient)
	info("AXS Level 2 (curl) — Cloudflare probe", probeAXSCurl)
	run("AXS fetcher: full cascade (L1 → L2 → L3/FlareSolverr)", checkAXSCascade)
	run("AXS monitor: parse event IDs from hub page", checkAXSMonitor)

	fmt.Printf("\n=== Results: %d passed, %d failed ===\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func checkStorage() error {
	os.Remove(dbPath)
	defer os.Remove(dbPath)

	s, err := storage.New(dbPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer s.Close()

	if err := s.AddSubscriber(42, "testuser"); err != nil {
		return fmt.Errorf("AddSubscriber: %w", err)
	}
	if !s.IsSubscribed(42) {
		return fmt.Errorf("IsSubscribed returned false after add")
	}
	if err := s.RemoveSubscriber(42); err != nil {
		return fmt.Errorf("RemoveSubscriber: %w", err)
	}
	if s.IsSubscribed(42) {
		return fmt.Errorf("IsSubscribed returned true after remove")
	}
	if err := s.MarkNotified("axs-916193"); err != nil {
		return fmt.Errorf("MarkNotified: %w", err)
	}
	if !s.AlreadyNotified("axs-916193") {
		return fmt.Errorf("AlreadyNotified returned false after mark")
	}
	if s.AlreadyNotified("axs-000000") {
		return fmt.Errorf("AlreadyNotified returned true for unknown ID")
	}
	return nil
}

func checkSteamNews() error {
	m := monitor.NewSteamNewsMonitor(steamNewsURL)
	events, err := m.Check()
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Println("   no ticket-related news yet")
	}
	for _, e := range events {
		fmt.Printf("   → MATCH  [%s] %s\n      %s\n", e.ID, e.Title, e.URL)
	}
	return nil
}

func probeAXSTLSClient() {
	html, err := fetcher.FetchLevel1(axsHubURL)
	if err != nil {
		fmt.Printf("   blocked (expected): %v\n", err)
		return
	}
	fmt.Printf("   passed! response size: %d bytes\n", len(html))
}

func probeAXSCurl() {
	html, err := fetcher.FetchLevel2(axsHubURL)
	if err != nil {
		fmt.Printf("   blocked (expected): %v\n", err)
		return
	}
	fmt.Printf("   passed! response size: %d bytes\n", len(html))
}

func checkAXSCascade() error {
	html, err := fetcher.Fetch(axsHubURL, flareSolverrURL)
	if err != nil {
		return fmt.Errorf("all levels failed: %w", err)
	}
	fmt.Printf("   response size: %d bytes\n", len(html))
	if nextDataRe.MatchString(html) {
		fmt.Println("   __NEXT_DATA__ found ✓")
	} else {
		lower := strings.ToLower(html)
		if strings.Contains(lower, "queueit-overlay") || strings.Contains(lower, "inqueue.queue-it.net") {
			fmt.Println("   WARNING: __NEXT_DATA__ absent — Queue-it waitroom active!")
		} else {
			fmt.Println("   WARNING: __NEXT_DATA__ absent — unexpected page structure!")
		}
	}
	return nil
}

func checkAXSMonitor() error {
	m := monitor.NewAXSMonitor(axsHubURL, flareSolverrURL, fetcher.Fetch)
	events, err := m.Check()
	if err != nil {
		return err
	}
	fmt.Printf("   found %d new AXS event(s)\n", len(events))
	for _, e := range events {
		fmt.Printf("   → [%s] %s\n   %s\n", e.ID, e.Title, e.URL)
	}
	return nil
}
