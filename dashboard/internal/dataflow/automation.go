package dataflow

import (
	"encoding/json"
	"fmt"
)

type automationDoc struct {
	Rules []struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Enabled    bool              `json:"enabled"`
		Conditions []json.RawMessage `json:"conditions"`
		Actions    []json.RawMessage `json:"actions"`
	} `json:"rules"`
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

var automationTitles = map[string]string{
	"balance_threshold": "Condition balance threshold",
	"topic_value":       "Condition topic value",
	"entity_value":      "Condition entity value",
	"publish":           "Action",
	"payload_balance":   "Value from the balance",
	"payload_topic":     "Value from a topic",
}

// automationGraph is the adapter for automation_rules.json. Rules are
// graph-shaped (conditions -> rule -> actions), so they get their own
// adapter instead of the schema-driven one, with the same edge model.
func automationGraph(doc []byte, ix *Index) (Graph, error) {
	graph := emptyGraph()
	var parsed automationDoc
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return graph, err
	}
	for _, rule := range parsed.Rules {
		ruleRef := Ref{VirtualID: RuleID(rule.ID)}
		link := Link{Tab: "automations", Target: rule.ID}
		enabled := rule.Enabled
		graph.Nodes = append(graph.Nodes, Node{VirtualID: ruleRef.VirtualID, Kind: "rule", Label: rule.Name, Enabled: &enabled,
			Conditions: rule.Conditions, Actions: rule.Actions, Link: link})
		edge := func(id, kind string, from, to Ref, details Details) {
			details.Kind = kind
			graph.Edges = append(graph.Edges, Edge{ID: id, Cat: CatAutomation, From: from, To: to,
				Title: automationTitles[kind], TitleKey: "devicemap.flow.title." + kind, Details: details, Link: link})
		}
		unresolved := func(id, kind, topic, direction, reason string) {
			graph.Unresolved = append(graph.Unresolved, Unresolved{ID: id, Config: "automation_rules", Item: rule.ID, Topic: topic,
				Direction: direction, Reason: reason, Title: automationTitles[kind], TitleKey: "devicemap.flow.title." + kind, Target: ruleRef, Link: link})
		}
		for index, raw := range rule.Conditions {
			var part automationPart
			if json.Unmarshal(raw, &part) != nil {
				continue
			}
			id := fmt.Sprintf("auto:%s/c%d", ruleRef.VirtualID, index)
			details := Details{Part: "condition", Index: index, Topic: part.Topic, JSONKey: part.JSONKey, Field: part.Field}
			switch part.Type {
			case "balance_threshold":
				edge(id, part.Type, Ref{VirtualID: BalanceID}, ruleRef, details)
			case "topic_value":
				if from, reason := ix.Input(part.Topic, part.JSONKey); reason == "" {
					edge(id, part.Type, from, ruleRef, details)
				} else {
					unresolved(id, part.Type, part.Topic, "input", reason)
				}
			case "entity_value":
				if from, ok := ix.Entity(part.EntityID); ok {
					edge(id, part.Type, from, ruleRef, details)
				} else {
					unresolved(id, part.Type, part.Topic, "input", ReasonNoProducer)
				}
			}
		}
		for index, raw := range rule.Actions {
			var part automationPart
			if json.Unmarshal(raw, &part) != nil || part.Type != "publish" {
				continue
			}
			id := fmt.Sprintf("auto:%s/a%d", ruleRef.VirtualID, index)
			details := Details{Part: "action", Index: index, Topic: part.Topic, Value: part.Payload}
			if to, reason := ix.Output(part.Topic); reason == "" {
				edge(id, part.Type, ruleRef, to, details)
			} else {
				unresolved(id, part.Type, part.Topic, "output", reason)
			}
			switch part.PayloadSource {
			case "balance":
				edge(id+"/src", "payload_balance", Ref{VirtualID: BalanceID}, ruleRef, Details{Part: "action", Index: index, Field: part.Field})
			case "topic":
				if from, reason := ix.Input(part.SourceTopic, part.SourceJSONKey); reason == "" {
					edge(id+"/src", "payload_topic", from, ruleRef, Details{Part: "action", Index: index, Topic: part.SourceTopic, JSONKey: part.SourceJSONKey})
				} else {
					unresolved(id+"/src", "payload_topic", part.SourceTopic, "input", reason)
				}
			}
		}
	}
	return graph, nil
}
