package dataflow

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// walkSchemaFields calls visit for every property schema below node, through
// properties, items, allOf and if/then/else, with a dotted path for messages.
func walkSchemaFields(node any, path string, visit func(path string, field map[string]any)) {
	schema, ok := node.(map[string]any)
	if !ok {
		return
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for key, child := range properties {
			if field, ok := child.(map[string]any); ok {
				visit(path+"."+key, field)
			}
			walkSchemaFields(child, path+"."+key, visit)
		}
	}
	for _, keyword := range []string{"items", "if", "then", "else"} {
		walkSchemaFields(schema[keyword], path+"."+keyword, visit)
	}
	if all, ok := schema["allOf"].([]any); ok {
		for _, child := range all {
			walkSchemaFields(child, path+".allOf", visit)
		}
	}
}

type fieldSpec struct {
	direction    string
	label        string
	labelKey     string
	jsonKeyField string
	unitField    string
}

type nodeSpec struct {
	idField          string
	labelField       string
	deviceIDTemplate string
}

// fieldSpecs collects the annotated fields of one object schema: its own
// properties and those of every allOf/then/else branch, the first
// declaration winning (as config.branchDeclarations). Whether a branch is
// active is decided per entry by config.ActiveProperties.
func fieldSpecs(schema map[string]any) map[string]fieldSpec {
	specs := map[string]fieldSpec{}
	var visit func(node any)
	visit = func(node any) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}
		if properties, ok := object["properties"].(map[string]any); ok {
			for key, child := range properties {
				field, _ := child.(map[string]any)
				annotation, ok := field["x-dataflow"].(map[string]any)
				if !ok {
					continue
				}
				if _, seen := specs[key]; seen {
					continue
				}
				specs[key] = fieldSpec{
					direction:    stringOf(annotation["direction"]),
					label:        stringOf(annotation["label"]),
					labelKey:     stringOf(field["x-dataflow-label-key"]),
					jsonKeyField: stringOf(annotation["json_key_field"]),
					unitField:    stringOf(annotation["unit_field"]),
				}
			}
		}
		if all, ok := object["allOf"].([]any); ok {
			for _, entry := range all {
				visit(entry)
			}
		}
		visit(object["then"])
		visit(object["else"])
	}
	visit(schema)
	return specs
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

func serviceGraph(name, labelKey, label string, doc, schemaRaw []byte, ix *Index) (Graph, error) {
	graph := emptyGraph()
	var schema map[string]any
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return graph, err
	}
	var value any
	if err := json.Unmarshal(doc, &value); err != nil {
		return graph, err
	}
	itemSchema := schema
	entries := []any{value}
	if schema["type"] == "array" {
		itemSchema, _ = schema["items"].(map[string]any)
		entries, _ = value.([]any)
	}
	specs := fieldSpecs(itemSchema)
	if len(specs) == 0 {
		return graph, nil
	}
	var node *nodeSpec
	if raw, ok := itemSchema["x-dataflow-node"].(map[string]any); ok {
		node = &nodeSpec{idField: stringOf(raw["id_field"]), labelField: stringOf(raw["label_field"]), deviceIDTemplate: stringOf(raw["device_id_template"])}
	}
	fileNodeAdded := false
	for _, entry := range entries {
		object, _ := entry.(map[string]any)
		item, itemLabel, itemLabelKey := "", label, labelKey
		if node != nil {
			item = stringOf(object[node.idField])
			if text := stringOf(object[node.labelField]); text != "" {
				itemLabel, itemLabelKey = text, ""
			}
		}
		link := Link{Tab: "config", Target: name, Item: item}
		target := Ref{VirtualID: ServiceID(name, item)}
		if node != nil && node.deviceIDTemplate != "" {
			if device := strings.ReplaceAll(node.deviceIDTemplate, "{id}", item); ix.HasDevice(device) {
				target = Ref{DeviceID: device}
			}
		}
		active, err := config.ActiveProperties(entry, itemSchema)
		if err != nil {
			graph.Unresolved = append(graph.Unresolved, Unresolved{ID: edgeID(name, item, ""), Config: name, Item: item, Reason: ReasonConfigInvalid, Link: link})
			continue
		}
		if target.VirtualID != "" && (node != nil || !fileNodeAdded) {
			graph.Nodes = append(graph.Nodes, Node{VirtualID: target.VirtualID, Kind: "service", Label: itemLabel, LabelKey: itemLabelKey, Link: link})
			fileNodeAdded = true
		}
		for _, field := range sortedSpecKeys(specs) {
			spec := specs[field]
			topic := stringOf(object[field])
			if !active[field] || topic == "" {
				continue
			}
			details := Details{Topic: topic, Field: field}
			if spec.jsonKeyField != "" {
				details.JSONKey = stringOf(object[spec.jsonKeyField])
			}
			if spec.unitField != "" {
				details.Unit = stringOf(object[spec.unitField])
			}
			id := edgeID(name, item, field)
			var other Ref
			var reason string
			if spec.direction == "output" {
				other, reason = ix.Output(topic)
			} else {
				other, reason = ix.Input(topic, details.JSONKey)
			}
			if reason != "" {
				graph.Unresolved = append(graph.Unresolved, Unresolved{ID: id, Config: name, Item: item, Topic: topic, Field: field,
					Direction: spec.direction, Reason: reason, Title: spec.label, TitleKey: spec.labelKey, Target: target, Link: link})
				continue
			}
			edge := Edge{ID: id, Cat: CatService, From: other, To: target, Title: spec.label, TitleKey: spec.labelKey, Details: details, Link: link}
			if spec.direction == "output" {
				edge.From, edge.To = target, other
			}
			graph.Edges = append(graph.Edges, edge)
		}
	}
	return graph, nil
}

func edgeID(name, item, field string) string {
	parts := []string{"svc:" + name}
	if item != "" {
		parts = append(parts, url.PathEscape(item))
	}
	if field != "" {
		parts = append(parts, field)
	}
	return strings.Join(parts, "/")
}

func sortedSpecKeys(specs map[string]fieldSpec) []string {
	keys := make([]string, 0, len(specs))
	for key := range specs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
