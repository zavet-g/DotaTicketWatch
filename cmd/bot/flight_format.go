package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/artem/dotaticketwatch/internal/flights"
)

var shanghai = time.FixedZone("CST", 8*60*60)

var cityNames = map[string]string{
	"MOW": "москва",
	"LED": "петербург",
	"VVO": "владивосток",
	"OVB": "новосибирск",
}

var monthsRu = []string{
	"", "янв", "фев", "мар", "апр", "май", "июн",
	"июл", "авг", "сен", "окт", "ноя", "дек",
}

func formatFlightParts(snap *flights.Snapshot, msc *time.Location, custom bool) []string {
	if snap == nil {
		return []string{"× нет данных · попробуй позже"}
	}

	dep, _ := time.Parse("2006-01-02", snap.Departure)
	ret, _ := time.Parse("2006-01-02", snap.Return)

	label := "туда и обратно"
	if custom {
		label = "твои даты"
	}
	header := fmt.Sprintf("<b>дуга над облаками</b>\n<i>борт прямой. курс — восток. осталось решиться.</i>\n\n<code>%s → %s · %s</code>",
		formatDateRu(dep), formatDateRu(ret), label)

	parts := []string{header}

	if len(snap.Routes) == 0 {
		parts = append(parts, "· нет прямых рейсов на эти даты")
	} else {
		primary := snap.Routes[0]
		city := cityName(primary.Origin)
		var sb strings.Builder
		if primary.Price == 0 {
			sb.WriteString(fmt.Sprintf("· <b>%s → шанхай</b> — нет данных\n\n", city))
			sb.WriteString(fmt.Sprintf("  <a href=\"%s\">посмотреть →</a>", primary.Link))
		} else {
			sb.WriteString(fmt.Sprintf("· <b>%s → шанхай</b> — <b>%s ₽</b>\n\n", city, formatPrice(primary.Price)))
			sb.WriteString(fmt.Sprintf("  %s → %s · %s\n", primary.OriginAirport, primary.DestinationAirport, primary.AirlineName))
			sb.WriteString(fmt.Sprintf("  <i>прямой · %s</i>\n", formatFlightDuration(primary.DurationTo)))
			sb.WriteString(formatTimes(primary, msc))
			sb.WriteString(fmt.Sprintf("\n  <a href=\"%s\">посмотреть →</a>", primary.Link))

			for _, alt := range snap.Routes[1:] {
				if alt.Price != primary.Price || alt.Origin != primary.Origin {
					break
				}
				sb.WriteString("\n\n  <i>ещё борт · та же цена · те же даты</i>\n")
				sb.WriteString(formatTimes(alt, msc))
				sb.WriteString(fmt.Sprintf("  <a href=\"%s\">посмотреть →</a>", alt.Link))
			}
		}
		parts = append(parts, sb.String())
	}

	var footer string
	if custom {
		footer = "\n<i>цена без багажа · остальное на aviasales</i>\n<i>обновлено только что</i>"
	} else {
		ts := snap.UpdatedAt.In(msc).Format("15:04")
		footer = fmt.Sprintf("\n<i>цена без багажа · остальное на aviasales</i>\n<i>обновлено %s MSK</i>", ts)
	}
	parts = append(parts, footer)

	return parts
}

func formatTimes(r flights.Route, msc *time.Location) string {
	if r.DepartureAt.IsZero() {
		return ""
	}
	depLocal := r.DepartureAt.In(msc)
	arrLocal := r.DepartureAt.Add(time.Duration(r.DurationTo) * time.Minute).In(shanghai)

	nextDay := ""
	if arrLocal.Day() != depLocal.Day() {
		nextDay = " на след. день"
	}

	return fmt.Sprintf("  <code>вылет в %s · прилёт в %s%s</code>\n",
		depLocal.Format("15:04"), arrLocal.Format("15:04"), nextDay)
}

func cityName(iata string) string {
	if name, ok := cityNames[iata]; ok {
		return name
	}
	return iata
}

func formatFlightDuration(minutes int) string {
	if minutes <= 0 {
		return "—"
	}
	h := minutes / 60
	m := minutes % 60
	if m == 0 {
		return fmt.Sprintf("%dч", h)
	}
	return fmt.Sprintf("%dч %dм", h, m)
}

func formatPrice(p int) string {
	s := fmt.Sprintf("%d", p)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for i := len(s); i > 0; i -= 3 {
		start := i - 3
		if start < 0 {
			start = 0
		}
		parts = append([]string{s[start:i]}, parts...)
	}
	return strings.Join(parts, " ")
}

func formatDateRu(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), monthsRu[t.Month()])
}
