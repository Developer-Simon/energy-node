package media

import (
	"encoding/json"
	"fmt"
)

// OSListURL is the Raspberry Pi Imager OS list.
const OSListURL = "https://downloads.raspberrypi.com/os_list_imagingutility_v4.json"

// Image is one downloadable OS image from the Raspberry Pi OS list.
type Image struct {
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	DownloadSize  int64    `json:"image_download_size"`
	ExtractSize   int64    `json:"extract_size"`
	ExtractSHA256 string   `json:"extract_sha256"`
	InitFormat    string   `json:"init_format"` // "cloudinit-rpi" | "systemd" | ...
	ReleaseDate   string   `json:"release_date"`
	Devices       []string `json:"devices"`
}

// osEntry is a list node: either an image or a group with subitems.
type osEntry struct {
	Image
	Subitems []osEntry `json:"subitems"`
}

// OSList is the parsed Raspberry Pi OS list document.
type OSList struct {
	Items []osEntry `json:"os_list"`
}

// ParseOSList parses the Raspberry Pi OS list JSON.
func ParseOSList(data []byte) (*OSList, error) {
	var list OSList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse OS list: %w", err)
	}
	return &list, nil
}

// Find returns the first image in document order whose name matches and that
// lists the device tag. It searches recursively through "subitems".
func (l *OSList) Find(name, tag string) (Image, bool) {
	return findIn(l.Items, name, tag)
}

func findIn(entries []osEntry, name, tag string) (Image, bool) {
	for _, e := range entries {
		if e.Name == name && hasTag(e.Devices, tag) {
			return e.Image, true
		}
		if img, ok := findIn(e.Subitems, name, tag); ok {
			return img, true
		}
	}
	return Image{}, false
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
