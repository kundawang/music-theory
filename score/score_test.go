package score

import (
	"math"
	"testing"

	"github.com/go-music-theory/music-theory/chord"
	"github.com/go-music-theory/music-theory/note"
	"gopkg.in/stretchr/testify.v1/assert"
	
)

// The fixed example score: 4/4 for bars 1-2, 7/8 for bars 3-4, 3/4 from bar 5.
//
//	bar 1-2:  4/4  1920 ticks each
//	bar 3-4:  7/8  1680 ticks each
//	bar 5+:   3/4  1440 ticks each
//
//	bar 3 starts at tick 3840
//	bar 5 starts at tick 7200
//	bar 9 starts at tick 12960
func newFixture(t *testing.T) *Timeline {
	tl, err := NewTimeline(Meter{Beats: 4, Unit: 4})
	assert.NoError(t, err)
	assert.NoError(t, tl.AddMeterChange(3, Meter{Beats: 7, Unit: 8}))
	assert.NoError(t, tl.AddMeterChange(5, Meter{Beats: 3, Unit: 4}))
	return tl
}

func TestTickOf_FixedScore(t *testing.T) {
	tl := newFixture(t)

	cases := []struct {
		pos  Position
		tick int64
	}{
		{Position{Bar: 1, Beat: 1}, 0},
		{Position{Bar: 1, Beat: 4}, 1440},
		{Position{Bar: 2, Beat: 4, Num: 1, Den: 2}, 1920 + 3*480 + 240},
		{Position{Bar: 3, Beat: 1}, 3840},
		{Position{Bar: 3, Beat: 7}, 3840 + 6*240},
		{Position{Bar: 4, Beat: 1, Num: 1, Den: 3}, 3840 + 1680 + 80},
		{Position{Bar: 5, Beat: 1}, 7200},
		{Position{Bar: 9, Beat: 3}, 13920}, // 第九小节第三拍
		{Position{Bar: 9, Beat: 3, Num: 1, Den: 2}, 13920 + 240},
		{Position{Bar: 1000, Beat: 2, Num: 1, Den: 4}, 7200 + 995*1440 + 480 + 120},
	}
	for _, c := range cases {
		tick, err := tl.TickOf(c.pos)
		assert.NoError(t, err, "TickOf(%s)", c.pos)
		assert.Equal(t, c.tick, tick, "TickOf(%s)", c.pos)
	}
}

func TestRoundTrip_FixedScore(t *testing.T) {
	tl := newFixture(t)

	positions := []Position{
		{Bar: 1, Beat: 1},
		{Bar: 2, Beat: 4, Num: 1, Den: 2},
		{Bar: 3, Beat: 7},
		{Bar: 4, Beat: 1, Num: 1, Den: 3},
		{Bar: 5, Beat: 3},
		{Bar: 9, Beat: 3},
		{Bar: 9, Beat: 3, Num: 1, Den: 2},
		{Bar: 1000, Beat: 2, Num: 1, Den: 4},
	}
	for _, p := range positions {
		tick, err := tl.TickOf(p)
		assert.NoError(t, err, "TickOf(%s)", p)
		back, err := tl.PositionOf(tick)
		assert.NoError(t, err, "PositionOf(%d)", tick)
		assert.Equal(t, p, back, "round trip of %s via tick %d", p, tick)
	}
}

func TestPositionOf_FixedScore(t *testing.T) {
	tl := newFixture(t)

	p, err := tl.PositionOf(13920)
	assert.NoError(t, err)
	assert.Equal(t, Position{Bar: 9, Beat: 3}, p)

	p, err = tl.PositionOf(0)
	assert.NoError(t, err)
	assert.Equal(t, Position{Bar: 1, Beat: 1}, p)
}

func TestEvents_StableOrderAtSameTick(t *testing.T) {
	tl := newFixture(t)

	assert.NoError(t, tl.Place(Position{Bar: 9, Beat: 3}, chord.Of("G7")))
	assert.NoError(t, tl.Place(Position{Bar: 1, Beat: 1}, chord.Of("C")))
	assert.NoError(t, tl.Place(Position{Bar: 9, Beat: 3}, chord.Of("Am"))) // same position as G7, placed later
	assert.NoError(t, tl.Place(Position{Bar: 3, Beat: 1}, chord.Of("F")))

	events, err := tl.Events()
	assert.NoError(t, err)
	assert.Len(t, events, 4)

	assert.Equal(t, note.C, events[0].Chord.Root)
	assert.Equal(t, int64(0), events[0].Tick)

	assert.Equal(t, note.F, events[1].Chord.Root)
	assert.Equal(t, int64(3840), events[1].Tick)

	// Same tick keeps insertion order: G7 before Am.
	assert.Equal(t, note.G, events[2].Chord.Root)
	assert.Equal(t, int64(13920), events[2].Tick)
	assert.Equal(t, note.A, events[3].Chord.Root)
	assert.Equal(t, int64(13920), events[3].Tick)
}

func TestMeterChange_RemapsLaterPositions(t *testing.T) {
	tl := newFixture(t)
	assert.NoError(t, tl.Place(Position{Bar: 9, Beat: 3}, chord.Of("G7")))

	before, err := tl.TickOf(Position{Bar: 9, Beat: 3})
	assert.NoError(t, err)
	assert.Equal(t, int64(13920), before)

	// Insert 5/8 for bars 7-8 only (3/4 resumes at bar 9).
	assert.NoError(t, tl.AddMeterChange(7, Meter{Beats: 5, Unit: 8}))
	assert.NoError(t, tl.AddMeterChange(9, Meter{Beats: 3, Unit: 4}))

	after, err := tl.TickOf(Position{Bar: 9, Beat: 3})
	assert.NoError(t, err)
	assert.Equal(t, int64(7200+2*1440+2*1200+960), after) // 13440
	assert.NotEqual(t, before, after)

	// Earlier positions are untouched.
	early, err := tl.TickOf(Position{Bar: 5, Beat: 3})
	assert.NoError(t, err)
	assert.Equal(t, int64(7200+2*480), early)

	// The placed event re-maps too.
	events, err := tl.Events()
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, int64(13440), events[0].Tick)

	// And the round trip still holds across all three meters.
	back, err := tl.PositionOf(after)
	assert.NoError(t, err)
	assert.Equal(t, Position{Bar: 9, Beat: 3}, back)
}

func TestMeter_Invalid(t *testing.T) {
	_, err := NewTimeline(Meter{Beats: 0, Unit: 4})
	assert.Error(t, err, "zero beats per bar")

	_, err = NewTimeline(Meter{Beats: -1, Unit: 4})
	assert.Error(t, err, "negative beats per bar")

	_, err = NewTimeline(Meter{Beats: 4, Unit: 0})
	assert.Error(t, err, "zero unit")

	_, err = NewTimeline(Meter{Beats: 4, Unit: 3})
	assert.Error(t, err, "unit not a power of two")

	_, err = NewTimeline(Meter{Beats: 1, Unit: 512})
	assert.Error(t, err, "unit does not divide the tick resolution evenly")

	tl := newFixture(t)
	assert.Error(t, tl.AddMeterChange(4, Meter{Beats: 6, Unit: 5}), "illegal denominator on change")
}

func TestAddMeterChange_Overlap(t *testing.T) {
	tl := newFixture(t)

	assert.Error(t, tl.AddMeterChange(3, Meter{Beats: 2, Unit: 2}), "segment already begins at bar 3")
	assert.Error(t, tl.AddMeterChange(1, Meter{Beats: 2, Unit: 2}), "bar 1 is the initial meter")
	assert.Error(t, tl.AddMeterChange(0, Meter{Beats: 2, Unit: 2}), "negative bar")
}

func TestTickOf_InvalidPosition(t *testing.T) {
	tl := newFixture(t)

	_, err := tl.TickOf(Position{Bar: 0, Beat: 1})
	assert.Error(t, err, "zero bar")

	_, err = tl.TickOf(Position{Bar: -3, Beat: 1})
	assert.Error(t, err, "negative bar")

	_, err = tl.TickOf(Position{Bar: 1, Beat: 0})
	assert.Error(t, err, "zero beat")

	_, err = tl.TickOf(Position{Bar: 1, Beat: 1, Num: -1, Den: 2})
	assert.Error(t, err, "negative fraction")

	_, err = tl.TickOf(Position{Bar: 1, Beat: 1, Num: 1, Den: 0})
	assert.Error(t, err, "zero denominator")

	_, err = tl.TickOf(Position{Bar: 1, Beat: 1, Num: 3, Den: 2})
	assert.Error(t, err, "fraction of one beat or more")

	_, err = tl.TickOf(Position{Bar: 3, Beat: 8})
	assert.Error(t, err, "beat 8 does not exist in 7/8")

	_, err = tl.TickOf(Position{Bar: 1, Beat: 1, Num: 1, Den: 7})
	assert.Error(t, err, "1/7 of a 480-tick beat is not a whole tick")
}

func TestPositionOf_NegativeTick(t *testing.T) {
	tl := newFixture(t)
	_, err := tl.PositionOf(-1)
	assert.Error(t, err)
}

func TestEvents_ErrorAfterMeterChangeInvalidatesPosition(t *testing.T) {
	tl := newFixture(t)
	assert.NoError(t, tl.Place(Position{Bar: 4, Beat: 5}, chord.Of("Dm"))) // valid in 7/8

	// Bar 4 becomes 2/4: beat 5 no longer exists there.
	assert.NoError(t, tl.AddMeterChange(4, Meter{Beats: 2, Unit: 4}))

	_, err := tl.Events()
	assert.Error(t, err, "placed position no longer resolves")
}

func TestVeryLongScore(t *testing.T) {
	tl := newFixture(t)

	// A very long score still resolves and round-trips.
	far := Position{Bar: 1 << 40, Beat: 3}
	tick, err := tl.TickOf(far)
	assert.NoError(t, err)
	back, err := tl.PositionOf(tick)
	assert.NoError(t, err)
	assert.Equal(t, far, back)

	// Beyond the int64 tick range the overflow is reported, not wrapped.
	_, err = tl.TickOf(Position{Bar: math.MaxInt32 << 22, Beat: 1})
	assert.Equal(t, ErrOverflow, err)
}
