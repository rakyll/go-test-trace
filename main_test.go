// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gotest is a tiny program that shells out to `go test`
// and prints the output in color.
package main

import (
	"testing"
	"time"
)

func TestParseNameAndDuration(t *testing.T) {
	tests := []struct {
		line     string
		wantName string
		wantDur  string
	}{
		{
			line:     "    --- PASS: TestGetConfig/otlp#01 (1.50s)",
			wantName: "TestGetConfig/otlp#01",
			wantDur:  "1.5s",
		},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			name, dur := parseNameAndDuration(tt.line)
			if name != tt.wantName {
				t.Errorf("parseNameAndDuration() name = %v, want %v", name, tt.wantName)
			}
			parsedWantDur, _ := time.ParseDuration(tt.wantDur)
			if dur != parsedWantDur {
				t.Errorf("parseNameAndDuration() duration = %v, want %v", dur, tt.wantDur)
			}
		})
	}
}
