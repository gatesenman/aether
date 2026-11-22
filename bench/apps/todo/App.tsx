import React, { useEffect, useState } from "react";
import { BrowserRouter, Routes, Route, Link } from "react-router-dom";

interface Todo { id: number; title: string; done: boolean; priority: number; }

async function api<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const r = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json", "x-role": localStorage.getItem("role") ?? "" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!r.ok) throw new Error((await r.json()).error);
  return r.status === 204 ? (undefined as T) : r.json();
}

function useTodos() {
  const [todos, setTodos] = useState<Todo[]>([]);
  const reload = () => api<Todo[]>("/api/todo").then(setTodos);
  useEffect(() => { reload(); }, []);
  return { todos, reload };
}

function Home() {
  const { todos, reload } = useTodos();
  const [progress, setProgress] = useState("");
  const [title, setTitle] = useState("");
  const [priority, setPriority] = useState(1);
  const [error, setError] = useState("");
  useEffect(() => { api<{ text: string }>("/api/progress").then(p => setProgress(p.text)); }, [todos]);

  const run = async (f: () => Promise<unknown>) => {
    try { setError(""); await f(); await reload(); } catch (e) { setError(String(e)); }
  };

  return (
    <div>
      <h1>我的待办</h1>
      <div className="stat"><span>进度</span><b>{progress}</b></div>
      {error && <div className="error">{error}</div>}
      <form onSubmit={e => { e.preventDefault(); run(() => api("/api/todo", "POST", { title, priority })); setTitle(""); }}>
        <input value={title} onChange={e => setTitle(e.target.value)} placeholder="title" />
        <input type="number" value={priority} onChange={e => setPriority(Number(e.target.value))} />
        <button>＋ 新增 Todo</button>
      </form>
      <ul>
        {todos.map(t => (
          <li key={t.id}>
            {t.title}
            <button onClick={() => run(() => api(`/api/todo/${t.id}/toggle`, "POST"))}>toggle</button>
            <button onClick={() => run(() => api(`/api/todo/${t.id}/bump`, "POST"))}>bump</button>
            <button onClick={() => run(() => api(`/api/todo/${t.id}`, "DELETE"))}>✕</button>
          </li>
        ))}
      </ul>
      <button onClick={() => run(() => api("/api/clear_done", "POST"))}>清除已完成</button>
    </div>
  );
}

function TablePage() {
  const { todos } = useTodos();
  return (
    <div>
      <h1>全部任务</h1>
      <table>
        <thead><tr><th>id</th><th>title</th><th>done</th><th>priority</th></tr></thead>
        <tbody>
          {todos.map(t => (
            <tr key={t.id}><td>{t.id}</td><td>{t.title}</td><td>{String(t.done)}</td><td>{t.priority}</td></tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <nav><Link to="/">首页</Link> <Link to="/table">表格</Link></nav>
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/table" element={<TablePage />} />
      </Routes>
    </BrowserRouter>
  );
}
