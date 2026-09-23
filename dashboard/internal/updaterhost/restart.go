package updaterhost

// libraryUsers maps a shared library to the service directories that import
// it; nil means every service. Keep in step with LIBRARY_USERS in
// scripts/bootstrap/lib/restart_rule.py (restart_test.go checks the rule, and
// test_restart_rule.py checks that table against the services' imports).
var libraryUsers = map[string][]string{
	"energy_node_common": nil,
	"battery_soc_core":   {"battery_soc"},
}

// restartReason is the Go copy of restart_rule.restart_reason: "" (no
// restart), "all", "first", "unknown", "version" or "library".
func restartReason(installed *installedManifest, candidate *candidateManifest, stepID string, restartAll bool) string {
	if restartAll {
		return "all"
	}
	if candidate == nil {
		return "unknown"
	}
	if installed == nil {
		return "first"
	}
	var dir, newVersion string
	found := false
	for _, s := range candidate.Steps {
		if s.ID == stepID {
			dir, newVersion, found = s.Dir, s.Version, true
			break
		}
	}
	if !found {
		return ""
	}
	oldVersion, seen := "", false
	for _, s := range installed.Steps {
		if s.ID == stepID {
			oldVersion, seen = s.Version, true
			break
		}
	}
	if !seen {
		return "first"
	}
	if oldVersion == "" || newVersion == "" {
		return "unknown"
	}
	if oldVersion != newVersion {
		return "version"
	}
	for name, users := range libraryUsers {
		version := candidate.Components[name]
		if version == "" || installed.Components[name] == version {
			continue
		}
		if users == nil {
			return "library"
		}
		for _, u := range users {
			if u == dir {
				return "library"
			}
		}
	}
	return ""
}
