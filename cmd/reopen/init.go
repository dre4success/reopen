package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/settings"
)

// A CLI's setup, as reopen init finds it.
const (
	notInstalled = iota // no such command on PATH
	setUp               // its settings hold reopen's start hook
	toAdd               // they don't, and reopen can add it
	cantAdd             // they can't be read, or aren't as a CLI writes them
)

type setup struct {
	cli   *agent.CLI
	spec  hookSpec
	state int
	why   string // cantAdd: why
	// existed: the file was there; data is what it held, next what it
	// will hold.
	existed    bool
	data, next []byte
	// untrusted: Codex has the hook but no record of the operator trusting
	// it, so it doesn't run it yet. It asks at its next start.
	untrusted bool
}

// setups finds each CLI's setup on this Mac. It only reads.
func (a *app) setups() []setup {
	var out []setup
	for _, name := range agent.Names() {
		c, _ := agent.Lookup(name)
		spec, _ := a.hookSpec(name)
		s := setup{cli: c, spec: spec}
		if _, err := a.lookPath(c.Command); err != nil {
			out = append(out, s) // not installed
			continue
		}
		if !filepath.IsAbs(spec.file) {
			// A relative folder would be the one reopen runs in, not the CLI's.
			s.state, s.why = cantAdd, "its settings folder isn't an absolute path; check HOME, CLAUDE_CONFIG_DIR and CODEX_HOME"
			out = append(out, s)
			continue
		}
		data, err := os.ReadFile(spec.file)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			s.state, s.why = cantAdd, err.Error()
			out = append(out, s)
			continue
		default:
			s.existed, s.data = true, data
		}
		if spec.has(data) {
			s.state = setUp
		} else if next, err := settings.Append(data, spec.path, spec.elem); err != nil {
			s.state, s.why = cantAdd, err.Error()
		} else {
			s.state, s.next = toAdd, next
		}
		if name == agent.Codex && s.state != cantAdd {
			s.untrusted = s.state == toAdd || !a.codexTrusts(spec, data)
		}
		out = append(out, s)
	}
	return out
}

// codexTrusts says whether Codex has a record of the operator trusting
// reopen's start hook. Codex notes each hook it was told to trust in its
// config.toml, as [hooks.state."<hooks.json>:session_start:<group>:<hook>"]
// with a trusted_hash, and runs no hook without one (docs/FACTS.md). With
// no config.toml to read, it has no record.
func (a *app) codexTrusts(spec hookSpec, data []byte) bool {
	conf, err := os.ReadFile(filepath.Join(a.homes().Codex, "config.toml"))
	if err != nil {
		return false
	}
	var groups struct {
		Hooks struct {
			SessionStart []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"SessionStart"`
		} `json:"hooks"`
	}
	if json.Unmarshal(data, &groups) != nil {
		return false
	}
	for i, g := range groups.Hooks.SessionStart {
		for j, h := range g.Hooks {
			if words, ok := shellWords(h.Command); !ok || !spec.reopens(words) {
				continue
			}
			section := fmt.Sprintf("[hooks.state.%q]", fmt.Sprintf("%s:session_start:%d:%d", spec.file, i, j))
			_, rest, ok := bytes.Cut(conf, []byte(section))
			if !ok {
				return false
			}
			if k := bytes.Index(rest, []byte("\n[")); k >= 0 {
				rest = rest[:k]
			}
			return bytes.Contains(rest, []byte("trusted_hash"))
		}
	}
	return false
}

// cmdInit adds reopen's start hook to the settings of each agent CLI on
// this Mac that lacks it, once the operator says yes at a terminal: one
// step, once, for every project. Each file it changes is copied beside
// itself first, and the hook goes in at one spot, every other byte as it
// was. It is refused in an agent's session: a CLI's settings are the
// operator's to change.
func (a *app) cmdInit(args []string) int {
	if len(args) > 0 {
		fmt.Fprintf(a.stderr, "reopen: init takes no arguments\n\n%s", usage)
		return 2
	}
	if v := a.agentSession(); v != "" {
		fmt.Fprintf(a.stderr, "reopen: init is refused in an agent's session (%s is set): it changes the CLIs' settings, which only the operator does, at a terminal of their own. reopen hooks <cli> prints a hook to add by hand.\n", v)
		return 1
	}
	if !filepath.IsAbs(a.home) {
		fmt.Fprintf(a.stderr, "reopen: no home folder (HOME is %q), so init can't tell where the CLIs' settings are. Nothing changed.\n", a.home)
		return 1
	}
	s := style{on: a.color}
	setups := a.setups()
	fmt.Fprintln(a.stdout, s.bold("reopen init")+s.dim(" · reopen's start hook in each CLI's settings, so its sessions get on their project's list as they start"))
	fmt.Fprintln(a.stdout)
	var adds []int
	for i, st := range setups {
		var mark, what string
		where := a.tilde(st.spec.file)
		switch st.state {
		case notInstalled:
			mark, what = s.dim("–"), s.dim("not installed (no "+st.cli.Command+" on PATH)")
		case setUp:
			mark, what = s.green("✓"), "set up already · "+where
		case toAdd:
			mark, what = s.cyan("+"), "to add · "+where
			adds = append(adds, i)
		case cantAdd:
			mark, what = s.red("✗"), s.red("can't add it: "+where+": "+agent.Clean(st.why))+s.dim(" · reopen hooks "+st.cli.Name+" prints it to add by hand")
		}
		fmt.Fprintf(a.stdout, "  %s %s  %s\n", mark, pad(st.spec.name, 11), what)
	}
	fmt.Fprintln(a.stdout)
	code := 0
	if len(adds) > 0 {
		if !a.terminal() {
			fmt.Fprintln(a.stderr, "reopen: stdin isn't a terminal, and init asks before it changes a file. Nothing changed.")
			return 1
		}
		fmt.Fprintf(a.stdout, "Add it to %d %s? Each is copied beside itself first. [Y/n] ", len(adds), plural(len(adds), "file", "files"))
		answer, err := bufio.NewReader(a.stdin).ReadString('\n')
		if err != nil {
			// No whole line: Ctrl-D, or stdin closed. Only Enter is yes.
			fmt.Fprintln(a.stdout)
			fmt.Fprintln(a.stdout, "reopen: nothing changed.")
			return 0
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
		default:
			fmt.Fprintln(a.stdout, "reopen: nothing changed.")
			return 0
		}
		for _, i := range adds {
			st := &setups[i]
			backup, err := a.writeSettings(st)
			switch {
			case err != nil:
				fmt.Fprintf(a.stdout, "  %s %s  %s\n", s.red("✗"), pad(st.spec.name, 11), s.red("not added: "+err.Error()))
				st.untrusted = false
				code = 1
			case backup != "":
				fmt.Fprintf(a.stdout, "  %s %s  added · the file as it was is %s\n", s.green("✓"), pad(st.spec.name, 11), a.tilde(backup))
			default:
				fmt.Fprintf(a.stdout, "  %s %s  added · %s is new\n", s.green("✓"), pad(st.spec.name, 11), a.tilde(st.spec.file))
			}
		}
		fmt.Fprintln(a.stdout)
	}
	for _, st := range setups {
		if st.untrusted {
			fmt.Fprintln(a.stdout, s.yellow("Codex runs reopen's hook only once you trust it, and its config.toml has no record of that yet: the next time Codex starts, it asks. Trust it."))
		}
	}
	if code == 0 {
		fmt.Fprintln(a.stdout, "From now on each new session gets on its project's list as it starts (Codex and Antigravity: at its first message). In a project's folder, reopen brings them back; a session handoff gives a role is found even without its hook.")
	}
	return code
}

// writeSettings writes a CLI's settings with reopen's hook added: the file
// as it was is copied beside it first, and the new one is put in its place
// whole, so a CLI never reads half of one. A link is followed, and the file
// it names is written. It fails, changing nothing, if the file changed since
// it was read. It returns the copy's path, or "" for a new file.
func (a *app) writeSettings(st *setup) (string, error) {
	target := st.spec.file
	if fi, err := os.Lstat(target); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if target, err = filepath.EvalSymlinks(target); err != nil {
			return "", err
		}
	}
	if testing.Testing() {
		if real, err := os.UserHomeDir(); err == nil && strings.HasPrefix(target, filepath.Clean(real)+string(filepath.Separator)) {
			return "", errors.New("a test may not write the real home's settings")
		}
	}
	mode := fs.FileMode(0o600)
	now, err := os.ReadFile(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if st.existed {
			return "", errors.New("it was removed while reopen read it; run reopen init again")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	case !st.existed || !bytes.Equal(now, st.data):
		return "", errors.New("it changed while reopen read it; run reopen init again")
	default:
		fi, err := os.Stat(target)
		if err != nil {
			return "", err
		}
		mode = fi.Mode().Perm()
	}
	backup := ""
	if st.existed {
		backup = st.spec.file + ".reopen-backup-" + a.now().Format("20060102-150405")
		f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return "", err
		}
		_, err = f.Write(st.data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(backup)
			return "", err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".reopen-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once renamed
	if _, err := f.Write(st.next); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return backup, os.Rename(tmp, target)
}

// setupNotes say, a line each, which agent CLIs on this Mac lack reopen's
// start hook, or haven't trusted it: their new sessions aren't recorded.
func (a *app) setupNotes() []string {
	var out []string
	var missing []string
	for _, st := range a.setups() {
		switch {
		case st.state == toAdd:
			missing = append(missing, st.spec.name)
		case st.state == cantAdd:
			out = append(out, fmt.Sprintf("reopen: %s's settings can't take reopen's hook (%s: %s); reopen hooks %s prints it to add by hand.", st.spec.name, a.tilde(st.spec.file), agent.Clean(st.why), st.cli.Name))
		case st.untrusted:
			out = append(out, "reopen: Codex has no record of trusting reopen's hook, so it doesn't run it yet: the next time Codex starts, it asks. Trust it.")
		}
	}
	if len(missing) > 0 {
		out = append([]string{fmt.Sprintf("reopen: %s %s no start hook from reopen, so new sessions there aren't recorded. reopen init adds it.", strings.Join(missing, " and "), plural(len(missing), "has", "have"))}, out...)
	}
	return out
}
