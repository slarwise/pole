package main

import "testing"

func TestMatchesPrompt(t *testing.T) {
	tests := map[string]struct {
		prompt      string
		key         string
		match       bool
		consecutive int
	}{
		"match": {
			prompt:      "set",
			key:         "secret",
			match:       true,
			consecutive: 1,
		},
		"match2": {
			prompt:      "seet",
			key:         "secret",
			match:       true,
			consecutive: 2,
		},
		"exact-match": {
			prompt:      "secret",
			key:         "secret",
			match:       true,
			consecutive: 5,
		},
		"short-match": {
			prompt:      "user",
			key:         "/user",
			match:       true,
			consecutive: 3,
		},
		"long-match": {
			prompt:      "user",
			key:         "/if/you/can/read/the/full/path/of/this/key/you/are/the/person/in/the/red/flag/monitor/meme",
			match:       true,
			consecutive: 0,
		},
		"case-insensitive-match": {
			prompt:      "user",
			key:         "UsEr",
			match:       true,
			consecutive: 3,
		},
		"no-match": {
			prompt:      "asdf",
			key:         "secret",
			match:       false,
			consecutive: 0,
		},
		"no-match1": {
			prompt:      "a",
			key:         "secret",
			match:       false,
			consecutive: 0,
		},
		"no-match2": {
			prompt:      "seeet",
			key:         "secret",
			match:       false,
			consecutive: 0,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			match, consecutive := matchesPrompt(test.prompt, test.key)
			if match != test.match {
				t.Fatalf("Expected %s to match %s", test.prompt, test.key)
			}
			if consecutive != test.consecutive {
				t.Fatalf("Expected %d consecutive character matches for prompt %s and key %s, got %d", test.consecutive, test.prompt, test.key, consecutive)
			}
		})
	}
}
