package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// kind is how a flag takes its value.
type kind int

const (
	boolean  kind = iota // --flag
	value                // --flag v, --flag=v, and for a short one -fv
	variadic             // --flag v1 v2 …, up to the next word that starts with "-"
	optional             // --flag, or --flag v when the next word doesn't start with "-"
)

// spec is one flag of a CLI. Only the flags reopen passes back are kept;
// the others are listed so their values are skipped, and a positional word,
// such as a subcommand, is found where the CLI would find it.
type spec struct {
	names []string // the long name first: what is passed back
	kind  kind
	keep  bool
	// dir says the values are folders: only one that still exists comes
	// back, so neither a folder since removed nor words a variadic flag took
	// from a first prompt are passed on.
	dir bool
	// only, for a kept flag that takes a value, keeps only a value it
	// accepts.
	only func(v string) bool
	// skips says whether this flag, with value v, skips permission checks.
	skips func(v string) bool
}

// parser reads a CLI's arguments as the CLI does, as far as reopen needs.
type parser struct {
	specs []spec
	// stopAtWord: the flags end at the first positional word, as with Go's
	// flag package, which agy uses.
	stopAtWord bool
}

func (p parser) lookup(name string) (spec, bool) {
	for _, s := range p.specs {
		if slices.Contains(s.names, name) {
			return s, true
		}
	}
	return spec{}, false
}

// parsed is what a CLI's arguments say.
type parsed struct {
	kept  []Flag   // the flags to pass back, in the order given
	words []string // the positional words: a subcommand, an id, a prompt
	has   map[string]bool
}

// Flag is one start flag to pass back, as its words.
type Flag struct {
	Words []string
	Skips bool // it skips permission checks
}

// parse reads args, args[0] being the program. folder is the folder the
// process started in, against which a relative folder is checked.
func (p parser) parse(args []string, folder string) parsed {
	out := parsed{has: map[string]bool{}}
	if len(args) == 0 {
		return out
	}
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		w := rest[i]
		if w == "--" {
			out.words = append(out.words, rest[i+1:]...)
			break
		}
		if len(w) < 2 || w[0] != '-' {
			out.words = append(out.words, w)
			if p.stopAtWord {
				out.words = append(out.words, rest[i+1:]...)
				break
			}
			continue
		}
		name, val, hasVal := strings.Cut(w, "=")
		s, ok := p.lookup(name)
		if !ok && !p.stopAtWord && !strings.HasPrefix(w, "--") && len(w) > 2 {
			// (Go's flag package, agy's, takes no value attached to a flag.)
			// A short flag with its value attached, as -sread-only or
			// -cmodel=o3.
			if s2, ok2 := p.lookup(w[:2]); ok2 && s2.kind == value {
				s, ok, val, hasVal = s2, true, w[2:], true
			}
		}
		if !ok {
			continue // a flag reopen doesn't know: dropped, its value too if it has one
		}
		long := s.names[0]
		out.has[long] = true
		var vals []string
		switch s.kind {
		case boolean:
			if hasVal {
				vals = []string{val} // Go's flag package takes --sandbox=false
			}
		case value:
			if hasVal {
				vals = []string{val}
			} else if i+1 < len(rest) {
				i++
				vals = []string{rest[i]}
			}
		case optional:
			if hasVal {
				vals = []string{val}
			} else if i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "-") {
				i++
				vals = []string{rest[i]}
			}
		case variadic:
			if hasVal {
				vals = []string{val}
			} else {
				for i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "-") {
					i++
					vals = append(vals, rest[i])
				}
			}
		}
		if !s.keep {
			continue
		}
		if s.kind == boolean {
			f := Flag{Words: []string{long}}
			if hasVal {
				f.Words = []string{long + "=" + val}
			}
			f.Skips = s.skips != nil && s.skips("")
			if b, err := strconv.ParseBool(val); hasVal && (err != nil || !b) {
				f.Skips = false // --flag=false, =0, =F…, as Go's flag package reads them
			}
			out.kept = append(out.kept, f)
			continue
		}
		for _, v := range vals {
			if s.dir && !isDir(v, folder) {
				continue
			}
			if s.only != nil && !s.only(v) {
				continue
			}
			out.kept = append(out.kept, Flag{Words: []string{long, v}, Skips: s.skips != nil && s.skips(v)})
		}
	}
	return out
}

func isDir(p, folder string) bool {
	if p == "" {
		return false
	}
	if !filepath.IsAbs(p) {
		if folder == "" {
			return false
		}
		p = filepath.Join(folder, p)
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func always(string) bool { return true }

func is(vs ...string) func(string) bool {
	return func(v string) bool { return slices.Contains(vs, v) }
}
