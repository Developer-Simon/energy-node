package autolearn

import "time"

// Value kinds. A time value carries Text as "HH:MM", a state value carries its
// state in Text, and a duration value carries its seconds in Num.
const (
	ValueTime     = "time"
	ValueDuration = "duration"
	ValueState    = "state"
)

// Value is a learned number, clock time or state. It is published under Key
// as a retained topic and feeds the rules that reference it. N is the number
// of days or runs the value rests on.
type Value struct {
	Key, Device, Kind, Group string
	Num                      float64
	Text                     string
	N                        int
}

// learnTodayGroup returns the day type group of the weekday of in.Now. A
// learned value describes the day of today, so the group comes from the
// weekday and not from the day profiles of today.
func learnTodayGroup(in *Input) (int, bool) {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	wd := in.Now.In(loc).Weekday()
	for g, days := range in.Base.Groups {
		for _, d := range days {
			if d == wd {
				return g, true
			}
		}
	}
	return 0, false
}
