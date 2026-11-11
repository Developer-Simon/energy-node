package backup

import (
	"regexp"
	"strconv"
	"time"
)

// Manifest beschreibt ein Archiv. Es steht als manifest.json zuletzt im Tar,
// damit die Prüfsummen beim Schreiben entstehen.
type Manifest struct {
	Format      int               `json:"format"`
	NodeVersion string            `json:"node_version"`
	Hostname    string            `json:"hostname"`
	Trigger     string            `json:"trigger"`
	CreatedAt   time.Time         `json:"created_at"`
	Areas       []Area            `json:"areas"`
	Secrets     bool              `json:"secrets"`
	Files       map[string]string `json:"files"` // Archivpfad -> sha256 (hex)
}

var semverPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// semverCore liefert major, minor und patch. Ein Suffix wie "-rc1" wird ignoriert.
func semverCore(v string) ([3]int, bool) {
	m := semverPattern.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// newerThanNode meldet, ob die Version eines Archivs neuer ist als die eigene.
// Der Build "dev" lehnt nie ab, und Versionen ohne lesbare Zahlen auch nicht.
func newerThanNode(archive string) bool {
	if NodeVersion == "dev" {
		return false
	}
	own, ok := semverCore(NodeVersion)
	if !ok {
		return false
	}
	theirs, ok := semverCore(archive)
	if !ok {
		return false
	}
	for i := range own {
		if theirs[i] != own[i] {
			return theirs[i] > own[i]
		}
	}
	return false
}
