// Package settings adds a hook to a CLI's JSON settings file in place:
// the new text goes in at one spot, and every other byte of the file stays
// as it was, its order, layout and the hooks of other tools among them.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Append returns data with elem appended to the array at path, a key at
// each level from the top object. What path names that is missing is made:
// at the first missing key, a member holding the rest of the path, down to
// an array of elem alone. Empty data is a file not yet written. It fails
// for data that isn't JSON, or where a step of path isn't what it must be.
func Append(data []byte, path []string, elem any) ([]byte, error) {
	if len(path) == 0 {
		return nil, errors.New("no path")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		v, err := render(nest(path, elem), "", "  ")
		if err != nil {
			return nil, err
		}
		return []byte(v + "\n"), nil
	}
	if !json.Valid(data) {
		return nil, errors.New("it isn't valid JSON")
	}
	p := &parser{data: data}
	p.space()
	top := p.value()
	if top.kind != '{' {
		return nil, errors.New("it isn't a JSON object")
	}
	unit := indentUnit(data)
	n := top
	for i, key := range path {
		m := n.member(key)
		if m == nil {
			return addMember(data, n, key, nest(path[i+1:], elem), unit)
		}
		n = m
		if i < len(path)-1 && n.kind != '{' {
			return nil, fmt.Errorf("%s isn't an object", strings.Join(path[:i+1], "."))
		}
	}
	if n.kind != '[' {
		return nil, fmt.Errorf("%s isn't an array", strings.Join(path, "."))
	}
	return addElem(data, n, elem, unit)
}

// nest is elem in an array, inside an object for each key of path.
func nest(path []string, elem any) any {
	var v any = []any{elem}
	for i := len(path) - 1; i >= 0; i-- {
		v = object{{path[i], v}}
	}
	return v
}

// object is a JSON object whose keys keep their order.
type object []struct {
	key string
	val any
}

func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshal(m.key)
		if err != nil {
			return nil, err
		}
		v, err := marshal(m.val)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Object is a JSON object with its keys in the order given: key, value,
// key, value.
func Object(kv ...any) any {
	var o object
	for i := 0; i+1 < len(kv); i += 2 {
		o = append(o, struct {
			key string
			val any
		}{kv[i].(string), kv[i+1]})
	}
	return o
}

func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// render writes v indented: its first line bare, the others after prefix.
func render(v any, prefix, unit string) (string, error) {
	raw, err := marshal(v)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, prefix, unit); err != nil {
		return "", err
	}
	return b.String(), nil
}

func addElem(data []byte, arr *node, elem any, unit string) ([]byte, error) {
	if len(arr.elems) == 0 {
		return fillEmpty(data, arr, func(ind string) (string, error) { return render(elem, ind, unit) }, unit)
	}
	first, last := arr.elems[0], arr.elems[len(arr.elems)-1]
	ind, sep := lineIndent(data, first.start), ",\n"
	if !bytes.ContainsRune(data[arr.start:first.start], '\n') {
		ind, sep = lineIndent(data, arr.start), ", " // all on one line
	}
	v, err := render(elem, ind, unit)
	if err != nil {
		return nil, err
	}
	if sep == ",\n" {
		v = ind + v
	}
	return splice(data, last.end, last.end, sep+v), nil
}

func addMember(data []byte, obj *node, key string, val any, unit string) ([]byte, error) {
	member := func(ind string) (string, error) {
		k, err := marshal(key)
		if err != nil {
			return "", err
		}
		v, err := render(val, ind, unit)
		return string(k) + ": " + v, err
	}
	if len(obj.members) == 0 {
		return fillEmpty(data, obj, member, unit)
	}
	first, last := obj.members[0], obj.members[len(obj.members)-1]
	ind, sep := lineIndent(data, first.keyStart), ",\n"
	if !bytes.ContainsRune(data[obj.start:first.keyStart], '\n') {
		ind, sep = lineIndent(data, obj.start), ", "
	}
	m, err := member(ind)
	if err != nil {
		return nil, err
	}
	if sep == ",\n" {
		m = ind + m
	}
	return splice(data, last.val.end, last.val.end, sep+m), nil
}

// fillEmpty writes the one child of an empty object or array, on a line of
// its own, one step in from the line the bracket is on.
func fillEmpty(data []byte, n *node, child func(ind string) (string, error), unit string) ([]byte, error) {
	outer := lineIndent(data, n.start)
	c, err := child(outer + unit)
	if err != nil {
		return nil, err
	}
	return splice(data, n.start+1, n.end-1, "\n"+outer+unit+c+"\n"+outer), nil
}

func splice(data []byte, from, to int, text string) []byte {
	out := make([]byte, 0, len(data)+len(text))
	out = append(out, data[:from]...)
	out = append(out, text...)
	return append(out, data[to:]...)
}

// lineIndent is the spaces and tabs that start the line off is on.
func lineIndent(data []byte, off int) string {
	start := bytes.LastIndexByte(data[:off], '\n') + 1
	end := start
	for end < len(data) && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return string(data[start:end])
}

// indentUnit is one step of the file's indentation: the first indented
// line's, or two spaces.
func indentUnit(data []byte) string {
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		ws := len(line) - len(bytes.TrimLeft(line, " \t"))
		if ws > 0 && ws < len(line) {
			return string(line[:ws])
		}
	}
	return "  "
}

// node is a JSON value and where it is in the file: data[start:end].
type node struct {
	start, end int
	kind       byte // '{', '[', or 0 for any other value
	members    []member
	elems      []*node
}

type member struct {
	key      string
	keyStart int
	val      *node
}

// member is the value of key in an object, the last if it is there twice,
// as a JSON reader takes it; or nil.
func (n *node) member(key string) *node {
	var v *node
	for _, m := range n.members {
		if m.key == key {
			v = m.val
		}
	}
	return v
}

// parser reads JSON already checked valid, noting where each value is.
type parser struct {
	data []byte
	i    int
}

func (p *parser) space() {
	for p.i < len(p.data) && strings.IndexByte(" \t\r\n", p.data[p.i]) >= 0 {
		p.i++
	}
}

func (p *parser) value() *node {
	n := &node{start: p.i}
	switch p.data[p.i] {
	case '{':
		n.kind = '{'
		p.i++
		for p.space(); p.data[p.i] != '}'; p.space() {
			ks := p.i
			p.str()
			var key string
			json.Unmarshal(p.data[ks:p.i], &key)
			p.space()
			p.i++ // :
			p.space()
			n.members = append(n.members, member{key: key, keyStart: ks, val: p.value()})
			p.space()
			if p.data[p.i] == ',' {
				p.i++
			}
		}
		p.i++
	case '[':
		n.kind = '['
		p.i++
		for p.space(); p.data[p.i] != ']'; p.space() {
			n.elems = append(n.elems, p.value())
			p.space()
			if p.data[p.i] == ',' {
				p.i++
			}
		}
		p.i++
	case '"':
		p.str()
	default:
		for p.i < len(p.data) && strings.IndexByte(",]} \t\r\n", p.data[p.i]) < 0 {
			p.i++
		}
	}
	n.end = p.i
	return n
}

func (p *parser) str() {
	p.i++ // "
	for p.data[p.i] != '"' {
		if p.data[p.i] == '\\' {
			p.i++
		}
		p.i++
	}
	p.i++
}
