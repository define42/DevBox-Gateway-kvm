package console

import (
	"math"
	"testing"
)

func TestStrtol(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, input string
		want        int64
	}{
		{"empty", "", 0},
		{"invalid", "word 12", 0},
		{"sign only", "+", 0},
		{"whitespace and suffix", " \t\n\v\f\r+42xyz", 42},
		{"negative", "-123.5", -123},
		{"not hexadecimal", "0x10", 0},
		{"maximum", "9223372036854775807", math.MaxInt64},
		{"minimum", "-9223372036854775808", math.MinInt64},
		{"positive saturation", "9223372036854775808", math.MaxInt64},
		{"negative saturation", "-9223372036854775809", math.MinInt64},
		{"unsigned overflow", "9999999999999999999999999999999", math.MaxInt64},
		{"negative unsigned overflow", "-9999999999999999999999999999999", math.MinInt64},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Strtol(tt.input); got != tt.want {
				t.Fatalf("Strtol(%q) = %d, want %d", tt.input, got, tt.want)
			}
			if got := Atol(tt.input); got != tt.want {
				t.Fatalf("Atol(%q) = %d, want %d", tt.input, got, tt.want)
			}
			if got := Atoi(tt.input); got != int32(tt.want) {
				t.Fatalf("Atoi(%q) = %d, want %d", tt.input, got, int32(tt.want))
			}
		})
	}
}

func TestAtof(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		input string
		want  float64
	}{
		{"", 0}, {"word", 0}, {"+.", 0}, {" \t\n\v\f\r-12.5tail", -12.5},
		{".25", .25}, {"1.", 1}, {"1.25e+2suffix", 125}, {"2E-2", .02},
		{"3e+", 3}, {"4e-foo", 4}, {"5e", 5}, {"-0", math.Copysign(0, -1)},
		{"0x1.8", 1.5}, {"-0x1.8p+2tail", -6}, {"0x", 0}, {"0xg", 0},
		{"0x1p99999", math.Inf(1)}, {"1e99999", math.Inf(1)}, {"1e-99999", 0},
		{"INFtail", math.Inf(1)}, {"-infinitytail", math.Inf(-1)}, {"+Inf", math.Inf(1)},
		{"NaN(payload)", math.NaN()}, {"-nan", math.NaN()},
	} {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := Atof(tt.input)
			if math.IsNaN(tt.want) {
				if !math.IsNaN(got) {
					t.Fatalf("Atof(%q) = %g, want NaN", tt.input, got)
				}
				return
			}
			if got != tt.want || math.Signbit(got) != math.Signbit(tt.want) {
				t.Fatalf("Atof(%q) = %g, want %g", tt.input, got, tt.want)
			}
		})
	}
}
