package main

// AeStore: Aether's own embedded storage engine (no SQLite).
// Design: an append-only write-ahead log of JSON records (one mutation per line, fsync'd before the
// in-memory state is updated), replayed on open, and compacted into a fresh log by atomic rename
// (close handle → write tmp → fsync → rename → reopen) once it grows. Crash safety: a torn last
// line is ignored on replay. On Windows the open file is closed before rename so the replace succeeds,
// and the file is opened with share-friendly flags via openStoreFile.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type table struct {
	rows   map[int64]*Record
	nextID int64
	cols   []string
	types  map[string]string
}
type Store struct {
	mu     sync.RWMutex
	path   string
	f      *os.File
	tables map[string]*table
	lines  int
}

const compactThreshold = 500

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, tables: map[string]*table{}}
	if path == "" || path == ":memory:" {
		s.path = ""
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	validEnd := 0
	torn := false
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if len(bytes.TrimSpace(line)) == 0 {
			validEnd += len(line)
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		var m map[string]any
		err := dec.Decode(&m)
		if err == nil {
			var extra any
			if dec.Decode(&extra) != io.EOF {
				err = fmt.Errorf("trailing WAL content")
			}
		}
		if err != nil {
			if line[len(line)-1] != '\n' && validEnd+len(line) == len(data) {
				torn = true
				break
			}
			return nil, fmt.Errorf("corrupt WAL at byte %d: %w", validEnd, err)
		}
		if err := s.replay(m); err != nil {
			return nil, fmt.Errorf("corrupt WAL at byte %d: %w", validEnd, err)
		}
		s.lines++
		validEnd += len(line)
	}
	if torn {
		if err := os.Truncate(path, int64(validEnd)); err != nil {
			return nil, err
		}
	}
	if s.lines > compactThreshold {
		if err := s.compactLocked(); err != nil {
			// fall through and try to open the existing file
			_ = err
		}
	}
	f, err := openStoreFile(path)
	if err != nil {
		return nil, err
	}
	s.f = f
	// A complete final JSON object without a newline is valid, but needs a separator before appending.
	if !torn && len(data) > 0 && data[len(data)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		err := s.f.Close()
		s.f = nil
		return err
	}
	return nil
}

// MaybeCompact rewrites the WAL when it has grown; force=true always rewrites if on disk.
func (s *Store) MaybeCompact(force bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return
	}
	if !force && s.lines <= compactThreshold {
		return
	}
	_ = s.compactLocked()
}

func (s *Store) tbl(name string) *table {
	t := s.tables[name]
	if t == nil {
		t = &table{rows: map[int64]*Record{}, nextID: 1, types: map[string]string{}}
		s.tables[name] = t
	}
	return t
}
func (s *Store) replay(m map[string]any) error {
	name, ok := m["t"].(string)
	n, idOK := m["id"].(json.Number)
	id, err := n.Int64()
	op, _ := m["op"].(string)
	if !ok || name == "" || !idOK || err != nil || id < 1 || (op != "put" && op != "del" && op != "seq") {
		return fmt.Errorf("invalid WAL operation/table/id")
	}
	if op == "put" {
		if _, ok := m["r"].(map[string]any); !ok {
			return fmt.Errorf("invalid WAL record")
		}
	}
	t := s.tbl(name)
	switch m["op"] {
	case "put":
		r := fromJSON(m["r"]).(*Record)
		t.rows[id] = r
		if id >= t.nextID {
			t.nextID = id + 1
		}
	case "del":
		delete(t.rows, id)
	case "seq":
		t.nextID = id
	}
	return nil
}
func (s *Store) wal(op, name string, id int64, r *Record) {
	s.lines++
	if s.f == nil {
		return
	}
	line := `{"op": ` + jsonStr(op) + `, "t": ` + jsonStr(name) + `, "id": ` + strconv.FormatInt(id, 10)
	if r != nil {
		line += `, "r": ` + toJSON(r)
	}
	line += "}\n"
	s.f.WriteString(line)
	s.f.Sync()
	if s.lines > compactThreshold*2 {
		_ = s.compactLocked()
	}
}

// compactLocked assumes s.mu is held. Closes the live handle, rewrites via tmp+rename, reopens.
func (s *Store) compactLocked() error {
	if s.path == "" {
		return nil
	}
	if s.f != nil {
		s.f.Close()
		s.f = nil
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		// try reopen original
		s.f, _ = openStoreFile(s.path)
		return err
	}
	w := bufio.NewWriter(f)
	n := 0
	for name, t := range s.tables {
		w.WriteString(`{"op": "seq", "t": ` + jsonStr(name) + `, "id": ` + strconv.FormatInt(t.nextID, 10) + "}\n")
		n++
		for _, id := range sortedIDs(t) {
			w.WriteString(`{"op": "put", "t": ` + jsonStr(name) + `, "id": ` + strconv.FormatInt(id, 10) + `, "r": ` + toJSON(t.rows[id]) + "}\n")
			n++
		}
	}
	w.Flush()
	f.Sync()
	f.Close()
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		s.f, _ = openStoreFile(s.path)
		return err
	}
	s.lines = n
	nf, err := openStoreFile(s.path)
	if err != nil {
		return err
	}
	s.f = nf
	return nil
}

func sortedIDs(t *table) []int64 {
	ids := make([]int64, 0, len(t.rows))
	for id := range t.rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
func (s *Store) Ensure(ent Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tbl(ent["name"].(string))
	for _, f := range ent["fields"].([]any) {
		fn := f.(Node)
		name := fn["name"].(string)
		if _, ok := t.types[name]; !ok {
			t.cols = append(t.cols, name) // automatic schema migration: new columns appear as nil
		}
		t.types[name] = fn["type"].(Node)["name"].(string)
	}
}
func (s *Store) out(t *table, id int64, r *Record) *Record {
	o := NewRec()
	o.Set("id", id)
	for _, c := range t.cols {
		v, ok := r.M[c]
		if !ok {
			v = nil
		}
		if t.types[c] == "Bool" && v != nil {
			v = truthy(v)
		}
		o.Set(c, v)
	}
	return o
}
func (s *Store) in(t *table, rec *Record, base *Record) *Record {
	r := base
	if r == nil {
		r = NewRec()
	}
	for _, c := range t.cols {
		v, ok := rec.M[c]
		if !ok {
			continue
		}
		switch t.types[c] {
		case "Int":
			if sv, ok := v.(string); ok {
				n, _ := strconv.ParseFloat(strings.TrimSpace(sv), 64)
				v = int64(n)
			} else if f, ok := v.(float64); ok && f == float64(int64(f)) {
				v = int64(f)
			}
		case "Float":
			if sv, ok := v.(string); ok {
				v, _ = strconv.ParseFloat(strings.TrimSpace(sv), 64)
			}
		case "Bool":
			switch x := v.(type) {
			case bool:
				v = x
			case int64:
				v = x == 1
			case string:
				v = x == "true" || x == "on" || x == "1"
			default:
				v = false
			}
		}
		r.Set(c, v)
	}
	return r
}
func (s *Store) All(name string) []*Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.tbl(name)
	res := []*Record{}
	for _, id := range sortedIDs(t) {
		res = append(res, s.out(t, id, t.rows[id]))
	}
	return res
}
func (s *Store) Get(name string, id int64) *Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.tbl(name)
	r, ok := t.rows[id]
	if !ok {
		return nil
	}
	return s.out(t, id, r)
}
func (s *Store) Insert(name string, rec *Record) *Record {
	s.mu.Lock()
	t := s.tbl(name)
	id := t.nextID
	t.nextID++
	r := s.in(t, rec, nil)
	s.wal("put", name, id, r)
	t.rows[id] = r
	s.mu.Unlock()
	return s.Get(name, id)
}
func (s *Store) Update(name string, id int64, rec *Record) *Record {
	s.mu.Lock()
	t := s.tbl(name)
	if old, ok := t.rows[id]; ok {
		r := s.in(t, rec, old.Copy())
		s.wal("put", name, id, r)
		t.rows[id] = r
	}
	s.mu.Unlock()
	return s.Get(name, id)
}
func (s *Store) Delete(name string, id int64) {
	s.mu.Lock()
	t := s.tbl(name)
	if _, ok := t.rows[id]; ok {
		s.wal("del", name, id, nil)
		delete(t.rows, id)
	}
	s.mu.Unlock()
}
