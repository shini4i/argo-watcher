import pytest

from uuid import uuid1
from time import sleep

from watcher.state import InMemoryState
from watcher.models import Task


def test_task_status():
    state = InMemoryState()

    task_id = str(uuid1())

    task = {
        "id": task_id,
        "app": "test_app",
        "author": "test_author",
        "project": "test_project",
        "images": [{"image": "test_image", "tag": "test_tag"}]
    }

    state.set_current_task(task=Task(**task), status="in progress")

    try:
        assert state.get_task_status(task_id=task_id) == "in progress"
        state.timer.cancel()
    except AssertionError:
        state.timer.cancel()
        pytest.fail('The correct task status should be returned. Expected "in progress"')


def test_task_expiration():
    state = InMemoryState(retry_interval=1)

    task_id = str(uuid1())

    task = {
        "id": task_id,
        "app": "test_app",
        "author": "test_author",
        "project": "test_project",
        "images": [{"image": "test_image", "tag": "test_tag"}]
    }

    state.set_current_task(task=Task(**task), status="in progress")
    sleep(5)

    try:
        assert state.get_task_status(task_id=task_id) == "task not found"
        state.timer.cancel()
    except AssertionError:
        state.timer.cancel()
        pytest.fail('The correct task status should be returned. Expected "task not found".')
