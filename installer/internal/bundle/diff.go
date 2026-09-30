package bundle

import "sort"

// DiffManifest compares old (the node's last verified or hashed bundle state,
// from DeltaBase) against new (the bundle about to be deployed) and reports
// exactly what DeployDelta must send and remove so that remoteDir ends up
// holding precisely new.Files -- one file at a time, without touching any
// file whose hash did not change between the two.
//
// old must not be nil: callers check DeltaBase's result (which never returns
// nil) before ever calling this. new comes from a verified manifest.json
// (signed, or hash-checked by VerifyDev) before it reached this function;
// old either comes from the node's last verified manifest or from hashing
// the node's own file listing. The relpaths in new are trusted -- DeployDelta
// still refuses any relpath that escapes remoteDir, as defence in depth.
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
