from pydantic import BaseModel
from typing import List


class Image(BaseModel):
    image: str
    tag: str


class Images(BaseModel):
    app: str
    images: List[Image]
