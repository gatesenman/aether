import express, { Request, Response, NextFunction } from "express";
import { Pool } from "pg";

const pool = new Pool({ connectionString: process.env.DATABASE_URL });
const app = express();
app.use(express.json());

interface Todo { id: number; title: string; done: boolean; priority: number; }

function validate(t: Partial<Todo>): string | null {
  if (t.title !== undefined && t.title.length === 0) return "title must not be empty";
  if (t.priority !== undefined && (t.priority < 1 || t.priority > 5)) return "priority must be 1..5";
  return null;
}

function requireRole(role: string) {
  return (req: Request, res: Response, next: NextFunction) => {
    if (req.header("x-role") !== role) return res.status(403).json({ error: "forbidden" });
    next();
  };
}

async function getTodo(id: number): Promise<Todo | undefined> {
  const r = await pool.query<Todo>("SELECT * FROM todo WHERE id = $1", [id]);
  return r.rows[0];
}

app.get("/api/todo", async (_req, res) => {
  const r = await pool.query<Todo>("SELECT * FROM todo ORDER BY id");
  res.json(r.rows);
});

app.get("/api/todo/:id", async (req, res) => {
  const t = await getTodo(Number(req.params.id));
  if (!t) return res.status(404).json({ error: "not found" });
  res.json(t);
});

app.post("/api/todo", async (req, res) => {
  const { title, done = false, priority = 1 } = req.body;
  const err = validate({ title, priority });
  if (err) return res.status(400).json({ error: err });
  const r = await pool.query<Todo>(
    "INSERT INTO todo (title, done, priority) VALUES ($1, $2, $3) RETURNING *",
    [title, done, priority]
  );
  res.status(201).json(r.rows[0]);
});

app.put("/api/todo/:id", async (req, res) => {
  const t = await getTodo(Number(req.params.id));
  if (!t) return res.status(404).json({ error: "not found" });
  const next = { ...t, ...req.body };
  const err = validate(next);
  if (err) return res.status(400).json({ error: err });
  const r = await pool.query<Todo>(
    "UPDATE todo SET title = $1, done = $2, priority = $3 WHERE id = $4 RETURNING *",
    [next.title, next.done, next.priority, t.id]
  );
  res.json(r.rows[0]);
});

app.delete("/api/todo/:id", async (req, res) => {
  await pool.query("DELETE FROM todo WHERE id = $1", [Number(req.params.id)]);
  res.status(204).end();
});

app.post("/api/todo/:id/toggle", async (req, res) => {
  const t = await getTodo(Number(req.params.id));
  if (!t) return res.status(404).json({ error: "not found" });
  const r = await pool.query<Todo>("UPDATE todo SET done = NOT done WHERE id = $1 RETURNING *", [t.id]);
  res.json(r.rows[0]);
});

app.post("/api/todo/:id/bump", async (req, res) => {
  const t = await getTodo(Number(req.params.id));
  if (!t) return res.status(404).json({ error: "not found" });
  if (!(t.priority < 5)) return res.status(400).json({ error: "precondition failed: priority < 5" });
  const r = await pool.query<Todo>("UPDATE todo SET priority = priority + 1 WHERE id = $1 RETURNING *", [t.id]);
  res.json(r.rows[0]);
});

app.post("/api/clear_done", requireRole("admin"), async (_req, res) => {
  await pool.query("DELETE FROM todo WHERE done = true");
  res.status(204).end();
});

app.get("/api/progress", async (_req, res) => {
  const r = await pool.query("SELECT count(*)::int AS n, count(*) FILTER (WHERE done)::int AS d FROM todo");
  const { n, d } = r.rows[0];
  res.json({ text: n === 0 ? "还没有任务" : `${d} / ${n} 已完成` });
});

app.listen(Number(process.env.PORT ?? 8080));
