//go:build windows

package main

import "testing"

func TestValidMountpoint(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "X:", want: true},
		{value: "m:", want: true},
		{value: "X", want: false},
		{value: "XX:", want: false},
		{value: "1:", want: false},
	}

	for _, test := range tests {
		if got := validMountpoint(test.value); got != test.want {
			t.Errorf("validMountpoint(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}
