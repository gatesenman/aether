from fastapi import FastAPI, HTTPException, Depends, Request
from fastapi.responses import HTMLResponse
from fastapi.templating import Jinja2Templates
from pydantic import BaseModel
from sqlalchemy import create_engine, Column, Integer, Text, Boolean
from sqlalchemy.orm import declarative_base, sessionmaker, Session

engine = create_engine("sqlite:///notes.db")
SessionLocal = sessionmaker(bind=engine)
Base = declarative_base()
templates = Jinja2Templates(directory="templates")
app = FastAPI()


class Note(Base):
    __tablename__ = "note"
    id = Column(Integer, primary_key=True)
    title = Column(Text, nullable=False)
    body = Column(Text, nullable=False)
    pinned = Column(Boolean, nullable=False, default=False)


Base.metadata.create_all(engine)


class NoteIn(BaseModel):
    title: str
    body: str
    pinned: bool = False


class NoteOut(NoteIn):
    id: int

    class Config:
        from_attributes = True


def db():
    s = SessionLocal()
    try:
        yield s
    finally:
        s.close()


@app.get("/api/note", response_model=list[NoteOut])
def list_notes(s: Session = Depends(db)):
    return s.query(Note).order_by(Note.id).all()


@app.get("/api/note/{id}", response_model=NoteOut)
def get_note(id: int, s: Session = Depends(db)):
    n = s.get(Note, id)
    if n is None:
        raise HTTPException(404)
    return n


@app.post("/api/note", response_model=NoteOut, status_code=201)
def create_note(body: NoteIn, s: Session = Depends(db)):
    n = Note(**body.model_dump())
    s.add(n)
    s.commit()
    s.refresh(n)
    return n


@app.put("/api/note/{id}", response_model=NoteOut)
def update_note(id: int, body: NoteIn, s: Session = Depends(db)):
    n = s.get(Note, id)
    if n is None:
        raise HTTPException(404)
    for k, v in body.model_dump().items():
        setattr(n, k, v)
    s.commit()
    return n


@app.delete("/api/note/{id}", status_code=204)
def delete_note(id: int, s: Session = Depends(db)):
    s.query(Note).filter(Note.id == id).delete()
    s.commit()


@app.get("/", response_class=HTMLResponse)
def home(request: Request, s: Session = Depends(db)):
    return templates.TemplateResponse("home.html", {"request": request, "notes": s.query(Note).all()})
