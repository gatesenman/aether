CREATE TABLE todo (
  id SERIAL PRIMARY KEY,
  title TEXT NOT NULL CHECK (length(title) > 0),
  done BOOLEAN NOT NULL DEFAULT false,
  priority INTEGER NOT NULL DEFAULT 1 CHECK (priority >= 1 AND priority <= 5)
);
