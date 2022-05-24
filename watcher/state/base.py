from abc import ABC
from abc import abstractmethod
from typing import List

from watcher.models import Task


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

    @staticmethod
    @abstractmethod
    def get_state_type() -> str:
        ...
