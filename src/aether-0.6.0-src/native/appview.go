package main

// Native renderer for Aether views: turns the compiled UI spec into pixels with our own layout engine.

import (
	"fmt"
	"image"
	"image/color"
	"strings"
)

type Hit struct {
	R image.Rectangle
	F func()
}
type UI struct {
	C     *Canvas
	Hits  []Hit
	Focus string
	Vals  map[string]string
}

func (u *UI) Click(x, y int) bool {
	for i := len(u.Hits) - 1; i >= 0; i-- {
		if (image.Point{x, y}).In(u.Hits[i].R) {
			u.Hits[i].F()
			return true
		}
	}
	u.Focus = ""
	return false
}
func (u *UI) Button(x, y int, label string, bg, fg colorT, clip image.Rectangle, f func()) int {
	icon := ""
	if strings.HasPrefix(label, "$") { // "$codicon-name label" renders a font icon before the label
		icon, label, _ = strings.Cut(label[1:], " ")
	}
	w := Measure(label, 13, false) + 20
	if icon != "" {
		w += 20
		if label == "" {
			w -= 8
		}
	}
	r := image.Rect(x, y, x+w, y+26)
	u.C.RoundRect(r.Intersect(clip), 6, bg)
	tx := x + 10
	if icon != "" {
		u.C.Icon(x+8, y+6, icon, 15, fg, r.Intersect(clip))
		tx += 20
	}
	u.C.Text(tx, y+5, label, 13, false, fg, clip)
	if r.Overlaps(clip) {
		u.Hits = append(u.Hits, Hit{r.Intersect(clip), f})
	}
	return w
}
func (u *UI) Input(key string, x, y, w int, placeholder string, clip image.Rectangle) {
	r := image.Rect(x, y, x+w, y+30)
	border := cLine
	if u.Focus == key {
		border = cAccent
	}
	u.C.RoundRect(r.Intersect(clip), 6, border)
	u.C.RoundRect(r.Inset(1).Intersect(clip), 5, cWhite)
	v := u.Vals[key]
	if v == "" && u.Focus != key {
		u.C.Text(x+9, y+7, placeholder, 13, false, cMuted, r.Intersect(clip))
	} else {
		tw := u.C.Text(x+9, y+7, v, 13, false, cInk, r.Intersect(clip))
		if u.Focus == key {
			u.C.Fill(image.Rect(x+10+tw, y+7, x+11+tw, y+23).Intersect(clip), cAccent)
		}
	}
	if r.Overlaps(clip) {
		u.Hits = append(u.Hits, Hit{r.Intersect(clip), func() { u.Focus = key }})
	}
}

type AppView struct {
	App    *App
	Cur    int
	Scroll int
	Role   string
	Msg    string
	MsgErr bool
}

func (av *AppView) call(u *UI, method, path string, body string) map[string]any {
	code, _, out := av.App.Handle(method, path, []byte(body), av.Role)
	r := decodeBody(out)
	if code >= 400 {
		av.Msg, av.MsgErr = fmt.Sprint(r.M["error"]), true
	} else {
		av.MsgErr = false
	}
	m := map[string]any{}
	for _, k := range r.Keys {
		m[k] = r.M[k]
	}
	return m
}

// Render draws the app inside rect r.
func (av *AppView) Render(u *UI, r image.Rectangle) {
	c := u.C
	if av.Role == "" {
		av.Role = "user"
	}
	if av.App == nil {
		c.Fill(r, cBg)
		c.Text(r.Min.X+20, r.Min.Y+20, "（没有可预览的应用：请修复错误后刷新）", 14, false, cMuted, r)
		return
	}
	spec := av.App.Spec()
	views := spec["views"].([]any)
	if av.Cur >= len(views) {
		av.Cur = 0
	}
	// header bar (gradient)
	hb := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+48)
	for x := hb.Min.X; x < hb.Max.X; x++ {
		t := float64(x-hb.Min.X) / float64(max(1, hb.Dx()))
		col := cAccent
		col.R = uint8(float64(cAccent.R)*(1-t) + float64(cAccent2.R)*t)
		col.G = uint8(float64(cAccent.G)*(1-t) + float64(cAccent2.G)*t)
		col.B = uint8(float64(cAccent.B)*(1-t) + float64(cAccent2.B)*t)
		c.Fill(image.Rect(x, hb.Min.Y, x+1, hb.Max.Y), col)
	}
	x := r.Min.X + 18
	x += c.Text(x, r.Min.Y+13, show(spec["module"]), 18, false, cWhite, hb) + 22
	for i, v := range views {
		name := show(v.(map[string]any)["name"])
		w := Measure(name, 15, false)
		col := rgb(0xe0e7ff)
		if i == av.Cur {
			col = cWhite
			c.Fill(image.Rect(x, r.Min.Y+38, x+w, r.Min.Y+40), cWhite)
		}
		c.Text(x, r.Min.Y+15, name, 15, false, col, hb)
		ii := i
		u.Hits = append(u.Hits, Hit{image.Rect(x-4, r.Min.Y+8, x+w+4, r.Min.Y+42), func() { av.Cur = ii; av.Scroll = 0 }})
		x += w + 18
	}
	// right side: flows + role
	rx := r.Max.X - 14
	roleLbl := "$account 角色: " + av.Role
	rx -= Measure("角色: "+av.Role, 13, false) + 40
	u.Button(rx, r.Min.Y+11, roleLbl, rgb(0x6d5ef5), cWhite, hb, func() {
		roles := []string{"user", "admin", "guest"}
		for i, ro := range roles {
			if ro == av.Role {
				av.Role = roles[(i+1)%len(roles)]
				return
			}
		}
		av.Role = "user"
	})
	for _, f := range spec["flows"].([]any) {
		fn := show(f)
		lbl := "$play " + fn
		rx -= Measure(fn, 13, false) + 48
		u.Button(rx, r.Min.Y+11, lbl, rgb(0x6d5ef5), cWhite, hb, func() {
			res := av.call(u, "POST", "/api/flow/"+fn, "{}")
			parts := []string{}
			if st, ok := res["steps"].(*List); ok {
				for _, s := range st.Items {
					sr := s.(*Record)
					mark := "成功"
					if !truthy(sr.M["ok"]) {
						mark = "失败"
					}
					parts = append(parts, show(sr.M["step"])+"("+mark+")")
				}
			}
			av.Msg = "流程 " + fn + "：" + strings.Join(parts, " → ")
			av.MsgErr = !truthy(res["ok"])
		})
	}
	body := image.Rect(r.Min.X, hb.Max.Y, r.Max.X, r.Max.Y)
	c.Fill(body, cBg)
	if av.Msg != "" {
		mb := image.Rect(body.Min.X, body.Min.Y, body.Max.X, body.Min.Y+28)
		bg, fg := rgb(0xdcfce7), cOK
		if av.MsgErr {
			bg, fg = cDangerB, cDanger
		}
		c.Fill(mb, bg)
		c.Text(mb.Min.X+16, mb.Min.Y+6, av.Msg, 13, false, fg, mb)
		u.Hits = append(u.Hits, Hit{mb, func() { av.Msg = "" }})
		body.Min.Y += 28
	}
	cw := min(body.Dx()-40, 820)
	left := body.Min.X + (body.Dx()-cw)/2
	y := body.Min.Y + 16 - av.Scroll
	v := views[av.Cur].(map[string]any)
	statX := -1
	for _, it := range v["items"].([]any) {
		el := it.(map[string]any)
		kind := el["kind"].(string)
		if kind != "stat" && statX >= 0 {
			statX = -1
			y += 78
		}
		switch kind {
		case "title":
			c.Text(left, y, show(el["text"]), 24, false, cInk, body)
			y += 44
		case "section":
			c.Text(left, y, show(el["text"]), 17, false, cInk, body)
			y += 32
		case "text", "error":
			col := cInk
			if kind == "error" {
				col = cDanger
			}
			card := image.Rect(left, y, left+cw, y+44)
			c.RoundRect(card.Intersect(body), 10, cCard)
			c.Text(left+16, y+13, show(el["text"]), 14, false, col, card.Intersect(body))
			y += 56
		case "stat":
			if statX < 0 {
				statX = left
			}
			lbl, val := show(el["label"]), show(el["value"])
			w := max(Measure(lbl, 13, false), Measure(val, 26, false)) + 34
			card := image.Rect(statX, y, statX+w, y+68)
			c.RoundRect(card.Intersect(body), 10, cCard)
			c.Text(statX+16, y+10, lbl, 13, false, cMuted, card.Intersect(body))
			c.Text(statX+16, y+28, val, 26, false, cAccent, card.Intersect(body))
			statX += w + 14
		case "button":
			action := show(el["action"])
			lbl := show(el["label"])
			for _, a := range spec["actions"].([]any) {
				am := a.(map[string]any)
				if am["name"] == action {
					if al, _ := am["allow"].([]any); len(al) > 0 {
						lbl = "$lock " + lbl
					}
				}
			}
			u.Button(left, y, lbl, cAccent, cWhite, body, func() {
				av.call(u, "POST", "/api/action/"+action, "{}")
				if !av.MsgErr {
					av.Msg = "已执行 " + action
				}
			})
			y += 40
		case "form":
			e := av.entity(spec, show(el["entity"]))
			fields := e["fields"].([]any)
			card := image.Rect(left, y, left+cw, y+0)
			// layout inputs in rows
			fx, fy := left+16, y+16
			iw := 200
			type pos struct{ x, y int }
			ps := []pos{}
			for range fields {
				if fx+iw > left+cw-16 {
					fx = left + 16
					fy += 40
				}
				ps = append(ps, pos{fx, fy})
				fx += iw + 10
			}
			lbl := "$add 新增 " + show(e["name"])
			bw := Measure(lbl[5:], 13, false) + 40
			if fx+bw > left+cw-16 {
				fx = left + 16
				fy += 40
			}
			card.Max.Y = fy + 46
			c.RoundRect(card.Intersect(body), 10, cCard)
			for i, f := range fields {
				fm := f.(map[string]any)
				key := "form:" + show(e["name"]) + ":" + show(fm["name"])
				if fm["type"] == "Bool" {
					on := u.Vals[key] == "true"
					box := image.Rect(ps[i].x, ps[i].y+7, ps[i].x+16, ps[i].y+23)
					c.Fill(box.Intersect(body), cLine)
					if on {
						c.Fill(box.Inset(3).Intersect(body), cAccent)
					} else {
						c.Fill(box.Inset(1).Intersect(body), cWhite)
					}
					c.Text(ps[i].x+22, ps[i].y+6, show(fm["name"]), 14, false, cInk, body)
					k := key
					u.Hits = append(u.Hits, Hit{image.Rect(ps[i].x, ps[i].y, ps[i].x+iw, ps[i].y+30).Intersect(body), func() {
						if u.Vals[k] == "true" {
							u.Vals[k] = ""
						} else {
							u.Vals[k] = "true"
						}
					}})
				} else {
					u.Input(key, ps[i].x, ps[i].y, iw, show(fm["name"])+" ("+show(fm["type"])+")", body)
				}
			}
			en := show(e["name"])
			u.Button(fx, fy+2, lbl, cAccent, cWhite, body, func() {
				rec := NewRec()
				for _, f := range fields {
					fm := f.(map[string]any)
					key := "form:" + en + ":" + show(fm["name"])
					val := u.Vals[key]
					switch fm["type"] {
					case "Bool":
						rec.Set(show(fm["name"]), val == "true")
					case "Int", "Float":
						n, _ := parseNum(val)
						if fm["type"] == "Int" && n == float64(int64(n)) {
							rec.Set(show(fm["name"]), int64(n))
						} else {
							rec.Set(show(fm["name"]), n)
						}
					default:
						rec.Set(show(fm["name"]), val)
					}
				}
				av.call(u, "POST", "/api/"+en, toJSON(rec))
				if !av.MsgErr {
					av.Msg = "已新增 " + en
					for _, f := range fields {
						delete(u.Vals, "form:"+en+":"+show(f.(map[string]any)["name"]))
					}
				}
			})
			y = card.Max.Y + 14
		case "list", "table":
			e := av.entity(spec, show(el["entity"]))
			en := show(e["name"])
			rows := av.App.It.Store.All(en)
			fields := e["fields"].([]any)
			lf := ""
			if s, ok := el["label_field"].(string); ok {
				lf = s
			} else {
				for _, f := range fields {
					if f.(map[string]any)["type"] == "Text" {
						lf = show(f.(map[string]any)["name"])
						break
					}
				}
				if lf == "" && len(fields) > 0 {
					lf = show(fields[0].(map[string]any)["name"])
				}
			}
			bf := ""
			for _, f := range fields {
				if f.(map[string]any)["type"] == "Bool" {
					bf = show(f.(map[string]any)["name"])
					break
				}
			}
			rh := 40
			hdr := 0
			if kind == "table" {
				hdr = 32
			}
			h := max(1, len(rows))*rh + 16 + hdr
			card := image.Rect(left, y, left+cw, y+h)
			c.RoundRect(card.Intersect(body), 10, cCard)
			cl := card.Intersect(body)
			if len(rows) == 0 {
				c.Text(left+16, y+18, "暂无数据", 14, false, cMuted, cl)
			}
			if kind == "table" {
				colw := (cw - 220) / max(1, len(fields)+1)
				c.Text(left+16, y+12, "id", 13, false, cMuted, cl)
				for i, f := range fields {
					c.Text(left+16+colw*(i+1), y+12, show(f.(map[string]any)["name"]), 13, false, cMuted, cl)
				}
			}
			for i, row := range rows {
				ry := y + 8 + hdr + i*rh
				if i > 0 {
					c.Fill(image.Rect(left+12, ry, left+cw-12, ry+1).Intersect(body), cLine)
				}
				if kind == "table" {
					colw := (cw - 220) / max(1, len(fields)+1)
					c.Text(left+16, ry+11, show(row.M["id"]), 14, false, cInk, cl)
					for j, f := range fields {
						c.Text(left+16+colw*(j+1), ry+11, show(row.M[show(f.(map[string]any)["name"])]), 14, false, cInk, image.Rect(left+16+colw*(j+1), ry, left+16+colw*(j+2)-6, ry+rh).Intersect(cl))
					}
				} else {
					col := cInk
					if bf != "" && truthy(row.M[bf]) {
						col = rgb(0x9aa0b8)
					}
					tw := c.Text(left+16, ry+10, show(row.M[lf]), 15, false, col, cl)
					if bf != "" && truthy(row.M[bf]) {
						c.Fill(image.Rect(left+16, ry+20, left+16+tw, ry+21).Intersect(cl), col)
					}
					extra := []string{}
					for _, f := range fields {
						fnm := show(f.(map[string]any)["name"])
						if fnm != lf {
							extra = append(extra, fnm+": "+show(row.M[fnm]))
						}
					}
					c.Text(left+24+tw, ry+12, strings.Join(extra, " · "), 13, false, cMuted, image.Rect(left, ry, left+cw-230, ry+rh).Intersect(cl))
				}
				// row buttons, right aligned
				bx := left + cw - 16
				id := show(row.M["id"])
				bx -= Measure("删除", 13, false) + 20
				u.Button(bx, ry+7, "删除", cDangerB, cDanger, cl, func() { av.call(u, "DELETE", "/api/"+en+"/"+id, "") })
				ras := e["row_actions"].([]any)
				for k := len(ras) - 1; k >= 0; k-- {
					an := show(ras[k])
					bx -= Measure(an, 13, false) + 26
					pname := av.actionParam(spec, an)
					u.Button(bx, ry+7, an, cGhost, rgb(0x334155), cl, func() {
						av.call(u, "POST", "/api/action/"+an, `{"`+pname+`": `+id+`}`)
					})
				}
			}
			y += h + 14
		case "chart":
			e := av.entity(spec, show(el["entity"]))
			en := show(e["name"])
			rows := av.App.It.Store.All(en)
			f := show(el["field"])
			lf := ""
			for _, fl := range e["fields"].([]any) {
				if fl.(map[string]any)["type"] == "Text" {
					lf = show(fl.(map[string]any)["name"])
					break
				}
			}
			mx := 1.0
			for _, rw := range rows {
				if n, ok := num(rw.M[f]); ok && n > mx {
					mx = n
				}
			}
			h := max(1, len(rows))*32 + 16
			card := image.Rect(left, y, left+cw, y+h)
			c.RoundRect(card.Intersect(body), 10, cCard)
			for i, rw := range rows {
				ry := y + 8 + i*32
				c.Text(left+16, ry+6, show(rw.M[lf]), 14, false, cInk, card.Intersect(body))
				n, _ := num(rw.M[f])
				bw := int(n / mx * float64(cw-260))
				c.RoundRect(image.Rect(left+160, ry+6, left+160+max(bw, 2), ry+24).Intersect(body), 4, rgb(0x818cf8))
				c.Text(left+170+bw, ry+6, show(rw.M[f]), 13, false, cMuted, card.Intersect(body))
			}
			y += h + 14
		}
	}
	if statX >= 0 {
		y += 78
	}
	// scroll bounds
	total := y + av.Scroll - body.Min.Y
	if av.Scroll > max(0, total-body.Dy()+20) {
		av.Scroll = max(0, total-body.Dy()+20)
	}
}
func (av *AppView) entity(spec map[string]any, name string) map[string]any {
	for _, e := range spec["entities"].([]any) {
		if e.(map[string]any)["name"] == name {
			return e.(map[string]any)
		}
	}
	return map[string]any{"name": name, "fields": []any{}, "row_actions": []any{}}
}
func (av *AppView) actionParam(spec map[string]any, an string) string {
	for _, a := range spec["actions"].([]any) {
		am := a.(map[string]any)
		if am["name"] == an {
			if ps := am["params"].([]any); len(ps) > 0 {
				return show(ps[0].(map[string]any)["name"])
			}
		}
	}
	return "x"
}

// Type alias to keep signatures short
type colorT = color.RGBA
