package hostapi

import "strings"

// Mask ersetzt ein Geheimnis in jeder Ausgabe.
const Mask = "***"

// minSecretLength ist die Laenge, ab der ein Wert ueberhaupt gefiltert wird.
// Ein ein- oder zweizeichiges "Geheimnis" kaeme in jeder zweiten Logzeile vor
// und machte das Log unlesbar, ohne irgendetwas zu schuetzen.
const minSecretLength = 3

// Redactor entfernt bekannte Geheimnisse aus allem, was die Oberflaeche zu
// sehen bekommt. Er deckt den Rueckweg ab; der Hinweg ist ueber Dateien mit
// 0600 geloest (siehe Umgang mit Geheimnissen in der Spec).
type Redactor struct {
	secrets []string
}

// NewRedactor baut einen Filter fuer die uebergebenen Werte. Leere und sehr
// kurze Werte werden verworfen.
func NewRedactor(secrets ...string) *Redactor {
	kept := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if len(secret) >= minSecretLength {
			kept = append(kept, secret)
		}
	}
	return &Redactor{secrets: kept}
}

// Line filtert eine einzelne Zeile.
func (r *Redactor) Line(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Mask)
	}
	return s
}
