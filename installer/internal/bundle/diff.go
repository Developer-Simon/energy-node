package bundle

import "sort"

// DiffManifest compares old (the node's last fully-applied bundle, read via
// ReadInstalledManifest) against new (the bundle about to be deployed) and
// reports exactly what DeployDelta must send and remove so that remoteDir
// ends up holding precisely new.Files -- one file at a time, without
// touching any file whose hash did not change between the two.
//
// old must not be nil: callers check ReadInstalledManifest's result (nil
// means "nothing trustworthy to diff against") before ever calling this.
// Both manifests come from a manifest.json that was itself verified
// (signed, or hash-checked by VerifyDev) before it reached this function,
// so the relpaths are trusted -- DeployDelta still refuses one that
// escapes remoteDir, as defence in depth.
func DiffManifest(old, new *Manifest) (changed, removed []string) {
	for rel, hash := range new.Files {
		if old.Files[rel] != hash {
			changed = append(changed, rel)
		}
	}
	for rel := range old.Files {
		if _, ok := new.Files[rel]; !ok {
			removed = append(removed, rel)
		}
	}
	sort.Strings(changed)
	sort.Strings(removed)
	return changed, removed
}
