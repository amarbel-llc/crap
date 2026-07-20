package main

import (
	"reflect"
	"testing"

	crap "code.linenisgreat.com/crap/go-crap/v2"
)

func TestParseExecArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantOpts crap.ExecOptions
		wantCmd  []string
		wantErr  bool
	}{
		{
			name:     "defaults",
			args:     []string{"ls", "-la"},
			wantOpts: crap.ExecOptions{TP: 1},
			wantCmd:  []string{"ls", "-la"},
		},
		{
			name:     "tp and name with separator",
			args:     []string{"--tp", "3", "--name", "dodder", "--", "just", "_update-repo-full", "/path", "abc123"},
			wantOpts: crap.ExecOptions{TP: 3, Name: "dodder"},
			wantCmd:  []string{"just", "_update-repo-full", "/path", "abc123"},
		},
		{
			name:     "wrapped command flags pass through without separator",
			args:     []string{"--tp", "2", "go", "test", "-run", "TestFoo"},
			wantOpts: crap.ExecOptions{TP: 2},
			wantCmd:  []string{"go", "test", "-run", "TestFoo"},
		},
		{
			name:    "missing command",
			args:    []string{"--tp", "5"},
			wantErr: true,
		},
		{
			name:    "no args",
			args:    []string{},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, cmd, err := parseExecArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got opts=%#v cmd=%#v", opts, cmd)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if opts != tc.wantOpts {
				t.Fatalf("opts: got %#v want %#v", opts, tc.wantOpts)
			}
			if !reflect.DeepEqual(cmd, tc.wantCmd) {
				t.Fatalf("cmd: got %#v want %#v", cmd, tc.wantCmd)
			}
		})
	}
}
