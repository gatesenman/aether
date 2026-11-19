package main

// aether migrate（0.6 第 2 轮）：schema 与已有数据对账。
// 借鉴 Convex「schema 必须与已有数据一致」、PocketBase 自动迁移快照、Prisma migrate 的“先出计划再执行”。
// 计划是纯函数（只读）；--apply 才写入 AeStore（走 WAL，可被 compact）。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type MigStep struct {
	Code   string `json:"code"`
	Entity string `json:"entity"`
	Field  string `json:"field,omitempty"`
	Rows   int    `json:"rows"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
	Block  bool   `json:"blocking"`
}

// raw helpers on Store
func (s *Store) TableNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o := []string{}
	for n, t := range s.tables {
		if len(t.rows) > 0 {
			o = append(o, n)
		}
	}
	sort.Strings(o)
	return o
}
func (s *Store) rawEach(name string, f func(id int64, r *Record)) {
	s.mu.RLock()
	t := s.tables[name]
	s.mu.RUnlock()
	if t == nil {
		return
	}
	for _, id := range sortedIDs(t) {
		f(id, t.rows[id])
	}
}
func (s *Store) rawPut(name string, id int64, r *Record) {
	s.mu.Lock()
	t := s.tbl(name)
	s.wal("put", name, id, r)
	t.rows[id] = r
	s.mu.Unlock()
}
func (s *Store) rawDel(name string, id int64) {
	s.mu.Lock()
	t := s.tbl(name)
	s.wal("del", name, id, nil)
	delete(t.rows, id)
	s.mu.Unlock()
}

func typeOK(ty string, v Value) bool {
	if v == nil {
		return true
	}
	switch ty {
	case "Int":
		switch x := v.(type) {
		case int64:
			return true
		case float64:
			return x == float64(int64(x))
		}
		return false
	case "Float":
		switch v.(type) {
		case int64, float64:
			return true
		}
		return false
	case "Bool":
		_, ok := v.(bool)
		return ok
	case "Text", "String":
		_, ok := v.(string)
		return ok
	}
	return true
}

// PlanMigration compares the entities in it (already loaded) with the raw rows in its store.
func PlanMigration(it *Interp) []MigStep {
	steps := []MigStep{}
	declared := map[string]bool{}
	for _, en := range it.EntOrder {
		declared[en] = true
		decl := it.Entities[en]
		fields := map[string]Node{}
		order := []string{}
		for _, f := range decl["fields"].([]any) {
			fn := f.(Node)
			fields[fn["name"].(string)] = fn
			order = append(order, fn["name"].(string))
		}
		missing := map[string]int{}
		extra := map[string]int{}
		badType := map[string]int{}
		inv := 0
		invMsg := ""
		total := 0
		it.Store.rawEach(en, func(id int64, r *Record) {
			total++
			for _, n := range order {
				v, ok := r.M[n]
				if !ok {
					missing[n]++
				} else if !typeOK(fields[n]["type"].(Node)["name"].(string), v) {
					badType[n]++
				}
			}
			for k := range r.M {
				if _, ok := fields[k]; !ok && k != "id" {
					extra[k]++
				}
			}
			full := it.NewRecord(it.Entities[en], r)
			if err := catch(func() { it.CheckInv(en, full) }); err != nil {
				inv++
				invMsg = err.Error()
			}
		})
		if total == 0 {
			continue
		}
		for _, n := range order {
			if c := missing[n]; c > 0 {
				fn := fields[n]
				ty := fn["type"].(Node)["name"].(string)
				if fn["default"] != nil || builtinZero(ty) {
					d := "零值"
					if fn["default"] != nil {
						d = "默认值 " + Unparse(fn["default"].(Node))
					}
					steps = append(steps, MigStep{"M400", en, n, c, "backfill", "用" + d + " 回填", false})
				} else if fn["type"].(Node)["optional"] == true {
					steps = append(steps, MigStep{"M400", en, n, c, "backfill", "可空字段，回填 nil", false})
				} else {
					steps = append(steps, MigStep{"M401", en, n, c, "block", "类型 " + ty + " 没有默认值", true})
				}
			}
			if c := badType[n]; c > 0 {
				steps = append(steps, MigStep{"M403", en, n, c, "block", "已有值不是 " + fields[n]["type"].(Node)["name"].(string), true})
			}
		}
		ex := sortedKeysInt(extra)
		for _, k := range ex {
			steps = append(steps, MigStep{"M402", en, k, extra[k], "drop-field", "字段已从 entity 中删除", false})
		}
		if inv > 0 {
			steps = append(steps, MigStep{"M403", en, "", inv, "block", invMsg, true})
		}
	}
	for _, tn := range it.Store.TableNames() {
		if strings.HasPrefix(tn, "_") || declared[tn] {
			continue
		}
		n := 0
		it.Store.rawEach(tn, func(int64, *Record) { n++ })
		steps = append(steps, MigStep{"M404", tn, "", n, "drop-entity", "entity 已从源码中删除（需 --drop）", false})
	}
	return steps
}

func builtinZero(ty string) bool {
	switch ty {
	case "Int", "Float", "Bool", "Text", "String":
		return true
	}
	return false
}
func sortedKeysInt(m map[string]int) []string {
	o := []string{}
	for k := range m {
		o = append(o, k)
	}
	sort.Strings(o)
	return o
}

// ApplyMigration executes non-blocking steps. Returns number of rows touched.
func ApplyMigration(it *Interp, steps []MigStep, drop bool) (int, error) {
	for _, s := range steps {
		if s.Block {
			return 0, fmt.Errorf("%s %s.%s：%s（先解决阻塞项）", s.Code, s.Entity, s.Field, s.Detail)
		}
	}
	touched := 0
	ents := map[string]bool{}
	for _, s := range steps {
		if s.Action == "backfill" || s.Action == "drop-field" {
			ents[s.Entity] = true
		}
		if s.Action == "drop-entity" && drop {
			ids := []int64{}
			it.Store.rawEach(s.Entity, func(id int64, _ *Record) { ids = append(ids, id) })
			for _, id := range ids {
				it.Store.rawDel(s.Entity, id)
				touched++
			}
		}
	}
	for en := range ents {
		type kv struct {
			id int64
			r  *Record
		}
		rows := []kv{}
		it.Store.rawEach(en, func(id int64, r *Record) { rows = append(rows, kv{id, r}) })
		for _, x := range rows {
			full := it.NewRecord(it.Entities[en], x.r)
			nr := NewRec()
			for _, k := range full.Keys {
				if k != "id" {
					nr.Set(k, full.M[k])
				}
			}
			it.Store.rawPut(en, x.id, nr)
			touched++
		}
	}
	it.Store.MaybeCompact(true)
	return touched, nil
}

func cmdMigrate(it *Interp, asJSON, apply, drop bool) int {
	steps := PlanMigration(it)
	if asJSON && !apply {
		b, _ := json.MarshalIndent(map[string]any{"steps": steps, "ok": !anyBlock(steps)}, "", " ")
		fmt.Println(string(b))
		return boolExit(!anyBlock(steps))
	}
	if len(steps) == 0 {
		fmt.Println("schema 与数据一致，无需迁移。")
		return 0
	}
	for _, s := range steps {
		mark := "·"
		if s.Block {
			mark = "×"
		}
		f := s.Entity
		if s.Field != "" {
			f += "." + s.Field
		}
		fmt.Printf("%s %s %-20s %4d 行  %s  %s\n", mark, s.Code, f, s.Rows, s.Action, s.Detail)
	}
	if !apply {
		fmt.Println("\n这是计划（未写入）。确认后加 --apply 执行；删除整个实体还需 --drop。错误码说明：aether explain M400")
		return boolExit(!anyBlock(steps))
	}
	n, err := ApplyMigration(it, steps, drop)
	if err != nil {
		fmt.Println("未执行：" + err.Error())
		return 1
	}
	fmt.Printf("已迁移，改写 %d 行。\n", n)
	return 0
}
func anyBlock(s []MigStep) bool {
	for _, x := range s {
		if x.Block {
			return true
		}
	}
	return false
}
func boolExit(ok bool) int {
	if ok {
		return 0
	}
	return 1
}
