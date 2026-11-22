import express from "express";
import { Pool } from "pg";

const pool = new Pool({ connectionString: process.env.DATABASE_URL });
const app = express();
app.use(express.json());

app.get("/api/note", async (_req, res) => {
  res.json((await pool.query("SELECT * FROM note ORDER BY id")).rows);
});
app.get("/api/note/:id", async (req, res) => {
  const r = await pool.query("SELECT * FROM note WHERE id = $1", [req.params.id]);
  r.rows[0] ? res.json(r.rows[0]) : res.status(404).end();
});
app.post("/api/note", async (req, res) => {
  const { title, body, pinned = false } = req.body;
  const r = await pool.query("INSERT INTO note (title, body, pinned) VALUES ($1, $2, $3) RETURNING *", [title, body, pinned]);
  res.status(201).json(r.rows[0]);
});
app.put("/api/note/:id", async (req, res) => {
  const { title, body, pinned } = req.body;
  const r = await pool.query("UPDATE note SET title = $1, body = $2, pinned = $3 WHERE id = $4 RETURNING *", [title, body, pinned, req.params.id]);
  r.rows[0] ? res.json(r.rows[0]) : res.status(404).end();
});
app.delete("/api/note/:id", async (req, res) => {
  await pool.query("DELETE FROM note WHERE id = $1", [req.params.id]);
  res.status(204).end();
});
app.listen(8080);
