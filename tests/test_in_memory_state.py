from uuid import uuid1
from time import sleep

from watcher.state import InMemoryState
from watcher.models import Task

task_template = {
    "app": "test_app",
    "author": "test_author",
    "project": "test_project",
    "images": [{"image": "test_image", "tag": "test_tag"}]
}


def test_task_status():
    state = InMemoryState()

    task = task_template
    task['id'] = str(uuid1())

    state.set_current_task(task=Task(**task), status="in progress")

    assert state.get_task_status(task_id=task['id']) == "in progress"


def test_task_expiration():
    state = InMemoryState()

    task = task_template
    task['id'] = str(uuid1())

    state.set_current_task(task=Task(**task), status="in progress")

    sleep(5)
    assert state.get_task_status(task_id=task['id']) == "task not found"
