package media

// wlanInterfaceInfo entspricht WLAN_INTERFACE_INFO (532 Bytes).
type wlanInterfaceInfo struct {
	GUID        [16]byte
	Description [256]uint16
	State       uint32
}

// wlanBssEntry entspricht WLAN_BSS_ENTRY (360 Bytes).
type wlanBssEntry struct {
	SSIDLen           uint32
	SSID              [32]byte
	PhyID             uint32
	BSSID             [6]byte
	BssType           uint32
	PhyType           uint32
	RSSI              int32
	LinkQuality       uint32
	InRegDomain       uint8
	BeaconPeriod      uint16
	Timestamp         uint64
	HostTimestamp     uint64
	Capability        uint16
	ChCenterFrequency uint32 // kHz
	RateSetLen        uint32
	RateSet           [126]uint16
	IeOffset          uint32
	IeSize            uint32
}

// wlanConnectionHead ist der Anfang von WLAN_CONNECTION_ATTRIBUTES bis zur BSSID.
type wlanConnectionHead struct {
	State       uint32
	Mode        uint32
	ProfileName [256]uint16
	SSIDLen     uint32
	SSID        [32]byte
	BssType     uint32
	BSSID       [6]byte
}

// wlanProfileInfo entspricht WLAN_PROFILE_INFO (516 Bytes).
type wlanProfileInfo struct {
	Name  [256]uint16
	Flags uint32
}
