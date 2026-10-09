// Package dataflow derives the read-only data flow of the device map: which
// device feeds which service input, and which automation rule reads or writes
// which topic. It reads the configurations and their schemas (opt-in
// annotation x-dataflow) plus a registry snapshot, per request, without a
// cache. Spec: .docs/superpowers/specs/2026-10-07-devicemap-energie-datenfluss-design.md
package dataflow

import (
	"encoding/json"
	"net/url"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

const (
	BalanceID = "balance"

	CatService    = "service"
	CatAutomation = "automation"

	ReasonNoProducer    = "no_producer"
	ReasonNoConsumer    = "no_consumer"
	ReasonKeyUnmatched  = "key_unmatched"
	ReasonConfigInvalid = "config_invalid"
)

// RuleID and ServiceID escape free text, the device map stores virtual ids
// under the pattern rule:\S+ and service:\S+.
func RuleID(id string) string { return "rule:" + url.PathEscape(id) }

func ServiceID(config, item string) string {
	if item == "" {
		return "service:" + config
	}
	return "service:" + config + "/" + url.PathEscape(item)
}

type Ref struct {
	DeviceID  string `json:"device_id,omitempty"`
	EntityID  string `json:"entity_id,omitempty"`
	VirtualID string `json:"virtual_id,omitempty"`
}

type Link struct {
	Tab    string `json:"tab"`
	Target string `json:"target,omitempty"`
	Item   string `json:"item,omitempty"`
}

type Details struct {
	Topic   string `json:"topic,omitempty"`
	JSONKey string `json:"json_key,omitempty"`
	Unit    string `json:"unit,omitempty"`
	Field   string `json:"field,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Part    string `json:"part,omitempty"`
	Index   int    `json:"index"`
	Value   string `json:"value,omitempty"`
}

type Node struct {
	VirtualID  string            `json:"virtual_id"`
	Kind       string            `json:"kind"`
	Label      string            `json:"label"`
	LabelKey   string            `json:"label_key,omitempty"`
	Enabled    *bool             `json:"enabled,omitempty"`
	Conditions []json.RawMessage `json:"conditions,omitempty"`
	Actions    []json.RawMessage `json:"actions,omitempty"`
	Link       Link              `json:"link"`
}

type Edge struct {
	ID       string  `json:"id"`
	Cat      string  `json:"cat"`
	From     Ref     `json:"from"`
	To       Ref     `json:"to"`
	Title    string  `json:"title"`
	TitleKey string  `json:"title_key,omitempty"`
	Details  Details `json:"details"`
	Link     Link    `json:"link"`
}

type Unresolved struct {
	ID        string `json:"id"`
	Config    string `json:"config"`
	Item      string `json:"item,omitempty"`
	Topic     string `json:"topic,omitempty"`
	Field     string `json:"field,omitempty"`
	Direction string `json:"direction,omitempty"`
	Reason    string `json:"reason"`
	Title     string `json:"title,omitempty"`
	TitleKey  string `json:"title_key,omitempty"`
	Target    Ref    `json:"target"`
	Link      Link   `json:"link"`
}

type Graph struct {
	Nodes      []Node       `json:"nodes"`
	Edges      []Edge       `json:"edges"`
	Unresolved []Unresolved `json:"unresolved"`
}

func emptyGraph() Graph {
	return Graph{Nodes: []Node{}, Edges: []Edge{}, Unresolved: []Unresolved{}}
}

type entityRef struct {
	device   string
	entity   string
	template string
}

// Index answers "which entity carries this topic" for one registry snapshot.
type Index struct {
	state    map[string][]entityRef
	command  map[string][]entityRef
	devices  map[string]bool
	entities map[string]string
}

func NewIndex(devices []registry.DeviceView) *Index {
	ix := &Index{state: map[string][]entityRef{}, command: map[string][]entityRef{}, devices: map[string]bool{}, entities: map[string]string{}}
	for _, device := range devices {
		ix.devices[device.ID] = true
		for _, entity := range device.Entities {
			ix.entities[entity.UniqueID] = device.ID
			ref := entityRef{device: device.ID, entity: entity.UniqueID, template: entity.ValueTemplate}
			if entity.StateTopic != "" {
				ix.state[entity.StateTopic] = append(ix.state[entity.StateTopic], ref)
			}
			if entity.CommandTopic != "" {
				ix.command[entity.CommandTopic] = append(ix.command[entity.CommandTopic], ref)
			}
		}
	}
	for _, refs := range []map[string][]entityRef{ix.state, ix.command} {
		for topic := range refs {
			sort.Slice(refs[topic], func(i, j int) bool { return refs[topic][i].entity < refs[topic][j].entity })
		}
	}
	return ix
}

// Input finds the entity a service reads when it subscribes to topic and
// takes jsonKey ("" = the bare payload). A topic several entities share
// without a matching key stays unresolved, guessing would draw a wrong edge.
func (ix *Index) Input(topic, jsonKey string) (Ref, string) {
	candidates := ix.state[topic]
	if len(candidates) == 0 {
		return Ref{}, ReasonNoProducer
	}
	for _, candidate := range candidates {
		key, ok := registry.TemplateJSONKey(candidate.template)
		if ok && key == jsonKey {
			return Ref{DeviceID: candidate.device, EntityID: candidate.entity}, ""
		}
	}
	return Ref{}, ReasonKeyUnmatched
}

// Output finds the entity that receives a publish on topic: the one whose
// command_topic it is.
func (ix *Index) Output(topic string) (Ref, string) {
	candidates := ix.command[topic]
	if len(candidates) == 0 {
		return Ref{}, ReasonNoConsumer
	}
	return Ref{DeviceID: candidates[0].device, EntityID: candidates[0].entity}, ""
}

func (ix *Index) HasDevice(id string) bool { return ix.devices[id] }

func (ix *Index) Entity(uniqueID string) (Ref, bool) {
	device, ok := ix.entities[uniqueID]
	if !ok {
		return Ref{}, false
	}
	return Ref{DeviceID: device, EntityID: uniqueID}, true
}
