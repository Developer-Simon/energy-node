package credstore

// Memory ist eine Ablage im Speicher fuer Tests. Die Fehlerfelder simulieren
// einen gesperrten oder fehlenden Schluesselbund.
type Memory struct {
	Saved     *Credentials
	LoadErr   error
	SaveErr   error
	DeleteErr error
	Saves     int
	Deletes   int
}

func (m *Memory) Load() (Credentials, bool, error) {
	if m.LoadErr != nil {
		return Credentials{}, false, m.LoadErr
	}
	if m.Saved == nil {
		return Credentials{}, false, nil
	}
	return *m.Saved, true, nil
}

func (m *Memory) Save(c Credentials) error {
	m.Saves++
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.Saved = &c
	return nil
}

func (m *Memory) Delete() error {
	m.Deletes++
	if m.DeleteErr != nil {
		return m.DeleteErr
	}
	m.Saved = nil
	return nil
}
