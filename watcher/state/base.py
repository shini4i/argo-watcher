from abc import ABC
from abc import abstractmethod
from typing import List

from sqlalchemy import JSON
from sqlalchemy import TIMESTAMP
from sqlalchemy import VARCHAR
from sqlalchemy import Column
from sqlalchemy.orm import declarative_base

from watcher.models import Task

Base = declarative_base()


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


class State(ABC):
    @abstractmethod
    def set_current_task(self, task: Task, status: str):
        ...

    @abstractmethod
    def get_task_status(self, task_id: str) -> str:
        ...

    @abstractmethod
    def update_task(self, task_id: str, status: str):
        ...

    @abstractmethod
    def get_state(
        self, time_range_from: float, time_range_to: float, app_name: str
    ) -> List[Task]:
        ...

    @abstractmethod
    def get_app_list(self) -> set:
        ...
