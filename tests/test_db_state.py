from time import time
from uuid import uuid1

from environs import Env
from test_in_memory_state import default_task_status
from test_in_memory_state import generate_task
from test_in_memory_state import task_template

from watcher.models import Task
from watcher.state.db_state import DBState

env = Env()

state = DBState(
    db_host=env.str("DB_HOST"),
    db_port=env.int("DB_PORT", 5432),
    db_name=env.str("DB_NAME"),
    db_user=env.str("DB_USER"),
    db_password=env.str("DB_PASSWORD"),
)


def truncate_table():
    state.session.execute("""TRUNCATE TABLE public.tasks""")


def test_get_task_status():
    truncate_table()

    task = task_template.copy()
    task["id"] = str(uuid1())

    state.set_current_task(task=Task(**task), status=default_task_status)

    assert state.get_task_status(task_id=task["id"]) == default_task_status


def test_set_current_task():
    task = task_template.copy()
    task["id"] = str(uuid1())

    state.set_current_task(task=Task(**task), status=default_task_status)
    state.update_task(task_id=task["id"], status="deployed")

    assert state.get_task_status(task_id=task["id"]) == "deployed"


def test_task_filter():
    for task in [generate_task(i) for i in range(2)]:
        state.set_current_task(task=task, status=default_task_status)

    assert (
        len(
            state.get_state(
                time_range_from=time() - 5, time_range_to=time(), app_name="example1"
            )
        )
        == 1
    )
    assert (
        state.get_state(
            time_range_from=time() - 5, time_range_to=time(), app_name="example1"
        )[0].app
        == "example1"
    )


def test_get_app_list():
    task = task_template.copy()
    task["app"] = "example1"
    task["id"] = str(uuid1())

    # Add a task for already existing app
    state.set_current_task(task=Task(**task), status=default_task_status)

    assert len(state.get_app_list()) == 3
