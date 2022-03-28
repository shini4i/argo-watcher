from uuid import uuid1
from time import sleep, time

from watcher.state import InMemoryState
from watcher.models import Task

task_template = {
    "app": "test_app",
    "author": "test_author",
    "project": "test_project",
    "images": [{"image": "example", "tag": "latest"}]
}

default_task_status = 'in progress'


def test_task_status():
    state = InMemoryState()

    task = task_template
    task['id'] = str(uuid1())

    state.set_current_task(task=Task(**task), status=default_task_status)

    assert state.get_task_status(task_id=task['id']) == default_task_status


def test_task_expiration():
    state = InMemoryState(history_ttl=1)

    task = task_template
    task['id'] = str(uuid1())

    state.set_current_task(task=Task(**task), status=default_task_status)
    sleep(1)

    assert state.get_task_status(task_id=task['id']) == "task not found"


def test_task_filter():
    state = InMemoryState()

    task1 = task_template
    task2 = task_template.copy()
    task2['app'] = "example"

    state.set_current_task(task=Task(**task1), status=default_task_status)
    state.set_current_task(task=Task(**task2), status=default_task_status)

    assert len(state.get_state(time_range=int(time()-60), app_name="example")) == 1
    assert state.get_state(time_range=int(time()-60), app_name="example")[0].app == "example"
