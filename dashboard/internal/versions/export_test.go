package versions

// FallbackComponents exposes the fallback table to the external test package
// as id -> [label, kind].
func FallbackComponents() map[string][2]string {
	out := make(map[string][2]string, len(fallbackComponents))
	for id, meta := range fallbackComponents {
		out[id] = [2]string{meta.label, meta.kind}
	}
	return out
}
