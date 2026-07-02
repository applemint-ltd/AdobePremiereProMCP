package grpc

import "testing"

func TestTimecodeToSeconds(t *testing.T) {
	cases := []struct {
		name string
		tc   Timecode
		want float64
	}{
		{
			name: "zero",
			tc:   Timecode{},
			want: 0,
		},
		{
			name: "whole seconds only",
			tc:   Timecode{Hours: 0, Minutes: 0, Seconds: 24, Frames: 0, FrameRate: 24},
			want: 24,
		},
		{
			name: "seconds and frames",
			tc:   Timecode{Hours: 0, Minutes: 0, Seconds: 1, Frames: 12, FrameRate: 24},
			want: 1.5,
		},
		{
			name: "hours minutes seconds",
			tc:   Timecode{Hours: 1, Minutes: 2, Seconds: 3, Frames: 0, FrameRate: 30},
			want: 3723,
		},
		{
			name: "zero frame rate does not divide by zero",
			tc:   Timecode{Seconds: 5, Frames: 10, FrameRate: 0},
			want: 5,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := timecodeToSeconds(c.tc)
			if got != c.want {
				t.Errorf("timecodeToSeconds(%+v) = %v, want %v", c.tc, got, c.want)
			}
		})
	}
}
