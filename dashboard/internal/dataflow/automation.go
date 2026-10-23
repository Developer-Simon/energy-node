package dataflow

import (
	"encoding/json"
	"fmt"
)

type automationDoc struct {
	Rules     []automationRule     `json:"rules"`
	Scenarios []automationScenario `json:"scenarios"`
}

type automationRule struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled"`
	Conditions []json.RawMessage `json:"conditions"`
	Actions    []json.RawMessage `json:"actions"`
}

type automationPhase struct {
	Conditions []json.RawMessage `json:"conditions"`
	Actions    []json.RawMessage `json:"actions"`
}

type automationScenario struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Enabled bool            `json:"enabled"`
	When    automationPhase `json:"when"`
	Until   automationPhase `json:"until"`
	OnStart automationPhase `json:"on_start"`
	While   struct {
		Actions   []json.RawMessage    `json:"actions"`
		Regulator *automationRegulator `json:"regulator"`
	} `json:"while"`
	OnEnd automationPhase `json:"on_end"`
}

type automationRegulator struct {
	Output struct {
		Topic string `json:"topic"`
	} `json:"output"`
	Base struct {
		Source   string `json:"source"`
		EntityID string `json:"entity_id"`
		Topic    string `json:"topic"`
		Field    string `json:"field"`
	} `json:"base"`
}

type automationPart struct {
	Type          string `json:"type"`
	Field         string `json:"field"`
	Topic         string `json:"topic"`
	JSONKey       string `json:"json_key"`
	EntityID      string `json:"entity_id"`
	PayloadSource string `json:"payload_source"`
	Payload       string `json:"payload"`
	SourceTopic   string `json:"source_topic"`
	SourceJSONKey string `json:"source_json_key"`
}

// automationDeviceID is the HA device of the automation service: its
// service id, see config.ServiceIDForConfig("automation_rules").
const automationDeviceID = "automation"

var automationTitles = map[string]string{
	"balance_threshold": "Condition balance threshold",
	"topic_value":       "Condition topic value",
	"entity_value":      "Condition entity value",
	"executes":          "Runs the rule",
	"publish":           "Action",
	"payload_balance":   "Value from the balance",
	"payload_topic":     "Value from a topic",
	"regulator_output":  "Regulator output",
	"regulator_base":    "Regulator reading",
}

// automationNode wires one rule or scenario node into the graph. Rules and
// scenarios share the namespace of their node IDs, so the device map treats
// both alike; only the edge IDs differ by the phase prefix.
type automationNode struct {
	graph *Graph
	ix    *Index
	item  string
	ref   Ref
	link  Link
}

func (n *automationNode) edge(id, kind string, from, to Ref, details Details) {
	details.Kind = kind
	n.graph.Edges = append(n.graph.Edges, Edge{ID: id, Cat: CatAutomation, From: from, To: to,
		Title: automationTitles[kind], TitleKey: "devicemap.flow.title." + kind, Details: details, Link: n.link})
}

func (n *automationNode) unresolved(id, kind, topic, direction, reason string) {
	n.graph.Unresolved = append(n.graph.Unresolved, Unresolved{ID: id, Config: "automation_rules", Item: n.item, Topic: topic,
		Direction: direction, Reason: reason, Title: automationTitles[kind], TitleKey: "devicemap.flow.title." + kind, Target: n.ref, Link: n.link})
}

// executes links the service device to the node it runs.
func (n *automationNode) executes() {
	if n.ix.HasDevice(automationDeviceID) {
		n.edge("auto:"+n.ref.VirtualID+"/run", "executes", Ref{DeviceID: automationDeviceID}, n.ref, Details{Part: "rule"})
	}
}

// conditions wires the conditions of one list; prefix keeps the edge IDs of
// different lists of the same node apart.
func (n *automationNode) conditions(prefix string, conditions []json.RawMessage) {
	for index, raw := range conditions {
		var part automationPart
		if json.Unmarshal(raw, &part) != nil {
			continue
		}
		id := fmt.Sprintf("auto:%s/%s%d", n.ref.VirtualID, prefix, index)
		details := Details{Part: "condition", Index: index, Topic: part.Topic, JSONKey: part.JSONKey, Field: part.Field}
		switch part.Type {
		case "balance_threshold":
			n.edge(id, part.Type, Ref{VirtualID: BalanceID}, n.ref, details)
		case "topic_value":
			if from, reason := n.ix.Input(part.Topic, part.JSONKey); reason == "" {
				n.edge(id, part.Type, from, n.ref, details)
			} else {
				n.unresolved(id, part.Type, part.Topic, "input", reason)
			}
		case "entity_value":
			if from, ok := n.ix.Entity(part.EntityID); ok {
				n.edge(id, part.Type, from, n.ref, details)
			} else {
				n.unresolved(id, part.Type, part.Topic, "input", ReasonNoProducer)
			}
		}
	}
}

// actions wires the publish actions of one list and their payload sources.
func (n *automationNode) actions(prefix string, actions []json.RawMessage) {
	for index, raw := range actions {
		var part automationPart
		if json.Unmarshal(raw, &part) != nil || part.Type != "publish" {
			continue
		}
		id := fmt.Sprintf("auto:%s/%s%d", n.ref.VirtualID, prefix, index)
		details := Details{Part: "action", Index: index, Topic: part.Topic, Value: part.Payload}
		if to, reason := n.ix.Output(part.Topic); reason == "" {
			n.edge(id, part.Type, n.ref, to, details)
		} else {
			n.unresolved(id, part.Type, part.Topic, "output", reason)
		}
		switch part.PayloadSource {
		case "balance":
			n.edge(id+"/src", "payload_balance", Ref{VirtualID: BalanceID}, n.ref, Details{Part: "action", Index: index, Field: part.Field})
		case "topic":
			if from, reason := n.ix.Input(part.SourceTopic, part.SourceJSONKey); reason == "" {
				n.edge(id+"/src", "payload_topic", from, n.ref, Details{Part: "action", Index: index, Topic: part.SourceTopic, JSONKey: part.SourceJSONKey})
			} else {
				n.unresolved(id+"/src", "payload_topic", part.SourceTopic, "input", reason)
			}
		}
	}
}

// regulator wires the output edge and the reading edge of a regulate phase.
func (n *automationNode) regulator(regulator *automationRegulator) {
	outID := "auto:" + n.ref.VirtualID + "/out"
	if to, reason := n.ix.Output(regulator.Output.Topic); reason == "" {
		n.edge(outID, "regulator_output", n.ref, to, Details{Part: "regulator", Topic: regulator.Output.Topic})
	} else {
		n.unresolved(outID, "regulator_output", regulator.Output.Topic, "output", reason)
	}
	baseID := "auto:" + n.ref.VirtualID + "/base"
	switch regulator.Base.Source {
	case "entity":
		if from, ok := n.ix.Entity(regulator.Base.EntityID); ok {
			n.edge(baseID, "regulator_base", from, n.ref, Details{Part: "regulator", Topic: regulator.Base.Topic})
		} else {
			n.unresolved(baseID, "regulator_base", regulator.Base.Topic, "input", ReasonNoProducer)
		}
	case "balance":
		n.edge(baseID, "regulator_base", Ref{VirtualID: BalanceID}, n.ref, Details{Part: "regulator", Field: regulator.Base.Field})
	}
}

// automationGraph is the adapter for automation_rules.json. Rules and
// scenarios are graph-shaped (conditions -> node -> actions), so they get their
// own adapter instead of the schema-driven one, with the same edge model.
func automationGraph(doc []byte, ix *Index) (Graph, error) {
	graph := emptyGraph()
	var parsed automationDoc
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return graph, err
	}
	for _, rule := range parsed.Rules {
		enabled := rule.Enabled
		node := automationNode{graph: &graph, ix: ix, item: rule.ID,
			ref: Ref{VirtualID: RuleID(rule.ID)}, link: Link{Tab: "automations", Target: rule.ID}}
		graph.Nodes = append(graph.Nodes, Node{VirtualID: node.ref.VirtualID, Kind: "rule", Label: rule.Name, Enabled: &enabled,
			Conditions: rule.Conditions, Actions: rule.Actions, Link: node.link})
		node.executes()
		node.conditions("c", rule.Conditions)
		node.actions("a", rule.Actions)
	}
	for _, scenario := range parsed.Scenarios {
		enabled := scenario.Enabled
		node := automationNode{graph: &graph, ix: ix, item: scenario.ID,
			ref: Ref{VirtualID: RuleID(scenario.ID)}, link: Link{Tab: "automations", Target: scenario.ID}}
		conditions := append(append([]json.RawMessage{}, scenario.When.Conditions...), scenario.Until.Conditions...)
		actions := append(append(append([]json.RawMessage{}, scenario.OnStart.Actions...), scenario.While.Actions...), scenario.OnEnd.Actions...)
		graph.Nodes = append(graph.Nodes, Node{VirtualID: node.ref.VirtualID, Kind: "rule", Label: scenario.Name, Enabled: &enabled,
			Conditions: conditions, Actions: actions, Link: node.link})
		node.executes()
		node.conditions("w", scenario.When.Conditions)
		node.conditions("u", scenario.Until.Conditions)
		node.actions("s", scenario.OnStart.Actions)
		node.actions("h", scenario.While.Actions)
		node.actions("e", scenario.OnEnd.Actions)
		if scenario.While.Regulator != nil {
			node.regulator(scenario.While.Regulator)
		}
	}
	return graph, nil
}
