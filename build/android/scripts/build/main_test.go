package main

import "testing"

func TestJavaMajorVersion(t *testing.T) {
	cases := []struct {
		output string
		want   int
	}{
		{`java version "1.8.0_291"`, 8},
		{`openjdk version "17.0.9" 2023-10-17`, 17},
		{`openjdk version "21.0.1" 2023-10-17 LTS`, 21},
		{`garbage`, 0},
	}

	for _, c := range cases {
		if got := javaMajorVersion(c.output); got != c.want {
			t.Errorf("javaMajorVersion(%q) = %d, want %d", c.output, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b []int
		want int
	}{
		{[]int{26, 3, 11579264}, []int{25, 2, 1}, 1},
		{[]int{26, 1}, []int{26, 1, 0}, 0},
		{[]int{25, 2}, []int{26, 0}, -1},
	}

	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if (got > 0) != (c.want > 0) || (got < 0) != (c.want < 0) {
			t.Errorf("compareVersions(%v, %v) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}
