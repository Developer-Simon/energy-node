// Package sun computes sunrise, sunset and the sun position with the NOAA
// solar calculator algorithm. It mirrors services/automation/sun.py, so the
// Go and Python sides agree on the same day (about one minute accuracy at
// mid latitudes).
package sun

import (
	"math"
	"time"
)

// zenithDeg is the zenith at which the sun's disc is fully below the horizon,
// including atmospheric refraction.
const zenithDeg = 90.833

// Location is a point on earth, latitude north positive, longitude east positive.
type Location struct {
	Lat, Lon float64
}

func julianDay(moment time.Time) float64 {
	return float64(moment.UnixNano())/1e9/86400.0 + 2440587.5
}

// solarParams holds the quantities shared by Times and Position for one instant.
type solarParams struct {
	meanLong float64 // radians
	meanAnom float64 // radians
	ecc      float64
	decl     float64 // radians
	eqTime   float64 // minutes
}

func params(moment time.Time) solarParams {
	t := (julianDay(moment) - 2451545.0) / 36525.0

	meanLong := radians(math.Mod(280.46646+t*(36000.76983+t*0.0003032), 360))
	meanAnom := radians(357.52911 + t*(35999.05029-0.0001537*t))
	ecc := 0.016708634 - t*(0.000042037+0.0000001267*t)
	center := math.Sin(meanAnom)*(1.914602-t*(0.004817+0.000014*t)) +
		math.Sin(2*meanAnom)*(0.019993-0.000101*t) +
		math.Sin(3*meanAnom)*0.000289
	omega := radians(125.04 - 1934.136*t)
	appLong := radians(degrees(meanLong) + center - 0.00569 - 0.00478*math.Sin(omega))
	obliq0 := 23 + (26+(21.448-t*(46.815+t*(0.00059-t*0.001813)))/60)/60
	obliq := radians(obliq0 + 0.00256*math.Cos(omega))
	decl := math.Asin(math.Sin(obliq) * math.Sin(appLong))

	y := math.Pow(math.Tan(obliq/2), 2)
	eqTime := 4 * degrees(
		y*math.Sin(2*meanLong)-
			2*ecc*math.Sin(meanAnom)+
			4*ecc*y*math.Sin(meanAnom)*math.Cos(2*meanLong)-
			0.5*y*y*math.Sin(4*meanLong)-
			1.25*ecc*ecc*math.Sin(2*meanAnom))

	return solarParams{meanLong: meanLong, meanAnom: meanAnom, ecc: ecc, decl: decl, eqTime: eqTime}
}

// Times returns sunrise and sunset for the calendar day of day at the given
// location. The calendar day is taken from day's year, month and day fields.
// ok is false during polar day and polar night, when the sun does not rise
// or does not set on that day.
func Times(day time.Time, lat, lon float64) (rise, set time.Time, ok bool) {
	y, m, d := day.Date()
	noonGuess := time.Date(y, m, d, 12, 0, 0, 0, time.UTC).
		Add(-time.Duration(lon / 15 * float64(time.Hour)))
	p := params(noonGuess)

	latRad := radians(lat)
	cosHourAngle := math.Cos(radians(zenithDeg))/(math.Cos(latRad)*math.Cos(p.decl)) -
		math.Tan(latRad)*math.Tan(p.decl)
	if cosHourAngle < -1 || cosHourAngle > 1 {
		return time.Time{}, time.Time{}, false
	}
	hourAngleDeg := degrees(math.Acos(cosHourAngle))

	noonMin := 720 - 4*lon - p.eqTime
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	riseOffset := time.Duration((noonMin - 4*hourAngleDeg) * float64(time.Minute))
	setOffset := time.Duration((noonMin + 4*hourAngleDeg) * float64(time.Minute))
	return midnight.Add(riseOffset), midnight.Add(setOffset), true
}

// Position returns the sun's azimuth in degrees, measured from north
// clockwise, and its elevation above the horizon in degrees. The elevation
// is geometric, without refraction.
func Position(t time.Time, lat, lon float64) (azimuthDeg, elevationDeg float64) {
	p := params(t)

	utcMin := float64(t.UTC().Hour()*60+t.UTC().Minute()) + float64(t.UTC().Second())/60
	trueSolarMin := math.Mod(utcMin+p.eqTime+4*lon, 1440)
	if trueSolarMin < 0 {
		trueSolarMin += 1440
	}
	hourAngleDeg := trueSolarMin/4 - 180
	if trueSolarMin/4 < 0 {
		hourAngleDeg = trueSolarMin/4 + 180
	}

	latRad := radians(lat)
	hourAngleRad := radians(hourAngleDeg)
	cosZenith := math.Sin(latRad)*math.Sin(p.decl) +
		math.Cos(latRad)*math.Cos(p.decl)*math.Cos(hourAngleRad)
	cosZenith = math.Max(-1, math.Min(1, cosZenith))
	zenith := math.Acos(cosZenith)
	elevationDeg = 90 - degrees(zenith)

	sinZenith := math.Sin(zenith)
	if sinZenith == 0 || math.Cos(latRad) == 0 {
		// At the zenith or a pole the azimuth is undefined, report due south.
		return 180, elevationDeg
	}
	cosAz := (math.Sin(latRad)*math.Cos(zenith) - math.Sin(p.decl)) /
		(math.Cos(latRad) * sinZenith)
	cosAz = math.Max(-1, math.Min(1, cosAz))
	acosDeg := degrees(math.Acos(cosAz))
	if hourAngleDeg > 0 {
		azimuthDeg = math.Mod(acosDeg+180, 360)
	} else {
		azimuthDeg = math.Mod(540-acosDeg, 360)
	}
	return azimuthDeg, elevationDeg
}

func radians(deg float64) float64 { return deg * math.Pi / 180 }

func degrees(rad float64) float64 { return rad * 180 / math.Pi }
