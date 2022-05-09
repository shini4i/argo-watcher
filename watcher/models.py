from typing import List
from typing import Optional

from pydantic import BaseModel
from sqlalchemy import JSON
from sqlalchemy import TIMESTAMP
from sqlalchemy import VARCHAR
from sqlalchemy import Column
from sqlalchemy.orm import declarative_base

Base = declarative_base()


class Image(BaseModel):
    image: str
    tag: str


class Task(BaseModel):
    id: Optional[str]
    created: Optional[int]
    updated: Optional[int]
    app: str
    author: str
    project: str
    images: List[Image]
    status: Optional[str]


class Tasks(Base):
    __tablename__ = "tasks"

    id = Column(VARCHAR(36), primary_key=True)
    created = Column(TIMESTAMP)
    updated = Column(TIMESTAMP)
    images = Column(JSON)
    status = Column(VARCHAR(255))
    app = Column(VARCHAR(255))
    author = Column(VARCHAR(255))
    project = Column(VARCHAR(255))
