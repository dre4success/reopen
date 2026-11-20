package settings

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var hook = Object("hooks", []any{Object("type", "command", "command", "reopen", "args", []string{"hook", "claude"})})

func TestAppend(t *testing.T) {
	for _, c := range []struct {
		name, in, want string
	}{
		{"a file not yet written", "", `{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "reopen",
            "args": [
              "hook",
              "claude"
            ]
          }
        ]
      }
    ]
  }
}
`},
		{"an empty object", "{}\n", `{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "reopen",
            "args": [
              "hook",
              "claude"
            ]
          }
        ]
      }
    ]
  }
}
`},
		{"other settings, kept in their order, no hooks yet", `{
  "model": "opus",
  "env": {"B": "1", "A": "2"},
  "theme": "dark"
}
`, `{
  "model": "opus",
  "env": {"B": "1", "A": "2"},
  "theme": "dark",
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "reopen",
            "args": [
              "hook",
              "claude"
            ]
          }
        ]
      }
    ]
  }
}
`},
		{"another tool's start hook, kept first", `{
    "hooks": {
        "SessionStart": [
            {
                "hooks": [{"type": "command", "command": "handoff hook brief"}]
            }
        ],
        "Stop": []
    }
}
`, `{
    "hooks": {
        "SessionStart": [
            {
                "hooks": [{"type": "command", "command": "handoff hook brief"}]
            },
            {
                "hooks": [
                    {
                        "type": "command",
                        "command": "reopen",
                        "args": [
                            "hook",
                            "claude"
                        ]
                    }
                ]
            }
        ],
        "Stop": []
    }
}
`},
		{"hooks but no start hooks, tabs", "{\n\t\"hooks\": {\n\t\t\"Stop\": []\n\t}\n}", "{\n\t\"hooks\": {\n\t\t\"Stop\": [],\n\t\t\"SessionStart\": [\n\t\t\t{\n\t\t\t\t\"hooks\": [\n\t\t\t\t\t{\n\t\t\t\t\t\t\"type\": \"command\",\n\t\t\t\t\t\t\"command\": \"reopen\",\n\t\t\t\t\t\t\"args\": [\n\t\t\t\t\t\t\t\"hook\",\n\t\t\t\t\t\t\t\"claude\"\n\t\t\t\t\t\t]\n\t\t\t\t\t}\n\t\t\t\t]\n\t\t\t}\n\t\t]\n\t}\n}"},
		{"an empty array", `{"hooks": {"SessionStart": []}}`, `{"hooks": {"SessionStart": [
  {
    "hooks": [
      {
        "type": "command",
        "command": "reopen",
        "args": [
          "hook",
          "claude"
        ]
      }
    ]
  }
]}}`},
		{"all on one line", `{"hooks":{"SessionStart":[{"hooks":[]}]},"x":"a\"}b"}`, `{"hooks":{"SessionStart":[{"hooks":[]}, {
  "hooks": [
    {
      "type": "command",
      "command": "reopen",
      "args": [
        "hook",
        "claude"
      ]
    }
  ]
}]},"x":"a\"}b"}`},
	} {
		got, err := Append([]byte(c.in), []string{"hooks", "SessionStart"}, hook)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if string(got) != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", c.name, got, c.want)
		}
		if !json.Valid(got) {
			t.Errorf("%s: not valid JSON", c.name)
		}
	}
}

// Whatever the file held, it holds still, with the hook added.
func TestAppendKeepsEverythingElse(t *testing.T) {
	in := `{"a": [1, 2.50, "x\u00e9"], "hooks": {"SessionStart": [{"hooks": [{"command": "h"}]}], "Stop": [{"hooks": []}]}, "z": {"<&>": null}}`
	out, err := Append([]byte(in), []string{"hooks", "SessionStart"}, hook)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	json.Unmarshal([]byte(in), &before)
	json.Unmarshal(out, &after)
	ss := after["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(ss) != 2 {
		t.Fatalf("%d start hooks", len(ss))
	}
	after["hooks"].(map[string]any)["SessionStart"] = ss[:1]
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("changed:\n%s", out)
	}
	if !strings.HasPrefix(string(out), `{"a": [1, 2.50, "x\u00e9"], "hooks": {"SessionStart": [{"hooks": [{"command": "h"}]}`) ||
		!strings.HasSuffix(string(out), `], "Stop": [{"hooks": []}]}, "z": {"<&>": null}}`) {
		t.Fatalf("bytes around the hook changed:\n%s", out)
	}
}

func TestAppendRefuses(t *testing.T) {
	for _, in := range []string{
		`{"hooks": `,                     // not JSON
		`// a comment` + "\n" + `{}`,     // not JSON either
		`[]`,                             // not an object
		`{"hooks": []}`,                  // hooks isn't an object
		`{"hooks": {"SessionStart": 1}}`, // nor is this an array
	} {
		if out, err := Append([]byte(in), []string{"hooks", "SessionStart"}, hook); err == nil {
			t.Errorf("%q: no error, gave %s", in, out)
		}
	}
}

// Antigravity's file is named groups of hooks: reopen's group is added
// beside the others, or its start hook beside those already in it.
func TestAppendAntigravity(t *testing.T) {
	h := Object("type", "command", "command", "exec reopen hook antigravity", "timeout", 10)
	in := "{\n  \"handoff\": {\n    \"SessionStart\": []\n  }\n}\n"
	out, err := Append([]byte(in), []string{"reopen", "SessionStart"}, h)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"handoff\": {\n    \"SessionStart\": []\n  },\n  \"reopen\": {\n    \"SessionStart\": [\n      {\n        \"type\": \"command\",\n        \"command\": \"exec reopen hook antigravity\",\n        \"timeout\": 10\n      }\n    ]\n  }\n}\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}
