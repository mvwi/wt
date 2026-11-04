package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestResolveEditor(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		onPath []string
		want   string
	}{
		{"WT_EDITOR wins", map[string]string{"WT_EDITOR": "code -n", "VISUAL": "nvim"}, []string{"cursor"}, "code -n"},
		{"then VISUAL", map[string]string{"VISUAL": "nvim"}, []string{"cursor"}, "nvim"},
		{"blank env ignored", map[string]string{"WT_EDITOR": "  "}, []string{"zed"}, "zed"},
		{"cursor before code", nil, []string{"code", "cursor"}, "cursor"},
		{"code before zed", nil, []string{"zed", "code"}, "code"},
		{"none", nil, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			lookPath := func(f string) (string, error) {
				for _, p := range tt.onPath {
					if p == f {
						return "/bin/" + f, nil
					}
				}
				return "", errors.New("not found")
			}
			got, err := resolveEditor(getenv, lookPath)
			if got != tt.want || (err != nil) != (tt.want == "") {
				t.Errorf("resolveEditor() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestIsTerminalEditor(t *testing.T) {
	for editor, want := range map[string]bool{
		"nvim":               true,
		"/usr/local/bin/hx":  true,
		"vim -p":             true,
		"code":               false,
		"code --wait":        false,
		"/usr/local/bin/zed": false,
		"":                   false,
	} {
		if got := isTerminalEditor(editor); got != want {
			t.Errorf("isTerminalEditor(%q) = %v, want %v", editor, got, want)
		}
	}
}

func TestFormatPrompt(t *testing.T) {
	tests := []struct {
		ahead, behind int
		pr            cachedPR
		want          string
	}{
		{0, 0, cachedPR{}, "login-form"},
		{2, 5, cachedPR{}, "login-form ⇡2 ⇣5"},
		{0, 3, cachedPR{Number: 12}, "login-form ⇣3 #12"},
		{2, 5, cachedPR{Number: 123, CI: "pass"}, "login-form ⇡2 ⇣5 #123 ✓"},
		{1, 0, cachedPR{Number: 7, CI: "fail"}, "login-form ⇡1 #7 ✗"},
		{0, 0, cachedPR{Number: 7, CI: "pending"}, "login-form #7 ◐"},
	}
	for _, tt := range tests {
		if got := formatPrompt("login-form", tt.ahead, tt.behind, tt.pr); got != tt.want {
			t.Errorf("formatPrompt(%d, %d, %+v) = %q, want %q", tt.ahead, tt.behind, tt.pr, got, tt.want)
		}
	}
}

func TestCreateFlags(t *testing.T) {
	for _, c := range []string{"new", "pr", "pull"} {
		cmd, _, err := rootCmd.Find([]string{c})
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.ParseFlags([]string{"-i", "-e", "--run", "claude 'fix it'", "--output", "json"}); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		if !createInit || !createEdit || createRun != "claude 'fix it'" || outputFormat != "json" {
			t.Errorf("%s: flags not set: init=%v edit=%v run=%q output=%q", c, createInit, createEdit, createRun, outputFormat)
		}
		createInit, createEdit, createRun, outputFormat = false, false, "", ""
	}
	for _, c := range []string{"switch", "close", "submit"} {
		cmd, _, _ := rootCmd.Find([]string{c})
		if cmd.Flags().Lookup("output") == nil {
			t.Errorf("%s has no --output flag", c)
		}
	}
}

func TestStartOutputRejectsUnknownFormat(t *testing.T) {
	outputFormat = "yaml"
	defer func() { outputFormat = "" }()
	if err := startOutput(); err == nil {
		t.Error("startOutput() accepted --output yaml")
	}
}

func TestJSONShapes(t *testing.T) {
	tests := []struct {
		v    any
		want string
	}{
		{worktreeJSON{Name: "a", Path: "/p", Branch: "me/a", Base: "main", CTA: []string{"wt switch a"}},
			`{"name":"a","path":"/p","branch":"me/a","base":"main","cta":["wt switch a"]}`},
		{worktreeJSON{Name: "a", Path: "/p", Branch: "b", Base: "main", PR: 12},
			`{"name":"a","path":"/p","branch":"b","base":"main","pr":12}`},
		{closeJSON{Name: "a", Path: "/p", Branch: "me/a", BranchDeleted: true},
			`{"name":"a","path":"/p","branch":"me/a","branch_deleted":true}`},
		{submitJSON{Branch: "me/a", Pushed: true, PR: 3, CTA: []string{"wt watch"}},
			`{"branch":"me/a","pushed":true,"pr":3,"cta":["wt watch"]}`},
		{submitJSON{Branch: "me/a"}, `{"branch":"me/a","pushed":false}`},
	}
	for _, tt := range tests {
		got, _ := json.Marshal(tt.v)
		if string(got) != tt.want {
			t.Errorf("got %s\nwant %s", got, tt.want)
		}
	}
}

func TestSkillMarkdown(t *testing.T) {
	md := skillMarkdown(rootCmd)
	if !strings.HasPrefix(md, "---\nname: wt\ndescription: ") {
		t.Errorf("missing frontmatter:\n%s", md)
	}
	for _, want := range []string{"- `wt new <name>`: ", "- `wt submit`: ", "- `wt edit [name]`: ", "--output json", "--force"} {
		if !strings.Contains(md, want) {
			t.Errorf("SKILL.md lacks %q", want)
		}
	}
	// Shell setup commands have no group and stay out.
	if strings.Contains(md, "`wt init-shell") || strings.Contains(md, "`wt skill`") {
		t.Error("SKILL.md lists ungrouped commands")
	}
}
