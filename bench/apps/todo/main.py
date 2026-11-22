from fastapi import FastAPI, HTTPException, Header, Depends, Request
from fastapi.responses import HTMLResponse
from fastapi.templating import Jinja2Templates
from pydantic import BaseModel, Field
from sqlalchemy import create_engine, Column, Integer, Text, Boolean, CheckConstraint
from sqlalchemy.orm import declarative_base, sessionmaker, Session

engine = create_engine("sqlite:///todo.db")
SessionLocal = sessionmaker(bind=engine)
Base = declarative_base()
templates = Jinja2Templates(directory="templates")
app = FastAPI()


class Todo(Base):
    __tablename__ = "todo"
    id = Column(Integer, primary_key=True)
    title = Column(Text, nullable=False)
    done = Column(Boolean, nullable=False, default=False)
    priority = Column(Integer, nullable=False, default=1)
    __table_args__ = (
        CheckConstraint("length(title) > 0"),
        CheckConstraint("priority >= 1 AND priority <= 5"),
    )


Base.metadata.create_all(engine)


class TodoIn(BaseModel):
    title: str = Field(min_length=1)
    done: bool = False
    priority: int = Field(default=1, ge=1, le=5)


class TodoOut(TodoIn):
    id: int

    class Config:
        from_attributes = True


def db():
    s = SessionLocal()
    try:
        yield s
    finally:
        s.close()


def get_or_404(s: Session, id: int) -> Todo:
    t = s.get(Todo, id)
    if t is None:
        raise HTTPException(404, "not found")
    return t


@app.get("/api/todo", response_model=list[TodoOut])
def list_todos(s: Session = Depends(db)):
    return s.query(Todo).order_by(Todo.id).all()


@app.get("/api/todo/{id}", response_model=TodoOut)
def get_todo(id: int, s: Session = Depends(db)):
    return get_or_404(s, id)


@app.post("/api/todo", response_model=TodoOut, status_code=201)
def create_todo(body: TodoIn, s: Session = Depends(db)):
    t = Todo(**body.model_dump())
    s.add(t)
    s.commit()
    s.refresh(t)
    return t


@app.put("/api/todo/{id}", response_model=TodoOut)
def update_todo(id: int, body: TodoIn, s: Session = Depends(db)):
    t = get_or_404(s, id)
    for k, v in body.model_dump().items():
        setattr(t, k, v)
    s.commit()
    return t


@app.delete("/api/todo/{id}", status_code=204)
def delete_todo(id: int, s: Session = Depends(db)):
    s.delete(get_or_404(s, id))
    s.commit()


@app.post("/api/todo/{id}/toggle", response_model=TodoOut)
def toggle(id: int, s: Session = Depends(db)):
    t = get_or_404(s, id)
    t.done = not t.done
    s.commit()
    return t


@app.post("/api/todo/{id}/bump", response_model=TodoOut)
def bump(id: int, s: Session = Depends(db)):
    t = get_or_404(s, id)
    if not t.priority < 5:
        raise HTTPException(400, "precondition failed: priority < 5")
    t.priority += 1
    s.commit()
    return t


@app.post("/api/clear_done", status_code=204)
def clear_done(x_role: str = Header(default=""), s: Session = Depends(db)):
    if x_role != "admin":
        raise HTTPException(403, "forbidden")
    s.query(Todo).filter(Todo.done.is_(True)).delete()
    s.commit()


def progress(s: Session) -> str:
    n = s.query(Todo).count()
    if n == 0:
        return "还没有任务"
    d = s.query(Todo).filter(Todo.done.is_(True)).count()
    return f"{d} / {n} 已完成"


@app.get("/", response_class=HTMLResponse)
def home(request: Request, s: Session = Depends(db)):
    todos = s.query(Todo).order_by(Todo.id).all()
    return templates.TemplateResponse("home.html", {"request": request, "todos": todos, "progress": progress(s)})


@app.get("/table", response_class=HTMLResponse)
def table(request: Request, s: Session = Depends(db)):
    todos = s.query(Todo).order_by(Todo.id).all()
    return templates.TemplateResponse("table.html", {"request": request, "todos": todos})
