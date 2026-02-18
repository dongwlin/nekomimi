package commands

import "testing"

func TestParseTargetSessionKey(t *testing.T) {
	tests := []struct {
		name       string
		args       string
		wantKey    string
		wantLabel  string
		shouldFail bool
	}{
		{
			name:      "group target",
			args:      "group 123456",
			wantKey:   "group:123456",
			wantLabel: "群聊(123456)",
		},
		{
			name:      "private target",
			args:      "private 10001",
			wantKey:   "private:10001",
			wantLabel: "私聊(10001)",
		},
		{
			name:      "alias target type",
			args:      "u 42",
			wantKey:   "private:42",
			wantLabel: "私聊(42)",
		},
		{
			name:       "missing args",
			args:       "group",
			shouldFail: true,
		},
		{
			name:       "invalid type",
			args:       "guild 123",
			shouldFail: true,
		},
		{
			name:       "non-digit id",
			args:       "group abc",
			shouldFail: true,
		},
	}
	for _, tt := range tests {
		gotKey, gotLabel, err := parseTargetSessionKey(tt.args)
		if tt.shouldFail {
			if err == nil {
				t.Fatalf("%s: expected error, got nil", tt.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if gotKey != tt.wantKey {
			t.Fatalf("%s: key got=%s want=%s", tt.name, gotKey, tt.wantKey)
		}
		if gotLabel != tt.wantLabel {
			t.Fatalf("%s: label got=%s want=%s", tt.name, gotLabel, tt.wantLabel)
		}
	}
}

func TestIsDigits(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "123", want: true},
		{value: "001", want: true},
		{value: "", want: false},
		{value: "12a3", want: false},
		{value: " 123 ", want: false},
		{value: "-1", want: false},
	}
	for _, tt := range tests {
		got := isDigits(tt.value)
		if got != tt.want {
			t.Fatalf("value=%q got=%v want=%v", tt.value, got, tt.want)
		}
	}
}

func TestParseActionArgs_ExtraArgsDetected(t *testing.T) {
	action, rest := parseActionArgs("on group 123456")
	if action != "on" {
		t.Fatalf("action got=%q want=%q", action, "on")
	}
	if rest != "group 123456" {
		t.Fatalf("rest got=%q want=%q", rest, "group 123456")
	}
}
