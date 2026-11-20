// A score Timeline maps musical positions (bar, beat, fraction of a beat)
// to absolute ticks across variable time signatures (meter changes), and
// arranges existing Chords at those positions.
//
// A Timeline is a sequence of meter Segments. Each Segment starts at a bar
// and runs until the next Segment begins (the last Segment is open-ended).
// Positions are stored musically (bar/beat/fraction), so inserting a meter
// change re-maps the absolute tick of every later position automatically.
//
// The package does not change the meaning of note.Note or chord.Chord; it
// only places a chord.Chord at a Position on the Timeline.
//
// # Credit
//
// Charney Kaye
// <hi@charneykaye.com>
// https://charneykaye.com
//
// XJ Music
// https://xj.io
package score

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/go-music-theory/music-theory/chord"
)

// TicksPerQuarter is the timeline resolution in ticks per quarter note (PPQ).
const TicksPerQuarter int64 = 480

// ticksPerWhole is the number of ticks in a whole note.
const ticksPerWhole = TicksPerQuarter * 4

// ErrOverflow is returned when a position or tick would exceed int64 range,
// e.g. an extremely long score.
var ErrOverflow = errors.New("score: position overflows the tick range")

// Meter is a time signature, e.g. Meter{Beats: 7, Unit: 8} is 7/8.
type Meter struct {
	Beats int // beats per bar (numerator), must be >= 1
	Unit  int // note value of one beat (denominator), must be a power of two
}

// Validate the meter: at least one beat per bar, a power-of-two unit, and a
// bar that divides evenly into whole ticks.
func (m Meter) Validate() error {
	if m.Beats <= 0 {
		return fmt.Errorf("score: meter must have at least 1 beat per bar, got %d", m.Beats)
	}
	if m.Unit <= 0 || m.Unit&(m.Unit-1) != 0 {
		return fmt.Errorf("score: meter unit must be a power of two, got %d", m.Unit)
	}
	if _, err := m.beatTicks(); err != nil {
		return err
	}
	return nil
}

// beatTicks of one beat, or an error if the unit does not divide the tick
// resolution evenly (e.g. 1/512 note at 480 PPQ).
func (m Meter) beatTicks() (int64, error) {
	if m.Unit <= 0 || ticksPerWhole%int64(m.Unit) != 0 {
		return 0, fmt.Errorf("score: meter unit %d does not divide %d ticks per whole note evenly", m.Unit, ticksPerWhole)
	}
	return ticksPerWhole / int64(m.Unit), nil
}

// barTicks of one whole bar in this meter.
func (m Meter) barTicks() (int64, error) {
	beat, err := m.beatTicks()
	if err != nil {
		return 0, err
	}
	if beat > math.MaxInt64/int64(m.Beats) {
		return 0, ErrOverflow
	}
	return beat * int64(m.Beats), nil
}

// Position is a musical location: a 1-based bar, a 1-based beat within the
// bar (in the meter governing that bar), and a fraction Num/Den of one beat.
// A zero Num (with Den 0 or 1) means exactly on the beat.
type Position struct {
	Bar  int // 1-based bar number
	Beat int // 1-based beat within the bar
	Num  int // numerator of the fraction of a beat past Beat
	Den  int // denominator of the fraction of a beat past Beat
}

// String formats the position, e.g. "9.3" or "9.3+1/2".
func (p Position) String() string {
	if p.Num > 0 {
		return fmt.Sprintf("%d.%d+%d/%d", p.Bar, p.Beat, p.Num, p.Den)
	}
	return fmt.Sprintf("%d.%d", p.Bar, p.Beat)
}

// validate the position in isolation (meter-independent).
func (p Position) validate() error {
	if p.Bar < 1 {
		return fmt.Errorf("score: bar must be >= 1, got %d", p.Bar)
	}
	if p.Beat < 1 {
		return fmt.Errorf("score: beat must be >= 1, got %d", p.Beat)
	}
	if p.Num < 0 || p.Den < 0 {
		return fmt.Errorf("score: beat fraction must not be negative, got %d/%d", p.Num, p.Den)
	}
	if p.Num > 0 && p.Den == 0 {
		return fmt.Errorf("score: beat fraction %d/%d has a zero denominator", p.Num, p.Den)
	}
	if p.Den > 0 && p.Num >= p.Den {
		return fmt.Errorf("score: beat fraction %d/%d must be less than one beat", p.Num, p.Den)
	}
	return nil
}

// Segment is one run of bars in a single Meter.
type Segment struct {
	Meter    Meter // time signature of the segment
	StartBar int   // 1-based bar at which the segment begins
	Bars     int   // number of bars in the segment; 0 means open-ended (to the end of the score)
}

// Event is a Chord placed at a Position on the Timeline.
type Event struct {
	Position Position    // musical position of the event
	Chord    chord.Chord // the chord sounding at the position
	Tick     int64       // absolute tick, resolved by Events()
	seq      int         // insertion order, stabilizes ties at one tick
}

// Timeline is a variable-meter score: meter segments plus placed events.
type Timeline struct {
	segments []Segment // sorted by StartBar, first starts at bar 1, last is open-ended
	events   []Event   // in insertion order
}

// NewTimeline starts a score whose initial meter (from bar 1) is m.
func NewTimeline(m Meter) (*Timeline, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &Timeline{
		segments: []Segment{{Meter: m, StartBar: 1, Bars: 0}},
	}, nil
}

// Segments returns a copy of the meter segments in order.
func (t *Timeline) Segments() []Segment {
	out := make([]Segment, len(t.segments))
	copy(out, t.segments)
	return out
}

// AddMeterChange inserts a meter change beginning at bar (bar must be >= 2;
// the initial meter is set by NewTimeline). The segment previously covering
// bar is truncated, and every later position re-maps to a new absolute tick.
// A change at a bar that already starts a segment is rejected as overlapping.
func (t *Timeline) AddMeterChange(bar int, m Meter) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if bar < 2 {
		return fmt.Errorf("score: meter change must begin at bar 2 or later, got bar %d", bar)
	}
	for _, s := range t.segments {
		if s.StartBar == bar {
			return fmt.Errorf("score: overlapping meter change, a segment already begins at bar %d", bar)
		}
	}
	seg := Segment{Meter: m, StartBar: bar}
	i := sort.Search(len(t.segments), func(i int) bool { return t.segments[i].StartBar > bar })
	t.segments = append(t.segments, Segment{})
	copy(t.segments[i+1:], t.segments[i:])
	t.segments[i] = seg
	t.reflow()
	return nil
}

// reflow recomputes each segment's bar count from the next segment's start.
func (t *Timeline) reflow() {
	for i := 0; i < len(t.segments)-1; i++ {
		t.segments[i].Bars = t.segments[i+1].StartBar - t.segments[i].StartBar
	}
	t.segments[len(t.segments)-1].Bars = 0 // open-ended
}

// segmentOf returns the index of the segment governing bar.
func (t *Timeline) segmentOf(bar int) int {
	i := sort.Search(len(t.segments), func(i int) bool { return t.segments[i].StartBar > bar })
	return i - 1
}

// startTick computes the absolute tick at which segment i begins.
func (t *Timeline) startTick(i int) (int64, error) {
	var tick int64
	for j := 0; j < i; j++ {
		barTicks, err := t.segments[j].Meter.barTicks()
		if err != nil {
			return 0, err
		}
		bars := int64(t.segments[j].Bars)
		if barTicks > 0 && bars > (math.MaxInt64-tick)/barTicks {
			return 0, ErrOverflow
		}
		tick += bars * barTicks
	}
	return tick, nil
}

// TickOf resolves a musical Position to its absolute tick.
func (t *Timeline) TickOf(p Position) (int64, error) {
	if err := p.validate(); err != nil {
		return 0, err
	}
	i := t.segmentOf(p.Bar)
	seg := t.segments[i]
	if p.Beat > seg.Meter.Beats {
		return 0, fmt.Errorf("score: beat %d does not exist in %d/%d at bar %d", p.Beat, seg.Meter.Beats, seg.Meter.Unit, p.Bar)
	}
	tick, err := t.startTick(i)
	if err != nil {
		return 0, err
	}
	barTicks, err := seg.Meter.barTicks()
	if err != nil {
		return 0, err
	}
	beatTicks, _ := seg.Meter.beatTicks() // validated by barTicks
	bars := int64(p.Bar - seg.StartBar)
	if barTicks > 0 && bars > (math.MaxInt64-tick)/barTicks {
		return 0, ErrOverflow
	}
	tick += bars * barTicks
	beats := int64(p.Beat - 1)
	if beatTicks > 0 && beats > (math.MaxInt64-tick)/beatTicks {
		return 0, ErrOverflow
	}
	tick += beats * beatTicks
	if p.Num > 0 {
		if (beatTicks*int64(p.Num))%int64(p.Den) != 0 {
			return 0, fmt.Errorf("score: fraction %d/%d of a %d-tick beat does not land on a whole tick", p.Num, p.Den, beatTicks)
		}
		frac := beatTicks * int64(p.Num) / int64(p.Den)
		if frac > math.MaxInt64-tick {
			return 0, ErrOverflow
		}
		tick += frac
	}
	return tick, nil
}

// PositionOf resolves an absolute tick to its musical Position. The beat
// fraction is reduced to lowest terms; an on-the-beat position has Num 0.
func (t *Timeline) PositionOf(tick int64) (Position, error) {
	if tick < 0 {
		return Position{}, fmt.Errorf("score: tick must not be negative, got %d", tick)
	}
	for i, seg := range t.segments {
		start, err := t.startTick(i)
		if err != nil {
			return Position{}, err
		}
		barTicks, err := seg.Meter.barTicks()
		if err != nil {
			return Position{}, err
		}
		last := i == len(t.segments)-1
		if !last && tick >= start+int64(seg.Bars)*barTicks {
			continue
		}
		if tick < start {
			return Position{}, fmt.Errorf("score: tick %d precedes segment at bar %d", tick, seg.StartBar)
		}
		beatTicks, _ := seg.Meter.beatTicks()
		offset := tick - start
		bar := seg.StartBar + int(offset/barTicks)
		offset %= barTicks
		beat := int(offset/beatTicks) + 1
		rem := offset % beatTicks
		if rem == 0 {
			return Position{Bar: bar, Beat: beat}, nil
		}
		g := gcd(rem, beatTicks)
		return Position{Bar: bar, Beat: beat, Num: int(rem / g), Den: int(beatTicks / g)}, nil
	}
	return Position{}, errors.New("score: no segment")
}

// Place arranges a Chord at a Position. The position must resolve to a tick
// under the current meter segments.
func (t *Timeline) Place(p Position, c chord.Chord) error {
	if _, err := t.TickOf(p); err != nil {
		return err
	}
	t.events = append(t.events, Event{Position: p, Chord: c, seq: len(t.events)})
	return nil
}

// Events returns all placed events ordered by absolute tick. Events sharing
// one tick keep their insertion order (stable). Tick is resolved against the
// current meter segments, so events placed before a meter change re-map.
// An error is returned if a meter change has made any placed position
// unresolvable (e.g. its beat no longer exists in the new meter).
func (t *Timeline) Events() ([]Event, error) {
	out := make([]Event, len(t.events))
	for i, e := range t.events {
		tick, err := t.TickOf(e.Position)
		if err != nil {
			return nil, fmt.Errorf("score: event at %s: %w", e.Position, err)
		}
		e.Tick = tick
		out[i] = e
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tick < out[j].Tick })
	return out, nil
}

// gcd of two positive integers.
func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
