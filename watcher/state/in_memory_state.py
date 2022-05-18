from time import time

from expiringdict import ExpiringDict

from watcher.config import config
from watcher.models import Task
from watcher.state.state import State


class InMemoryState(State):
    def __init__(self, history_ttl=config.get_watcher_history_ttl()):
        self.tasks = ExpiringDict(max_len=100, max_age_seconds=history_ttl)

    def set_current_task(self, task: Task, status: str):
        task.status = status
        task.created = time()
        self.tasks[task.id] = task

    def get_task_status(self, task_id: str) -> str:
        try:
            return self.tasks.get(task_id).status
        except AttributeError:
            return "task not found"

    def update_task(self, task_id: str, status: str):
        self.tasks[task_id].status = status
        self.tasks[task_id].updated = time()

    def get_state(
        self, time_range_from: float, time_range_to: float, app_name: str
    ) -> list:
        result = [
            task for task in self.tasks.values() if time_range_from <= task.created
        ]

        if time_range_to is not None:
            result = [task for task in result if task.created <= time_range_to]

        if app_name is not None:
            result = [task for task in result if task.app == app_name]

        return result

    def get_app_list(self) -> set:
        return {task.app for task in self.tasks.values()}
