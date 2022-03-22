import logging
import psycopg2
import psycopg2.extras
import json

from abc import ABC, abstractmethod
from typing import Optional
from time import time
from threading import Timer
from datetime import date

from watcher.models import Task
from watcher.settings import Settings


class State(ABC):

    @abstractmethod
    def set_current_task(self, task: Task, status: str): ...

    @abstractmethod
    def get_task_status(self, task_id: str) -> Optional[Task]: ...

    @abstractmethod
    def update_task(self, task_id: str, status: str): ...

    @abstractmethod
    def get_state(self): ...


class InMemoryState(State):
    def __init__(self):
        self.tasks = dict()
        self.expire_tasks()

    def set_current_task(self, task: Task, status: str):
        task.status = status
        task.created = time()
        self.tasks[task.id] = task

    def get_task_status(self, task_id: str) -> Optional[Task]:
        return self.tasks.get(task_id).status

    def update_task(self, task_id: str, status: str):
        self.tasks[task_id].status = status

    def expire_tasks(self):
        Timer(5.0, self.expire_tasks).start()
        current_time = time()
        if len(self.tasks) > 0:
            for task in self.tasks.copy().values():
                if int(current_time - task.created) > Settings.Watcher.history_ttl:
                    logging.debug(f"Expiring {task.id} task...")
                    self.tasks.pop(task.id)

    def get_state(self):
        return [task for task in self.tasks.values()]


class DBState(State):
    def __init__(self):
        self.db = psycopg2.connect(
            host="127.0.0.1",
            database="argo",
            user="postgres",
            password="example"
        )

    def set_current_task(self, task: Task, status: str):
        task.status = status
        task.created = time()

        cursor = self.db.cursor()
        cursor.execute(
            "INSERT INTO public.tasks(id, created, images, status, app, author) VALUES (%s, %s, %s, %s, %s, %s);",
            (task.id, date.fromtimestamp(task.created), json.dumps(json.loads(task.json())['images']), task.status,
             task.app, task.author))
        self.db.commit()

    def get_task_status(self, task_id: str) -> Optional[Task]:
        query = f"select id, created, images, status, app, author from public.tasks where id = '{task_id}'"
        cursor = self.db.cursor(cursor_factory=psycopg2.extras.DictCursor)
        cursor.execute(query=query)
        task = cursor.fetchone()
        return task['status']

    def update_task(self, task_id: str, status: str):
        cursor = self.db.cursor()
        cursor.execute(f"UPDATE public.tasks SET status = '{status}' where id = '{task_id}'")

    def get_state(self):
        pass
