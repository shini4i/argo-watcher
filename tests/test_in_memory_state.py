import pytest

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

    try:
        assert state.get_task_status(task_id=task['id']) == "in progress"
        state.timer.cancel()
    except AssertionError:
        state.timer.cancel()
        pytest.fail('The correct task status should be returned. Expected "in progress"')


def test_task_expiration():
    state = InMemoryState(retry_interval=1)

    task = task_template
    task['id'] = str(uuid1())

    state.set_current_task(task=Task(**task), status="in progress")

    try:
        sleep(5)
        assert state.get_task_status(task_id=task['id']) == "task not found"
        state.timer.cancel()
    except AssertionError:
        state.timer.cancel()
        pytest.fail('The correct task status should be returned. Expected "task not found".')
