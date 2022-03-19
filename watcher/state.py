from typing import Optional

from watcher.models import Task


class State:
    def __init__(self):
        self.tasks = dict()

    def set_current_task(self, task: Task, status: str):
        task.status = status
        self.tasks[task.id] = task

    def get_task_status(self, task_id: str) -> Optional[Task]:
        return self.tasks.get(task_id).status

    def update_task(self, task_id: str, status: str):
        self.tasks[task_id].status = status

    def get_state(self):
        return [task for task in self.tasks.values()]
