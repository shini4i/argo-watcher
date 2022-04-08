from typing import List
from typing import Optional

from pydantic import BaseModel


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
