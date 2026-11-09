package energystats

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// MergeDir merges the monthly files of srcDir (from an archive) into dstDir (live).
// Per month and hour: a source-'h' hour (Home Assistant) beats everything,
// otherwise an existing value in dst stays, an empty dst hour ('-' or NaN) is filled from src.
// A month file in src that does not decode is skipped; its name is collected and returned
// in the error as a MergeSkipped (see below), all other months are still merged.
// A month that does not exist in dst is copied as is (after validating via decodeMonth).
// Files whose name is not a month (year-month plus .json) are ignored.
func MergeDir(dstDir, srcDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	var skipped []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isMonthFileName(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return err
		}
		src, err := decodeMonth(data)
		if err != nil {
			skipped = append(skipped, name)
			continue
		}
		if err := mergeMonthFile(dstDir, name, data, src); err != nil {
			return err
		}
	}
	if len(skipped) > 0 {
		return &MergeSkipped{Files: skipped}
	}
	return nil
}

// MergeSkipped meldet Monatsdateien des Archivs, die nicht gelesen werden
// konnten. Alle anderen Monate sind trotzdem zusammengefuehrt.
type MergeSkipped struct{ Files []string }

func (e *MergeSkipped) Error() string {
	return fmt.Sprintf("energystats: %d Monatsdatei(en) im Archiv nicht lesbar: %s",
		len(e.Files), strings.Join(e.Files, ", "))
}

func isMonthFileName(name string) bool {
	base, ok := strings.CutSuffix(name, ".json")
	if !ok || len(base) != len("2006-01") {
		return false
	}
	_, err := time.Parse("2006-01", base)
	return err == nil
}

// mergeMonthFile fuehrt einen Monat aus dem Archiv in die Live-Datei ein.
// srcData ist die unveraendert gelesene Archivdatei, src ihr decodierter Monat.
func mergeMonthFile(dstDir, name string, srcData []byte, src *Month) error {
	path := filepath.Join(dstDir, name)
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return config.AtomicWrite(path, srcData, 0o600)
	}
	if err != nil {
		return err
	}
	dst, err := decodeMonth(existing)
	if err != nil {
		return fmt.Errorf("energystats: Live-Monat %s: %w", name, err)
	}

	changed := false
	taken := make([]bool, dst.Hours())
	for role, srcSeries := range src.Roles {
		dstSeries := dst.series(role)
		for i := range srcSeries.Source {
			if srcSeries.Source[i] == SourceNone {
				continue
			}
			take := false
			switch {
			case srcSeries.Source[i] == SourceHA:
				take = dstSeries.Source[i] != SourceHA
			case dstSeries.Source[i] == SourceNone:
				take = true
			}
			if !take {
				continue
			}
			dstSeries.KWh[i] = srcSeries.KWh[i]
			dstSeries.Source[i] = srcSeries.Source[i]
			taken[i] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	for i, ok := range taken {
		if ok && src.Covered[i] > dst.Covered[i] {
			dst.Covered[i] = src.Covered[i]
		}
	}
	data, err := dst.MarshalJSON()
	if err != nil {
		return err
	}
	return config.AtomicWrite(path, data, 0o600)
}
