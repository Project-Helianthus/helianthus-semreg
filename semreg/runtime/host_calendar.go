package runtime

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// hostCalendar is the installation calendar service. It resolves its location
// once and reads the installation clock; it never owns freshness, evidence
// timestamps or identity. A fresh service has a fresh generation even when the
// location name is unchanged.
type hostCalendar struct {
	location   *time.Location
	clock      func() time.Time
	generation *byte
}

var _ driver.Calendar = (*hostCalendar)(nil)

// newHostCalendar resolves the location once. A nil explicit location takes the
// zone the process already resolved (time.Local); no environment value or path
// is ever handed to time.LoadLocation. An explicit value must be an IANA
// location name; an empty explicit value is an error, never a request for the
// default, and an invalid one is reported with the configured value.
func newHostCalendar(explicit *string, clock func() time.Time) (*hostCalendar, error) {
	if clock == nil {
		return nil, errors.New("calendar requires a clock")
	}
	if explicit == nil {
		return &hostCalendar{location: time.Local, clock: clock, generation: new(byte)}, nil
	}
	name := *explicit
	if name == "" || strings.TrimSpace(name) != name ||
		(name != "UTC" && !strings.Contains(name, "/")) ||
		strings.HasPrefix(name, "Etc/GMT") {
		return nil, fmt.Errorf("calendar requires an IANA location, not an offset or abbreviation: %q", name)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load calendar location %q: %w", name, err)
	}
	return &hostCalendar{location: location, clock: clock, generation: new(byte)}, nil
}

type calendarReference struct {
	instant time.Time
	date    driver.CalendarDate
}

func (r calendarReference) Instant() time.Time        { return r.instant }
func (r calendarReference) Date() driver.CalendarDate { return r.date }
func (r calendarReference) Year() int                 { return r.date.Year }
func (r calendarReference) PreviousYear() int         { return r.date.Year - 1 }
func (r calendarReference) Month() driver.CalendarMonth {
	return driver.CalendarMonth{Year: r.date.Year, Month: r.date.Month}
}
func (r calendarReference) PreviousMonth() driver.CalendarMonth {
	if r.date.Month == time.January {
		return driver.CalendarMonth{Year: r.date.Year - 1, Month: time.December}
	}
	return driver.CalendarMonth{Year: r.date.Year, Month: r.date.Month - 1}
}

func (c *hostCalendar) capture() calendarReference {
	instant := c.clock()
	year, month, day := instant.In(c.location).Date()
	return calendarReference{instant: instant, date: driver.CalendarDate{Year: year, Month: month, Day: day}}
}

// calendarAcquisition is a single-owner guard. Once invalidated it stays
// aborted even if the clock returns to the captured date.
type calendarAcquisition struct {
	owner     *hostCalendar
	reference calendarReference
	aborted   bool
}

// BeginAcquisition captures every civil component from one clock sample.
func (c *hostCalendar) BeginAcquisition() driver.CalendarAcquisition {
	return &calendarAcquisition{owner: c, reference: c.capture()}
}

func (a *calendarAcquisition) Reference() driver.CalendarReference { return a.reference }

// Check runs before each reply is accepted. A different calendar or any civil
// date change fails closed.
func (a *calendarAcquisition) Check(c driver.Calendar) error {
	if a == nil {
		return errors.New("missing acquisition")
	}
	if a.aborted {
		return errors.New("acquisition already aborted")
	}
	other, ok := c.(*hostCalendar)
	if !ok || other == nil || other.location == nil || other.clock == nil || other.generation != a.owner.generation ||
		other.capture().date != a.reference.date {
		a.aborted = true
		return errors.New("acquisition calendar changed")
	}
	return nil
}
