package mathutil

import "testing"

func TestCeilDiv(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{25, 10, 3},
		{20, 10, 2},
		{0, 8, 0},
		{17, 8, 3},
		{1, 8, 1},
	}
	for _, c := range cases {
		if got := CeilDiv(c.a, c.b); got != c.want {
			t.Errorf("CeilDiv(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCeilDivInt64(t *testing.T) {
	if got := CeilDiv(int64(25), int64(10)); got != 3 {
		t.Errorf("CeilDiv(25, 10) = %d, want 3", got)
	}
}
