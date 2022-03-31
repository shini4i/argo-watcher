from uuid import uuid1
from time import sleep, time

from watcher.state import DBState
from watcher.models import Task
from test_in_memory_state import task_template, default_task_status, generate_task

state = DBState()


def truncate_table():
    cursor = state.db.cursor()
    cursor.execute("TRUNCATE TABLE public.tasks")
    state.db.commit()


def test_get_task_status():
    truncate_table()

    task = task_template.copy()
    task['id'] = str(uuid1())

    state.set_current_task(task=Task(**task), status=default_task_status)

    assert state.get_task_status(task_id=task['id']) == default_task_status


def test_set_current_task():
    task = task_template.copy()
    task['id'] = str(uuid1())

    state.set_current_task(task=Task(**task), status=default_task_status)
    state.update_task(task_id=task['id'], status="deployed")

    assert state.get_task_status(task_id=task['id']) == "deployed"


def test_task_filter():
    for task in [generate_task(i) for i in range(2)]:
        state.set_current_task(task=task, status=default_task_status)

    assert len(state.get_state(time_range=5, app_name="example1")) == 1
    assert state.get_state(time_range=5, app_name="example1")[0].app == "example1"


def test_get_app_list():
    task = task_template.copy()
    task['app'] = 'example1'
    task['id'] = str(uuid1())

    # Add a task for already existing app
    state.set_current_task(task=Task(**task), status=default_task_status)

    assert len(state.get_app_list()) == 3
