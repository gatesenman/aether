package main

// Durable, resumable flows (v0.5): every flow run is persisted in the app's own store (_FlowRun),
// checkpointed after each step (ctx + log). Steps may declare `retry N backoff 200ms`; a failing
// step is retried with exponential backoff. A failed or interrupted run (crash / restart) can be
// resumed from the failed step with the saved ctx: POST /api/_flows/runs/<id>/resume,
// `aether flow resume <file> <id>`, or resume_flow(id) from code.

import (
	"bytes"
	"encoding/json"
	"math"
	"time"
)

var flowRunDecl = Node{"name": "_FlowRun", "fields": []any{
	Node{"name": "flow", "type": Node{"name": "Text"}}, Node{"name": "status", "type": Node{"name": "Text"}},
	Node{"name": "step", "type": Node{"name": "Int"}}, Node{"name": "ctx", "type": Node{"name": "Text"}},
	Node{"name": "log", "type": Node{"name": "Text"}}, Node{"name": "error", "type": Node{"name": "Text"}},
	Node{"name": "started", "type": Node{"name": "Text"}}, Node{"name": "updated", "type": Node{"name": "Text"}}}}

// FlowSleep is replaceable in tests.
var FlowSleep = func(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func jsonDecodeValue(s string) Value {
	if s == "" {
		return NewRec()
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return NewRec()
	}
	return fromJSON(v)
}

func (it *Interp) RunFlow(name string, ctx Value) *Record {
	f, ok := it.Flows[name]
	if !ok {
		panic(aeErr("R050", 0, "no flow '%s'", name))
	}
	if ctx == nil {
		ctx = NewRec()
	}
	it.Store.Ensure(flowRunDecl)
	now := time.Now().Format(time.RFC3339)
	run := NewRec()
	run.Set("flow", name)
	run.Set("status", "running")
	run.Set("step", int64(0))
	run.Set("ctx", toJSON(ctx))
	run.Set("log", "[]")
	run.Set("started", now)
	run.Set("updated", now)
	rec := it.Store.Insert("_FlowRun", run)
	return it.continueFlow(f, rec.M["id"].(int64), 0, ctx, &List{Items: []Value{}})
}

func (it *Interp) ResumeFlow(id int64) *Record {
	it.Store.Ensure(flowRunDecl)
	r := it.Store.Get("_FlowRun", id)
	if r == nil {
		panic(aeErr("R051", 0, "no flow run #%d", id))
	}
	if r.M["status"] == "done" {
		panic(aeErr("R052", 0, "flow run #%d already completed", id))
	}
	f, ok := it.Flows[show(r.M["flow"])]
	if !ok {
		panic(aeErr("R050", 0, "flow '%s' no longer exists", show(r.M["flow"])))
	}
	step, _ := r.M["step"].(int64)
	logv, _ := jsonDecodeValue(show(r.M["log"])).(*List)
	if logv == nil {
		logv = &List{Items: []Value{}}
	}
	// drop the failed attempt entry, keep completed steps
	keep := []Value{}
	for _, x := range logv.Items {
		if rr, ok := x.(*Record); ok && truthy(rr.M["ok"]) {
			keep = append(keep, x)
		}
	}
	logv.Items = keep
	return it.continueFlow(f, id, int(step), jsonDecodeValue(show(r.M["ctx"])), logv)
}

// RecoverFlows marks runs left "running" by a crash as "interrupted" (resumable). Returns their ids.
func (it *Interp) RecoverFlows() []int64 {
	it.Store.Ensure(flowRunDecl)
	ids := []int64{}
	for _, r := range it.Store.All("_FlowRun") {
		if r.M["status"] == "running" {
			u := NewRec()
			u.Set("status", "interrupted")
			id := r.M["id"].(int64)
			it.Store.Update("_FlowRun", id, u)
			ids = append(ids, id)
		}
	}
	return ids
}

func (it *Interp) FlowRuns() *List {
	it.Store.Ensure(flowRunDecl)
	l := &List{Items: []Value{}}
	for _, r := range it.Store.All("_FlowRun") {
		o := NewRec()
		for _, k := range []string{"id", "flow", "status", "step", "error", "started", "updated"} {
			o.Set(k, r.M[k])
		}
		o.Set("ctx", jsonDecodeValue(show(r.M["ctx"])))
		o.Set("steps", jsonDecodeValue(show(r.M["log"])))
		l.Items = append(l.Items, o)
	}
	return l
}

func (it *Interp) continueFlow(f Node, runID int64, start int, ctx Value, steps *List) *Record {
	env := NewEnv(it.Globals)
	env.Define("ctx", ctx, true)
	log := NewRec()
	log.Set("flow", f["name"])
	log.Set("run", runID)
	log.Set("steps", steps)
	log.Set("ok", true)
	all := f["steps"].([]any)
	status, errMsg := "done", ""
	checkpoint := func(step int, st string) {
		u := NewRec()
		u.Set("status", st)
		u.Set("step", int64(step))
		u.Set("ctx", toJSON(env.Get("ctx", 0)))
		u.Set("log", toJSON(steps))
		u.Set("error", errMsg)
		u.Set("updated", time.Now().Format(time.RFC3339))
		it.Store.Update("_FlowRun", runID, u)
	}
	for i := start; i < len(all); i++ {
		st := all[i].(Node)
		retry, _ := st["retry"].(int)
		backoff, _ := st["backoff"].(int)
		var v Value
		var err error
		attempts := 0
		t0 := time.Now()
		for {
			attempts++
			env.Define("attempt", int64(attempts), true)
			err = catch(func() { v = it.execBlock(st["body"].(Node), env) })
			if err == nil || attempts > retry {
				break
			}
			FlowSleep(int(math.Min(float64(backoff)*math.Pow(2, float64(attempts-1)), 30000)))
		}
		r := NewRec()
		r.Set("step", st["name"])
		r.Set("attempts", int64(attempts))
		if err != nil {
			r.Set("ok", false)
			if e, isA := err.(*AeError); isA {
				r.Set("error", e.Msg)
			} else {
				r.Set("error", err.Error())
			}
			steps.Items = append(steps.Items, r)
			log.Set("ok", false)
			status, errMsg = "failed", show(r.M["error"])
			checkpoint(i, status)
			log.Set("ctx", env.Get("ctx", 0))
			log.Set("status", status)
			log.Set("resumable", true)
			return log
		}
		r.Set("ok", true)
		r.Set("value", show(v))
		r.Set("ms", math.Round(float64(time.Since(t0).Microseconds())/10)/100)
		steps.Items = append(steps.Items, r)
		checkpoint(i+1, "running")
	}
	checkpoint(len(all), status)
	log.Set("ctx", env.Get("ctx", 0))
	log.Set("status", status)
	return log
}
