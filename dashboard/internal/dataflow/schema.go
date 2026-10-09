package dataflow

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
