package driver

import "time"

// CalendarDate is a civil date.
type CalendarDate struct {
	Year  int
	Month time.Month
	Day   int
}

// CalendarMonth is a civil month.
type CalendarMonth struct {
	Year  int
	Month time.Month
}

// CalendarReference is the immutable civil reference of one acquisition.
type CalendarReference interface {
	Instant() time.Time
	Date() CalendarDate
	Year() int
	PreviousYear() int
	Month() CalendarMonth
	PreviousMonth() CalendarMonth
}

// CalendarAcquisition is one immutable acquisition of the installation calendar.
// Check applies the same-calendar and civil-day guard against a calendar.
type CalendarAcquisition interface {
	Reference() CalendarReference
	Check(Calendar) error
}

// Calendar is the installation calendar service. A driver uses it for native
// date-dependent queries only, never as a freshness or retention clock or as an
// identity. The contract supplies no location, clock default or scheduler.
type Calendar interface {
	BeginAcquisition() CalendarAcquisition
}
