from pydantic import BaseModel
from typing import List


class Image(BaseModel):
    image: str
    tag: str


class Task(BaseModel):
    app: str
    author: str
    images: List[Image]
