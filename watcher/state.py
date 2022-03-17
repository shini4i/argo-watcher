import logging
from typing import Optional

from watcher.models import Task


class State:
    def __init__(self):
        self.state = dict()

    def set_current_task(self, task_id: str, task: Task, status: str):
        self.state[task_id] = {
            "app": task.app,
            "author": task.author,
            "tags": set([image.tag for image in task.images]),
            "status": status
        }
        print(self.state)

    def get_task_status(self, task_id: str) -> Optional[dict]:
        return self.state.get(task_id)['status']

    def update_task(self, task_id: str, status: str):
        self.state[task_id].update({"status": status})

    def get_state(self):
        return self.state
