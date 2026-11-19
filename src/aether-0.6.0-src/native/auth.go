package main

// Built-in authentication & permissions for generated apps (v0.5).
//   auth {
//     roles admin, editor, user      // first role = administrator
//     default user                   // role given to new sign-ups
//     signup open                    // open | closed (closed: only the admin can add users)
//     login required                 // required | optional
//     can editor write Post          // entity rules: read | write | manage
//     can user read Post
//   }
// Passwords: PBKDF2-HMAC-SHA256 (120k iterations, per-user salt). Sessions: HMAC-signed bearer
// tokens (7 days) with a per-app secret persisted in the store; the role is re-read from the user
// record on every request, so role changes apply immediately. Without an auth block the app runs
// in development mode (role picked in the preview), exactly as before.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var userDecl = Node{"name": "_User", "fields": []any{
	Node{"name": "username", "type": Node{"name": "Text"}}, Node{"name": "pw", "type": Node{"name": "Text"}},
	Node{"name": "role", "type": Node{"name": "Text"}}, Node{"name": "created", "type": Node{"name": "Text"}}}}
var secretDecl = Node{"name": "_Secret", "fields": []any{Node{"name": "key", "type": Node{"name": "Text"}}}}

func pbkdf2(pw, salt []byte, iter int) []byte {
	mac := hmac.New(sha256.New, pw)
	mac.Write(salt)
	mac.Write([]byte{0, 0, 0, 1})
	u := mac.Sum(nil)
	out := append([]byte{}, u...)
	for i := 1; i < iter; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}
func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	return "pbkdf2$120000$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(pbkdf2([]byte(pw), salt, 120000))
}
func checkPassword(pw, stored string) bool {
	p := strings.Split(stored, "$")
	if len(p) != 4 {
		return false
	}
	n, _ := strconv.Atoi(p[1])
	salt, _ := hex.DecodeString(p[2])
	want, _ := hex.DecodeString(p[3])
	return subtle.ConstantTimeCompare(pbkdf2([]byte(pw), salt, n), want) == 1
}

func (a *App) authOn() bool { return a.It.Auth != nil }
func (a *App) roles() []string {
	o := []string{}
	for _, r := range a.It.Auth["roles"].([]any) {
		o = append(o, r.(string))
	}
	return o
}
func (a *App) adminRole() string { return a.roles()[0] }

func (a *App) secret() []byte {
	st := a.It.Store
	st.Ensure(secretDecl)
	if rs := st.All("_Secret"); len(rs) > 0 {
		b, _ := hex.DecodeString(show(rs[0].M["key"]))
		return b
	}
	k := make([]byte, 32)
	rand.Read(k)
	r := NewRec()
	r.Set("key", hex.EncodeToString(k))
	st.Insert("_Secret", r)
	return k
}
func (a *App) token(uid int64) string {
	payload := fmt.Sprintf("%d|%d", uid, time.Now().Add(7*24*time.Hour).Unix())
	m := hmac.New(sha256.New, a.secret())
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// userFromToken returns the user record or nil.
func (a *App) userFromToken(tok string) *Record {
	i := strings.IndexByte(tok, '.')
	if i < 0 {
		return nil
	}
	payload, err1 := base64.RawURLEncoding.DecodeString(tok[:i])
	sig, err2 := base64.RawURLEncoding.DecodeString(tok[i+1:])
	if err1 != nil || err2 != nil {
		return nil
	}
	m := hmac.New(sha256.New, a.secret())
	m.Write(payload)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return nil
	}
	parts := strings.Split(string(payload), "|")
	if len(parts) != 2 {
		return nil
	}
	uid, _ := strconv.ParseInt(parts[0], 10, 64)
	exp, _ := strconv.ParseInt(parts[1], 10, 64)
	if time.Now().Unix() > exp {
		return nil
	}
	a.It.Store.Ensure(userDecl)
	return a.It.Store.Get("_User", uid)
}
func publicUser(u *Record) any {
	if u == nil {
		return nil
	}
	return map[string]any{"id": u.M["id"], "username": u.M["username"], "role": u.M["role"]}
}

// identity splits the transport "role" (header role \x00 bearer token) into user + effective role.
func (a *App) identity(raw string) (*Record, string) {
	if raw == "\x01dev" { // IDE developer tools (data browser / API tester via LSP only; not reachable over HTTP)
		if a.authOn() {
			return nil, a.adminRole()
		}
		return nil, "admin"
	}
	hdr, tok, _ := strings.Cut(raw, "\x00")
	if !a.authOn() {
		if hdr == "" {
			hdr = "user"
		}
		return nil, hdr
	}
	if u := a.userFromToken(tok); u != nil {
		return u, show(u.M["role"])
	}
	return nil, "guest"
}

var reUserID = regexp.MustCompile(`^/api/_auth/users/(\d+)$`)
var reUsername = regexp.MustCompile(`^[\p{L}\p{N}_.@-]{2,40}$`)

func (a *App) handleAuth(method, path string, data *Record, user *Record, role string) (int, string, []byte) {
	st := a.It.Store
	st.Ensure(userDecl)
	if !a.authOn() {
		return errResp(404, "this app has no auth block")
	}
	find := func(name string) *Record {
		for _, u := range st.All("_User") {
			if u.M["username"] == name {
				return u
			}
		}
		return nil
	}
	isAdmin := role == a.adminRole()
	switch {
	case path == "/api/_auth/me":
		return jsonResp(200, map[string]any{"user": publicUser(user), "role": role})
	case path == "/api/_auth/signup" && method == "POST":
		name, pw := strings.TrimSpace(show(data.M["username"])), show(data.M["password"])
		first := len(st.All("_User")) == 0
		if a.It.Auth["signup"] == "closed" && !first && !isAdmin {
			return errResp(403, "注册已关闭，请联系管理员")
		}
		if !reUsername.MatchString(name) {
			return errResp(400, "用户名需 2-40 个字母/数字/下划线")
		}
		if len([]rune(pw)) < 6 {
			return errResp(400, "密码至少 6 位")
		}
		if find(name) != nil {
			return errResp(409, "用户名已存在")
		}
		r := a.It.Auth["default"].(string)
		if r == "" {
			rs := a.roles()
			r = rs[len(rs)-1]
		}
		if first {
			r = a.adminRole()
		} else if isAdmin && data.M["role"] != nil {
			r = show(data.M["role"])
		}
		u := NewRec()
		u.Set("username", name)
		u.Set("pw", hashPassword(pw))
		u.Set("role", r)
		u.Set("created", time.Now().Format(time.RFC3339))
		nu := st.Insert("_User", u)
		return jsonResp(201, map[string]any{"token": a.token(nu.M["id"].(int64)), "user": publicUser(nu)})
	case path == "/api/_auth/login" && method == "POST":
		u := find(strings.TrimSpace(show(data.M["username"])))
		if u == nil || !checkPassword(show(data.M["password"]), show(u.M["pw"])) {
			return errResp(401, "用户名或密码错误")
		}
		return jsonResp(200, map[string]any{"token": a.token(u.M["id"].(int64)), "user": publicUser(u)})
	case path == "/api/_auth/users" && method == "GET":
		if !isAdmin {
			return errResp(403, "需要管理员")
		}
		l := []any{}
		for _, u := range st.All("_User") {
			l = append(l, publicUser(u))
		}
		return jsonResp(200, l)
	}
	if m := reUserID.FindStringSubmatch(path); m != nil && (method == "PUT" || method == "PATCH") {
		if !isAdmin {
			return errResp(403, "需要管理员")
		}
		id, _ := strconv.ParseInt(m[1], 10, 64)
		nr := show(data.M["role"])
		ok := false
		for _, r := range a.roles() {
			ok = ok || r == nr
		}
		if !ok {
			return errResp(400, "未知角色 "+nr)
		}
		upd := NewRec()
		upd.Set("role", nr)
		return jsonResp(200, publicUser(st.Update("_User", id, upd)))
	}
	return errResp(404, "no auth route "+method+" "+path)
}

// entityAllowed applies `can` rules. No rule for an entity/op => allowed (when logged in if login required).
func (a *App) entityAllowed(role, ent, op string) bool {
	if !a.authOn() || role == a.adminRole() {
		return true
	}
	ruled, ok := false, false
	for _, x := range a.It.Auth["rules"].([]any) {
		r := x.(Node)
		if !(r["op"] == op || r["op"] == "manage" || (op == "read" && r["op"] == "write")) {
			continue
		}
		for _, e := range r["entities"].([]any) {
			if e == ent {
				ruled = true
				for _, ro := range r["roles"].([]any) {
					if ro == role {
						ok = true
					}
				}
			}
		}
	}
	return !ruled || ok
}
