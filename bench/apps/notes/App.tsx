import React, { useEffect, useState } from "react";

interface Note { id: number; title: string; body: string; pinned: boolean; }

export default function App() {
  const [notes, setNotes] = useState<Note[]>([]);
  const [form, setForm] = useState({ title: "", body: "", pinned: false });
  const reload = () => fetch("/api/note").then(r => r.json()).then(setNotes);
  useEffect(() => { reload(); }, []);
  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    await fetch("/api/note", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(form) });
    setForm({ title: "", body: "", pinned: false });
    reload();
  };
  const del = async (id: number) => { await fetch(`/api/note/${id}`, { method: "DELETE" }); reload(); };
  return (
    <div>
      <h1>Note</h1>
      <form onSubmit={add}>
        <input value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} placeholder="title" />
        <input value={form.body} onChange={e => setForm({ ...form, body: e.target.value })} placeholder="body" />
        <label><input type="checkbox" checked={form.pinned} onChange={e => setForm({ ...form, pinned: e.target.checked })} /> pinned</label>
        <button>＋ 新增 Note</button>
      </form>
      <ul>{notes.map(n => <li key={n.id}>{n.title} <button onClick={() => del(n.id)}>✕</button></li>)}</ul>
    </div>
  );
}
