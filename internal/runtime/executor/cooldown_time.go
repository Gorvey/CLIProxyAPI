package executor

import "time"

func timeUntilNextDayAt(now time.Time, loc *time.Location) (time.Duration, time.Time) {
	nowLocal := now.In(loc)
	tomorrow := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day()+1, 0, 0, 0, 0, loc)
	return tomorrow.Sub(now), tomorrow
}
