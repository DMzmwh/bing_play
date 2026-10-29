package service

import (
	"path/filepath"
	"testing"
)

func TestSeriesGroupName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Show 第01集", "Show"},
		{"Show 第一话", "Show"},
		{"Show 01集", "Show"},
		{"Show.第12期", "Show"},
		{"Show.S01E01.1080p", "Show"},
		{"Show - S01E02", "Show"},
		{"Show EP01", "Show"},
		{"Show - E01", "Show"},
		{"Show - 01", "Show"},
		{"Show.01", "Show"},
		{"Show [01]", "Show"},
		{"Show (S01E01)", "Show"},
		{"Show Season 1", "Show Season 1"},
		{"Show Part 2", "Show Part 2"},
		{"Show 2024", "Show 2024"},
		{"第01集", ""},
		{"01", ""},
	}

	for _, c := range cases {
		if got := seriesGroupName(c.in); got != c.want {
			t.Errorf("seriesGroupName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGroupNameForLocalVideo(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{filepath.Join("root", "Downloads", "Show.S01E01.mkv"), "Show"},
		{filepath.Join("root", "Show", "Show.第01集.mkv"), "Show"},
		{filepath.Join("root", "Show Season 1", "Show.S01E01.mkv"), "Show"},
		{filepath.Join("root", "Show", "01.mkv"), "Show"},
		{filepath.Join("root", "Show 第01集", "01.mkv"), "Show"},
		{"01.mkv", "01"},
	}

	for _, c := range cases {
		if got := groupNameForLocalVideo(c.path, filepath.Base(c.path)); got != c.want {
			t.Errorf("groupNameForLocalVideo(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
