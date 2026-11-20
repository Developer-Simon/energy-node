package autolearn

import (
	"math"
	"sort"
	"time"
)

// Tagestypen, Abwesenheit und Tagesgrenzen nach Spec 15.5. Die Erkenner lesen
// nur das Ergebnis, die Berechnung läuft über die Tagesdateien.

const (
	// baseDayGap ist der kleinste Sprung im Abstand zweier Wochentage (1 minus
	// Korrelation), der als Trennung gilt. Darunter ist es Rauschen. Die Spec
	// nennt dafür keine Zahl, der Wert ist eine Annahme.
	baseDayGap = 0.2
	// baseNoon ist die Stunde, bis zu der der heutige Tag als sein Wochentag gilt.
	baseNoon = 12
	// baseMorning sind die Viertelstunden bis zum Mittag.
	baseMorning = 48
	// baseNight sind die Viertelstunden der Nachtstunden, 0 bis 6 Uhr. Die Spec
	// nennt die Nachtstunden nicht, der Wert ist eine Annahme.
	baseNight = 24
	// baseQuietSigma ist die Zahl der Streuungen unter dem Median für einen stillen Tag.
	baseQuietSigma = 2.5
	// baseQuietHours sind die stillen Stunden, nach denen Abwesenheit beginnt.
	baseQuietHours = 36.0
	// baseQuarterMin ist die Länge einer Viertelstunde in Minuten.
	baseQuarterMin = 15.0
	// baseWattQuarterKWh rechnet W je Viertelstunde in kWh um.
	baseWattQuarterKWh = 0.25 / 1000
)

// baseRec ist ein Tag mit den Größen, die die Grundlagen brauchen.
type baseRec struct {
	date  string
	t     time.Time
	wd    int // Wochentag, Montag gleich 0
	week  int // ISO-Woche
	today bool
	full  bool // alle Viertelstunden erfasst und Last vorhanden
	morn  bool // alle Viertelstunden bis zum Mittag erfasst und Last vorhanden
	load  [dayQuarters]float64
	prof  [dayQuarters]float64 // Last geteilt durch Tagessumme, nur bei full
	hand  [dayQuarters]float64 // Handgriffe aller Geräte je Viertelstunde
	group int                  // Tagestyp, -1 ohne Zuordnung
}

// baseMetric sind die drei Größen der Abwesenheit: Energie über der
// Grundlast in kWh, Handgriffe und Unruhe (Summe der Betragsänderungen in W).
type baseMetric struct {
	e, h, u float64
}

// baseSpan ist Median und Streuung einer Größe in einem Tagestyp.
type baseSpan struct {
	med, sigma float64
}

// BuildBase leitet aus den Tagesprofilen die Tagestypen, die Abwesenheit und
// die Tagesgrenzen je Tagestyp ab. Der heutige Tag zählt für die Gruppen nicht,
// für Abwesenheit und Tagesgrenzen auch nicht.
func BuildBase(days []DayFile, loc *time.Location) Base {
	if loc == nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	recs := baseRecords(days, loc, now.Format(dayLayout))
	base := Base{
		DayType:   map[string]int{},
		Away:      map[string]bool{},
		DayStart:  map[int]float64{},
		NightRest: map[int]float64{},
	}

	var sample []int
	for i := range recs {
		if recs[i].full && !recs[i].today {
			sample = append(sample, i)
		}
	}
	if len(sample) == 0 {
		return base
	}
	split := baseSplit(recs, sample)
	base.Groups = baseGroups(split)
	profiles := baseProfiles(recs, sample, split, len(base.Groups))
	for i := range recs {
		g := baseAssign(&recs[i], split, profiles, now)
		if g < 0 {
			continue
		}
		recs[i].group = g
		base.DayType[recs[i].date] = g
	}
	baseAway(recs, &base)
	baseBounds(recs, &base)
	return base
}

// baseRecords liest die Tagesdateien in Datumsreihenfolge. Dateien mit
// unlesbarem Datum fallen weg.
func baseRecords(days []DayFile, loc *time.Location, today string) []baseRec {
	recs := make([]baseRec, 0, len(days))
	for i := range days {
		f := &days[i]
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		r := baseRec{date: f.Day, t: t, wd: (int(t.Weekday()) + 6) % 7, today: f.Day == today, group: -1}
		_, r.week = t.ISOWeek()
		allSeen, morning := true, true
		sum, sumMorning := 0.0, 0.0
		for q := 0; q < dayQuarters; q++ {
			if f.Site.Seen[q] == 0 {
				allSeen = false
				if q < baseMorning {
					morning = false
				}
			}
			r.load[q] = float64(f.Site.LoadTotal[q])
			sum += r.load[q]
			if q < baseMorning {
				sumMorning += r.load[q]
			}
		}
		for _, dd := range f.Devices {
			if dd == nil {
				continue
			}
			for q := range dd.Hand {
				r.hand[q] += float64(dd.Hand[q])
			}
		}
		r.full = allSeen && sum > 0
		r.morn = morning && sumMorning > 0
		if r.full {
			for q := range r.prof {
				r.prof[q] = r.load[q] / sum
			}
		}
		recs = append(recs, r)
	}
	sort.SliceStable(recs, func(a, b int) bool { return recs[a].date < recs[b].date })
	return recs
}

// baseSplit wählt die Gruppen der Wochentage. Zuerst die Clusterung über alle
// Tage, die Gegenprobe über gerade und ungerade Wochen. Besteht sie nicht, gilt
// Montag bis Freitag gegen Samstag und Sonntag, wenn auch diese Trennung in
// beiden Hälften hält. Sonst sind alle Tage gleich.
func baseSplit(recs []baseRec, sample []int) [7]int {
	all := baseMedians(recs, sample)
	var halves [2][]int
	for _, i := range sample {
		halves[recs[i].week%2] = append(halves[recs[i].week%2], i)
	}
	h0, h1 := baseMedians(recs, halves[0]), baseMedians(recs, halves[1])
	split := baseCluster(all)
	if baseAgrees(split, baseCluster(h0)) && baseAgrees(split, baseCluster(h1)) {
		return split
	}
	fixed := baseWorkWeekend(all)
	if baseSeparated(all, fixed) && baseSeparated(h0, fixed) && baseSeparated(h1, fixed) {
		return fixed
	}
	return baseOneGroup(all)
}

// baseCluster fasst die Wochentage zu höchstens drei Gruppen zusammen. Es wird
// nach mittlerem Abstand (average linkage) bis zu einer Gruppe verschmolzen.
// Geschnitten wird für zwei oder drei Gruppen an der größten Lücke zwischen
// den Verschmelzungsabständen. Ist die Lücke kleiner als baseDayGap, gibt es
// nur eine Gruppe. Fehlende Wochentage bekommen -1.
func baseCluster(m [7][]float64) [7]int {
	out := baseOneGroup(m)
	var present []int
	for i := range m {
		if m[i] != nil {
			present = append(present, i)
		}
	}
	n := len(present)
	if n < 2 {
		return out
	}
	var dist [7][7]float64
	for _, a := range present {
		for _, b := range present {
			dist[a][b] = 1 - baseCorr(m[a], m[b])
		}
	}
	clusters := make([][]int, n)
	for k, i := range present {
		clusters[k] = []int{i}
	}
	snaps := [][][]int{baseCopy(clusters)}
	var heights []float64
	for len(clusters) > 1 {
		ba, bb, best := 0, 1, math.Inf(1)
		for a := 0; a < len(clusters); a++ {
			for b := a + 1; b < len(clusters); b++ {
				if d := baseAvgDist(dist, clusters[a], clusters[b]); d < best {
					ba, bb, best = a, b, d
				}
			}
		}
		heights = append(heights, best)
		merged := append(append([]int{}, clusters[ba]...), clusters[bb]...)
		next := make([][]int, 0, len(clusters)-1)
		for k, c := range clusters {
			if k != ba && k != bb {
				next = append(next, c)
			}
		}
		clusters = append(next, merged)
		snaps = append(snaps, baseCopy(clusters))
	}
	bestK, bestJump := 0, 0.0
	for k := 2; k <= 3 && k <= n; k++ {
		idx := n - k
		prev := 0.0
		if idx > 0 {
			prev = heights[idx-1]
		}
		if jump := heights[idx] - prev; bestK == 0 || jump > bestJump {
			bestK, bestJump = k, jump
		}
	}
	if bestK == 0 || bestJump < baseDayGap {
		return out
	}
	return baseIDs(snaps[n-bestK])
}

// baseAgrees meldet, ob zwei Gruppierungen dieselbe Trennung zeigen, auf den
// Wochentagen, die in beiden vorkommen. Dafür müssen mindestens zwei gemeinsam
// sein.
func baseAgrees(a, b [7]int) bool {
	common := 0
	for i := 0; i < 7; i++ {
		if a[i] < 0 || b[i] < 0 {
			continue
		}
		common++
		for j := i + 1; j < 7; j++ {
			if a[j] < 0 || b[j] < 0 {
				continue
			}
			if (a[i] == a[j]) != (b[i] == b[j]) {
				return false
			}
		}
	}
	return common >= 2
}

// baseSeparated meldet, ob eine feste Gruppierung trennt: Der mittlere Abstand
// zwischen den Gruppen liegt mindestens baseDayGap über dem mittleren Abstand
// innerhalb der Gruppen.
func baseSeparated(m [7][]float64, split [7]int) bool {
	var within, between []float64
	for i := 0; i < 7; i++ {
		for j := i + 1; j < 7; j++ {
			if m[i] == nil || m[j] == nil || split[i] < 0 || split[j] < 0 {
				continue
			}
			d := 1 - baseCorr(m[i], m[j])
			if split[i] == split[j] {
				within = append(within, d)
			} else {
				between = append(between, d)
			}
		}
	}
	if len(between) == 0 {
		return false
	}
	return baseMean(between)-baseMean(within) >= baseDayGap
}

// baseOneGroup setzt alle vorhandenen Wochentage in Gruppe 0.
func baseOneGroup(m [7][]float64) [7]int {
	var s [7]int
	for i := range s {
		s[i] = -1
		if m[i] != nil {
			s[i] = 0
		}
	}
	return s
}

// baseWorkWeekend setzt Montag bis Freitag in Gruppe 0, Samstag und Sonntag in Gruppe 1.
func baseWorkWeekend(m [7][]float64) [7]int {
	s := baseOneGroup(m)
	for i := 5; i < 7; i++ {
		if s[i] >= 0 {
			s[i] = 1
		}
	}
	return s
}

// baseGroups listet die Wochentage je Gruppe, Montag zuerst.
func baseGroups(split [7]int) [][]time.Weekday {
	n := 0
	for _, g := range split {
		if g+1 > n {
			n = g + 1
		}
	}
	groups := make([][]time.Weekday, n)
	for i, g := range split {
		if g >= 0 {
			groups[g] = append(groups[g], time.Weekday((i+1)%7))
		}
	}
	return groups
}

// baseProfiles liefert je Gruppe das Medianprofil ihrer Tage.
func baseProfiles(recs []baseRec, sample []int, split [7]int, n int) [][]float64 {
	members := make([][]int, n)
	for _, i := range sample {
		if g := split[recs[i].wd]; g >= 0 {
			members[g] = append(members[g], i)
		}
	}
	out := make([][]float64, n)
	for g := range members {
		out[g] = baseMedianOf(recs, members[g])
	}
	return out
}

// baseAssign ordnet einen Tag seiner Gruppe zu. Der heutige Tag gilt bis Mittag
// als sein Wochentag, danach zählt der Vormittag. Sonst entscheidet das
// nächste Profil.
func baseAssign(r *baseRec, split [7]int, profiles [][]float64, now time.Time) int {
	switch {
	case r.today && now.Hour() < baseNoon:
		return split[r.wd]
	case r.today:
		if !r.morn {
			return -1
		}
		return baseNearest(r.load[:baseMorning], profiles)
	case r.full:
		return baseNearest(r.prof[:], profiles)
	}
	return -1
}

// baseNearest liefert die Gruppe des Profils, das dem Vektor am nächsten liegt.
func baseNearest(vec []float64, profiles [][]float64) int {
	best, bestD := -1, math.Inf(1)
	for g, p := range profiles {
		if d := 1 - baseCorr(vec, p); d < bestD {
			best, bestD = g, d
		}
	}
	return best
}

// baseAway markiert die Abwesenheit. Ein Tag ist still, wenn Energie über der
// Grundlast, Handgriffe und Unruhe alle mehr als 2,5 Streuungen unter dem
// Median seines Tagestyps liegen und kein Handgriff kam. Beginnt eine Folge
// stiller Tage, gilt ab 36 stillen Stunden die ganze Folge. Die Auflösung ist
// ein Tag, zwei stille Tage sind also 48 Stunden.
func baseAway(recs []baseRec, base *Base) {
	met := make([]baseMetric, len(recs))
	var eval []int
	byGroup := make([][]int, len(base.Groups))
	for i := range recs {
		r := &recs[i]
		if r.group < 0 || !r.full || r.today {
			continue
		}
		met[i] = baseMetricOf(r)
		eval = append(eval, i)
		byGroup[r.group] = append(byGroup[r.group], i)
	}
	lim := make([][3]baseSpan, len(base.Groups))
	for g, members := range byGroup {
		if len(members) == 0 {
			continue
		}
		var vals [3][]float64
		for _, i := range members {
			vals[0] = append(vals[0], met[i].e)
			vals[1] = append(vals[1], met[i].h)
			vals[2] = append(vals[2], met[i].u)
		}
		for k := range vals {
			lim[g][k] = baseSpan{med: Median(vals[k]), sigma: MADScale(vals[k])}
		}
	}
	// Gleich dem Abstand zählt mit, sonst fiele eine Gruppe ohne Streuung und
	// ohne Handgriffe (Median 0) nie als still aus.
	quiet := func(i int) bool {
		m := met[i]
		l := lim[recs[i].group]
		return m.h == 0 &&
			m.e <= l[0].med-baseQuietSigma*l[0].sigma &&
			m.h <= l[1].med-baseQuietSigma*l[1].sigma &&
			m.u <= l[2].med-baseQuietSigma*l[2].sigma
	}
	var run []int
	flush := func() {
		if float64(len(run))*24 >= baseQuietHours {
			for _, i := range run {
				base.Away[recs[i].date] = true
			}
		}
		run = run[:0]
	}
	for _, i := range eval {
		if !quiet(i) {
			flush()
			continue
		}
		if len(run) > 0 && !recs[run[len(run)-1]].t.AddDate(0, 0, 1).Equal(recs[i].t) {
			flush()
		}
		run = append(run, i)
	}
	flush()
}

// baseMetricOf misst die drei Größen der Abwesenheit an einem Tag.
func baseMetricOf(r *baseRec) baseMetric {
	return baseMetricOver(r.load[:], r.hand[:])
}

// baseMetricOver misst die drei Größen über einer Folge von Viertelstunden,
// einem Tag oder einem Fenster. Die Grundlast ist das 10-Prozent-Quantil der Folge.
func baseMetricOver(load, hand []float64) baseMetric {
	floor := basePercentile10(load)
	var m baseMetric
	for _, l := range load {
		if l > floor {
			m.e += l - floor
		}
	}
	m.e *= baseWattQuarterKWh
	for _, h := range hand {
		m.h += h
	}
	for q := 1; q < len(load); q++ {
		m.u += math.Abs(load[q] - load[q-1])
	}
	return m
}

// houseWindowQuarters is the window of HouseState, the last three hours.
const houseWindowQuarters = 12

// houseMinDays is the number of comparison days HouseState needs before it is ok.
const houseMinDays = 7

// HouseState tells whether the house was quiet over the last three hours. It
// measures the three quantities of baseAway over that window and compares them
// with the same window on the days of the day type of today. The state is
// quiet when all three lie more than baseQuietSigma spreads below the median
// and no hand changed a device in the window, otherwise active. ok is false
// without today's data or with fewer than houseMinDays comparison days.
func HouseState(in *Input) (state string, n int, ok bool) {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	g, has := learnTodayGroup(in)
	if !has {
		return "", 0, false
	}
	now := in.Now.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	q := now.Hour()*4 + now.Minute()/int(baseQuarterMin)
	byDate := map[string]*DayFile{}
	for i := range in.Days {
		byDate[in.Days[i].Day] = &in.Days[i]
	}
	cur, ok := houseWindowOf(byDate, day, q)
	if !ok {
		return "", 0, false
	}
	var es, hs, us []float64
	for i := range in.Days {
		f := &in.Days[i]
		if f.Day == day.Format(dayLayout) || in.Base.DayType[f.Day] != g {
			continue
		}
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		m, ok := houseWindowOf(byDate, t, q)
		if !ok {
			continue
		}
		es = append(es, m.e)
		hs = append(hs, m.h)
		us = append(us, m.u)
	}
	if len(es) < houseMinDays {
		return "", len(es), false
	}
	below := func(v float64, xs []float64) bool {
		return v < Median(xs)-baseQuietSigma*MADScale(xs)
	}
	quiet := cur.h == 0 && below(cur.e, es) && below(cur.h, hs) && below(cur.u, us)
	if quiet {
		return "quiet", len(es), true
	}
	return "active", len(es), true
}

// houseWindowOf measures the window of HouseState that ends before quarter q of
// the day. The quarters before midnight come from the day before. ok is false
// when a quarter of the window was not seen.
func houseWindowOf(byDate map[string]*DayFile, day time.Time, q int) (baseMetric, bool) {
	load := make([]float64, 0, houseWindowQuarters)
	hand := make([]float64, 0, houseWindowQuarters)
	for k := q - houseWindowQuarters; k < q; k++ {
		d, idx := day, k
		if k < 0 {
			d, idx = day.AddDate(0, 0, -1), k+dayQuarters
		}
		f := byDate[d.Format(dayLayout)]
		if f == nil || f.Site.Seen[idx] == 0 {
			return baseMetric{}, false
		}
		var h float64
		for _, dd := range f.Devices {
			if dd != nil {
				h += float64(dd.Hand[idx])
			}
		}
		load = append(load, float64(f.Site.LoadTotal[idx]))
		hand = append(hand, h)
	}
	return baseMetricOver(load, hand), true
}

// baseBounds setzt je Tagestyp den Kreismedian von Tagesbeginn und Nachtruhe.
func baseBounds(recs []baseRec, base *Base) {
	starts := map[int][]float64{}
	ends := map[int][]float64{}
	for i := range recs {
		r := &recs[i]
		if r.group < 0 || !r.full || r.today {
			continue
		}
		s, e, ok := baseDayBounds(r)
		if !ok {
			continue
		}
		starts[r.group] = append(starts[r.group], s)
		ends[r.group] = append(ends[r.group], e)
	}
	for g, v := range starts {
		base.DayStart[g] = CircMedian(v)
	}
	for g, v := range ends {
		base.NightRest[g] = CircMedian(v)
	}
}

// baseDayBounds findet Tagesbeginn und Nachtruhe eines Tages in Minuten. Die
// Last liegt für zwei Viertelstunden über Nachtlast + max(50 W, 3 Streuungen).
// Der Beginn ist die erste solche Viertelstunde oder der erste Handgriff, je
// nachdem was früher kommt. Die Nachtruhe spiegelt das vom Abend her.
func baseDayBounds(r *baseRec) (start, end float64, ok bool) {
	night := r.load[:baseNight]
	thr := basePercentile10(night) + math.Max(50, 3*MADScale(night))
	first, last := -1, -1
	for q := 0; q+1 < dayQuarters && first < 0; q++ {
		if r.load[q] > thr && r.load[q+1] > thr {
			first = q
		}
	}
	for q := dayQuarters - 1; q >= 1 && last < 0; q-- {
		if r.load[q] > thr && r.load[q-1] > thr {
			last = q
		}
	}
	hf, hl := -1, -1
	for q, h := range r.hand {
		if h > 0 {
			if hf < 0 {
				hf = q
			}
			hl = q
		}
	}
	start, end = math.Inf(1), math.Inf(-1)
	if first >= 0 {
		start = float64(first) * baseQuarterMin
		end = float64(last+1) * baseQuarterMin
	}
	if hf >= 0 {
		start = math.Min(start, float64(hf)*baseQuarterMin)
		end = math.Max(end, float64(hl+1)*baseQuarterMin)
	}
	if math.IsInf(start, 1) {
		return 0, 0, false
	}
	return start, end, true
}

// baseMedians liefert je wochentag das Medianprofil der Tage in idx. Fehlt ein
// Wochentag, bleibt sein Eintrag nil.
func baseMedians(recs []baseRec, idx []int) [7][]float64 {
	var by [7][]int
	for _, i := range idx {
		by[recs[i].wd] = append(by[recs[i].wd], i)
	}
	var out [7][]float64
	for w := range by {
		if len(by[w]) > 0 {
			out[w] = baseMedianOf(recs, by[w])
		}
	}
	return out
}

// baseMedianOf liefert je Viertelstunde den Median der normierten Profile der Tage in idx.
func baseMedianOf(recs []baseRec, idx []int) []float64 {
	med := make([]float64, dayQuarters)
	col := make([]float64, len(idx))
	for q := 0; q < dayQuarters; q++ {
		for k, i := range idx {
			col[k] = recs[i].prof[q]
		}
		med[q] = Median(col)
	}
	return med
}

// baseCorr ist die Pearson-Korrelation der ersten n Werte beider Reihen. Ohne
// Streuung ist sie 0.
func baseCorr(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var ma, mb float64
	for i := 0; i < n; i++ {
		ma += a[i]
		mb += b[i]
	}
	ma /= float64(n)
	mb /= float64(n)
	var sab, saa, sbb float64
	for i := 0; i < n; i++ {
		da, db := a[i]-ma, b[i]-mb
		sab += da * db
		saa += da * da
		sbb += db * db
	}
	if saa == 0 || sbb == 0 {
		return 0
	}
	return sab / math.Sqrt(saa*sbb)
}

// basePercentile10 liefert das 10. Perzentil nach dem Rang-Verfahren.
func basePercentile10(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	k := int(math.Ceil(0.1*float64(len(s)))) - 1
	if k < 0 {
		k = 0
	}
	return s[k]
}

// baseAvgDist ist der mittlere Abstand zwischen den Wochentagen zweier Gruppen.
func baseAvgDist(dist [7][7]float64, a, b []int) float64 {
	sum := 0.0
	for _, i := range a {
		for _, j := range b {
			sum += dist[i][j]
		}
	}
	return sum / float64(len(a)*len(b))
}

// baseCopy kopiert die Gruppen tief, damit Schnappschüsse nicht geteilt werden.
func baseCopy(clusters [][]int) [][]int {
	out := make([][]int, len(clusters))
	for k, c := range clusters {
		out[k] = append([]int(nil), c...)
	}
	return out
}

// baseIDs nummert die Gruppen in der Reihenfolge ihres frühesten Wochentags,
// Montag zuerst. Wochentage ohne Gruppe bekommen -1.
func baseIDs(clusters [][]int) [7]int {
	var out [7]int
	for i := range out {
		out[i] = -1
	}
	sort.Slice(clusters, func(a, b int) bool { return baseMin(clusters[a]) < baseMin(clusters[b]) })
	for id, c := range clusters {
		for _, i := range c {
			out[i] = id
		}
	}
	return out
}

// baseMin liefert den kleinsten Wert einer nicht leeren Liste.
func baseMin(xs []int) int {
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

// baseMean liefert den Mittelwert, für eine leere Liste 0.
func baseMean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}
