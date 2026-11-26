package usecase

import "testing"

func TestDiceRoll(t *testing.T) {
	d := &Dice{random: func(n int64) int64 { return n - 1 }}
	for _, tc := range []struct{ input, want string }{
		{"２Ｄ６+３", "15 (2d6+3)"},
		{"1d1+2", "3 (1d1+2)"},
	} {
		got, err := d.DiceRoll(tc.input)
		if err != nil || got.GetResult() != tc.want {
			t.Fatalf("%q: got %v, %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"1d0", "1dd6", "abc", "1d6+"} {
		if _, err := d.DiceRoll(input); err == nil {
			t.Errorf("%q: expected error", input)
		}
	}
}
