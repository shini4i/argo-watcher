from pydantic import BaseModel


class Image(BaseModel):
    image: str
    tag: str


class Images(BaseModel):
    app: str
    images: Image
