from datetime import datetime
from datetime import timezone
from typing import List
from typing import Optional

from pydantic import BaseModel
from pydantic import validator


class Image(BaseModel):
    image: str
    tag: str


class Task(BaseModel):
    id: Optional[str]
    created: Optional[float]
    updated: Optional[float]
    app: str
    author: str
    project: str
    images: List[Image]
    status: Optional[str]

    @validator("created", "updated", pre=True)
    def convert_datetime_to_float(cls, timestamp):
        if type(timestamp) is datetime:
            return float(timestamp.replace(tzinfo=timezone.utc).timestamp())
        else:
            return timestamp
