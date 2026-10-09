package dataflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Spec "Dienst-Einbettung": every field with format mqtt-topic in a service
// schema decides whether it shows up in the data flow, with an x-dataflow
// object or an explicit false. A new service that adds a topic field fails
// here until it decides.
func TestEveryServiceTopicFieldDeclaresDataflow(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "services", "*", "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no service schemas found")
	}
	topicFields := 0
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var schema any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		walkSchemaFields(schema, filepath.Base(file), func(path string, field map[string]any) {
			if field["format"] != "mqtt-topic" {
				return
			}
			topicFields++
			switch value := field["x-dataflow"].(type) {
			case bool:
				if value {
					t.Errorf("%s: x-dataflow true is not allowed, use an object or false", path)
				}
			case map[string]any:
				if direction := value["direction"]; direction != "input" && direction != "output" {
					t.Errorf("%s: x-dataflow.direction must be input or output, got %v", path, direction)
				}
				if label, _ := value["label"].(string); label == "" {
					t.Errorf("%s: x-dataflow.label is missing", path)
				}
			default:
				t.Errorf("%s: format mqtt-topic needs x-dataflow (an object or false)", path)
			}
		})
	}
	if topicFields < 9 {
		t.Fatalf("found only %d topic fields, the walk misses branches", topicFields)
	}
}
