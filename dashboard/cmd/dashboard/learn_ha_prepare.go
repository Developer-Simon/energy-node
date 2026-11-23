package main

import (
	"context"
	"sort"
	"time"
)

// haPrefetchDays ist die Zahl der Tage vor heute, die der Vorlauf prueft.
const haPrefetchDays = 14

// haPrefetchTimeout ist die Frist des Vorlaufs. Antworten danach zaehlen nicht.
const haPrefetchTimeout = 90 * time.Second

// haFetcher ist der Teil des Abgleichs, den der Vorlauf braucht.
type haFetcher interface {
	Offered() bool
	Prefetch(keys []string, days []string, deadline time.Time)
	WaitIdle(ctx context.Context) bool
	Drop()
}

// ownMinutes ist der Teil des eigenen Minutenspeichers, den der Vorlauf liest.
type ownMinutes interface {
	Keys(from, to time.Time) []string
	Coverage(from, to time.Time) int
}

// haPreparer holt vor dem schweren Lauf die Minuten nach, die der eigene
// Speicher in den letzten Tagen nicht lueckenlos hat. Nach dem Lauf verwirft
// er den Zwischenspeicher.
type haPreparer struct {
	own ownMinutes
	ha  haFetcher
	// active meldet, ob der Abgleich laeuft und der Companion gekoppelt ist.
	active func() bool
	loc    *time.Location
}

// Prepare plant die Nachfragen fuer die Tage ohne lueckenlose Minuten und
// wartet, bis alle Antworten da sind oder die Frist vorbei ist.
func (p *haPreparer) Prepare(now time.Time) {
	if p.active == nil || !p.active() {
		return
	}
	loc := p.loc
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	from := today.AddDate(0, 0, -haPrefetchDays)

	keySet := map[string]bool{"b:pv": true, "b:battery_soc": true}
	for _, key := range p.own.Keys(from, today) {
		keySet[key] = true
	}
	var days []string
	for k := haPrefetchDays; k >= 1; k-- {
		start := today.AddDate(0, 0, -k)
		if p.own.Coverage(start, start.AddDate(0, 0, 1)) != 1 {
			days = append(days, start.Format("2006-01-02"))
		}
	}
	if len(days) == 0 || !p.ha.Offered() {
		return
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	p.ha.Prefetch(keys, days, now.Add(haPrefetchTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), haPrefetchTimeout)
	defer cancel()
	p.ha.WaitIdle(ctx)
}

// Release verwirft den Zwischenspeicher, auch wenn der Lauf fehlschlug.
func (p *haPreparer) Release() {
	p.ha.Drop()
}
