from pydantic import BaseModel
from typing import List, Optional


class Image(BaseModel):
    image: str
    tag: str


class Task(BaseModel):
    id: Optional[str]
    created: Optional[int]
    app: str
    author: str
    project: str
    images: List[Image]
    status: Optional[str]
