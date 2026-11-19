package autolearn

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const (
	// checkerDocEvery is how long the rule document is kept before it is read again.
	checkerDocEvery = time.Minute
	// checkerMaxFindings caps the findings the check reports.
	checkerMaxFindings = 20
)

// Checker runs the static and observed checks on the rule document and keeps
// the findings the user chose to ignore. All methods are safe for concurrent use.
type Checker struct {
	obs          *Observer
	season       SeasonalSource
	readDoc      func() ([]byte, error)
	entityExists func(string) bool
	ignoredPath  string
	logf         func(string, ...any)

	mu sync.Mutex
	// doc is the last rule document that parsed. docOK is false when the last
	// read failed, which keeps the ignored IDs and the observations as they are.
	doc    Doc
	docOK  bool
	read   bool
	docAt  time.Time
	errMsg string
	// ignored is the set of ignored finding IDs, stored in ignoredPath.
	ignored map[string]bool
}

// NewChecker builds a checker. readDoc returns the rule document, entityExists
// may be nil, ignoredPath may be empty to keep the ignored IDs in memory only,
// and logf may be nil for log.Printf. A broken ignored file starts empty.
func NewChecker(obs *Observer, season SeasonalSource, readDoc func() ([]byte, error), entityExists func(string) bool, ignoredPath string, logf func(string, ...any)) *Checker {
	if logf == nil {
		logf = log.Printf
	}
	if obs == nil {
		obs = NewObserver(time.Local)
	}
	return &Checker{
		obs:          obs,
		season:       season,
		readDoc:      readDoc,
		entityExists: entityExists,
		ignoredPath:  ignoredPath,
		logf:         logf,
		ignored:      loadIgnored(ignoredPath, logf),
	}
}

// Findings returns the static and observed findings without the ignored ones,
// ordered by severity and ID, at most 20. Ignored IDs of rules that no longer
// exist are removed from the ignored file here.
func (c *Checker) Findings(now time.Time) []Finding {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.refreshLocked(now)
	all := StaticFindings(c.doc, c.entityExists)
	all = append(all, ObservedFindings(c.doc, c.obs.Snapshot(), c.season, now)...)
	sort.SliceStable(all, func(i, j int) bool {
		ri, rj := severityRank[all[i].Severity], severityRank[all[j].Severity]
		if ri != rj {
			return ri < rj
		}
		return all[i].ID < all[j].ID
	})

	if c.docOK && c.dropOrphansLocked(all) {
		if err := c.saveIgnoredLocked(); err != nil {
			c.logf("autolearn: save %s: %v", c.ignoredPath, err)
		}
	}

	out := []Finding{}
	for _, f := range all {
		if c.ignored[f.ID] {
			continue
		}
		out = append(out, f)
		if len(out) == checkerMaxFindings {
			break
		}
	}
	return out
}

// Ignore hides the finding with the given ID until Unignore is called.
func (c *Checker) Ignore(id string) error {
	return c.setIgnored(id, true)
}

// Unignore shows the finding with the given ID again.
func (c *Checker) Unignore(id string) error {
	return c.setIgnored(id, false)
}

func (c *Checker) setIgnored(id string, ignore bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ignored[id] == ignore {
		return nil
	}
	if ignore {
		c.ignored[id] = true
	} else {
		delete(c.ignored, id)
	}
	if err := c.saveIgnoredLocked(); err != nil {
		// Roll back, so the memory matches the file.
		if ignore {
			delete(c.ignored, id)
		} else {
			c.ignored[id] = true
		}
		return err
	}
	return nil
}

// ObserveTick feeds the observer with one tick of the energy node. statePayload
// is the state of the rule engine with the trigger count per rule, balance the
// balance values by field, and entityValues reads an entity value. Only the
// values the current rules read are collected. The rule document is read at
// most once a minute, and the observations of rules that are gone are pruned.
func (c *Checker) ObserveTick(now time.Time, statePayload string, balance map[string]float64, entityValues func(entityID string) (float64, bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.refreshLocked(now)
	c.obs.ObserveTriggers(now, triggerCounts(statePayload))
	if !c.docOK {
		// Without a document nothing is known about the rules, so nothing is pruned.
		return
	}

	keepRules := map[string]bool{}
	keepKeys := map[string]bool{}
	values := map[string]float64{}
	for _, rule := range c.doc.Rules {
		keepRules[rule.ID] = true
		for _, cond := range rule.Conditions {
			key := cond.ValueKey()
			if key == "" {
				continue
			}
			keepKeys[key] = true
			switch {
			case strings.HasPrefix(key, "balance:"):
				if v, ok := balance[cond.Field]; ok {
					values[key] = v
				}
			case strings.HasPrefix(key, "entity:"):
				if entityValues == nil {
					continue
				}
				if v, ok := entityValues(cond.EntityID); ok {
					values[key] = v
				}
			}
		}
	}
	c.obs.ObserveValues(now, values)
	c.obs.Prune(keepRules, keepKeys)
}

// refreshLocked reads and parses the rule document if it is older than
// checkerDocEvery. A failed read logs once per distinct error.
func (c *Checker) refreshLocked(now time.Time) {
	if c.read && !now.Before(c.docAt) && now.Sub(c.docAt) < checkerDocEvery {
		return
	}
	c.read, c.docAt = true, now
	c.doc, c.docOK = Doc{}, false

	var err error
	var raw []byte
	if c.readDoc == nil {
		err = errors.New("no rule source")
	} else {
		raw, err = c.readDoc()
	}
	var doc Doc
	if err == nil {
		doc, err = ParseDoc(raw)
	}
	if err != nil {
		if msg := err.Error(); msg != c.errMsg {
			c.errMsg = msg
			c.logf("autolearn: rule document unreadable: %v", err)
		}
		return
	}
	c.errMsg = ""
	c.doc, c.docOK = doc, true
}

// dropOrphansLocked removes ignored IDs that are neither in the unfiltered
// findings nor mention a current rule. It reports whether anything was removed.
func (c *Checker) dropOrphansLocked(all []Finding) bool {
	present := make(map[string]bool, len(all))
	for _, f := range all {
		present[f.ID] = true
	}
	current := make(map[string]bool, len(c.doc.Rules))
	for _, rule := range c.doc.Rules {
		current[rule.ID] = true
	}

	changed := false
	for id := range c.ignored {
		if present[id] || mentionsRule(id, current) {
			continue
		}
		delete(c.ignored, id)
		changed = true
	}
	return changed
}

// mentionsRule reports whether one of the colon separated parts after the kind
// of a finding ID is a current rule ID.
func mentionsRule(id string, rules map[string]bool) bool {
	parts := strings.Split(id, ":")
	for _, part := range parts[1:] {
		if rules[part] {
			return true
		}
	}
	return false
}

// saveIgnoredLocked writes the ignored IDs as a sorted JSON list, atomically.
func (c *Checker) saveIgnoredLocked() error {
	if c.ignoredPath == "" {
		return nil
	}
	ids := make([]string, 0, len(c.ignored))
	for id := range c.ignored {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return energystats.WriteFileAtomic(filepath.Dir(c.ignoredPath), c.ignoredPath, data)
}

// loadIgnored reads the ignored IDs. A missing file is an empty set, a broken
// one logs once and is an empty set too.
func loadIgnored(path string, logf func(string, ...any)) map[string]bool {
	set := map[string]bool{}
	if path == "" {
		return set
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return set
	}
	var ids []string
	if err == nil {
		err = json.Unmarshal(data, &ids)
	}
	if err != nil {
		logf("autolearn: %s unreadable, starting without ignored findings: %v", path, err)
		return map[string]bool{}
	}
	for _, id := range ids {
		if id != "" {
			set[id] = true
		}
	}
	return set
}

// triggerCounts reads trigger_count per rule from the state payload. A payload
// that does not parse gives no counts.
func triggerCounts(payload string) map[string]int {
	if payload == "" {
		return nil
	}
	var state struct {
		Rules map[string]struct {
			TriggerCount int `json:"trigger_count"`
		} `json:"rules"`
	}
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return nil
	}
	counts := make(map[string]int, len(state.Rules))
	for id, r := range state.Rules {
		counts[id] = r.TriggerCount
	}
	return counts
}
