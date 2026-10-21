package main

import "testing"

func TestAccountFromClientID(t *testing.T) {
	cases := []struct {
		cid  string
		want int
	}{
		{"growupchat_acc4", 4},
		{"growupchat_acc10", 10},
		{"growupchat_acc0", 0},
		{"growupchat_acc", 0},
		{"", 0},
		{"painel", 0},
		{"widget_123", 0},
		{"growupchat_accX", 0},
	}
	for _, c := range cases {
		if got := accountFromClientID(c.cid); got != c.want {
			t.Errorf("accountFromClientID(%q) = %d, quer %d", c.cid, got, c.want)
		}
	}
}
