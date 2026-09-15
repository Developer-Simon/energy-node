// Package hostapi ist Schicht 2: der wirt-neutrale HTTP+SSE-Server der
// Oberflaeche. Er kennt keinen Transport - was tatsaechlich auf dem Node
// passiert, liefert ein Backend (siehe backend.go). Der Installer-Wirt fuellt
// es ueber SSH, der Dashboard-Wirt spaeter lokal ueber die Updater-Unit; die
// Oberflaeche sieht in beiden Faellen denselben Ereignisstrom und kennt den
// Unterschied nicht.
package hostapi
