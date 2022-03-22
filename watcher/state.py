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
            host=Settings.DB.host,
            database=Settings.DB.db_name,
            user=Settings.DB.db_user,
            password=Settings.DB.db_password
        )

    def set_current_task(self, task: Task, status: str):
        task = {
            "id": task.id,
            "created": date.fromtimestamp(time()),
            "images": json.dumps(json.loads(task.json())['images']),
            "status": status,
            "app": task.app,
            "author": task.author
        }

        cursor = self.db.cursor()
        cursor.execute(
            "INSERT INTO public.tasks(id, created, images, status, app, author) VALUES (%s, %s, %s, %s, %s, %s);",
            (task['id'], task['created'], task['images'], task['status'], task['app'], task['author']))
        self.db.commit()

    def get_task_status(self, task_id: str) -> Optional[Task]:
        query = f"select status from public.tasks where id='{task_id}'"
        cursor = self.db.cursor(cursor_factory=psycopg2.extras.DictCursor)
        cursor.execute(query=query)
        task = cursor.fetchone()
        return task['status']

    def update_task(self, task_id: str, status: str):
        cursor = self.db.cursor()
        cursor.execute(f"UPDATE public.tasks SET status='{status}' where id='{task_id}'")

    def get_state(self):
        query = f"select * from public.tasks"
        cursor = self.db.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cursor.execute(query=query)
        tasks = cursor.fetchall()
        task_list = []
        for task in tasks:
            tmp = {
                "id": task['id'],
                "created": task['created'].strftime('%s'),
                "images": task['images'],
                "status": task['status'],
                "app": task['app'],
                "author": task['author']
            }
            tmp = Task(**tmp)

            task_list.append(tmp)

        if len(task_list) > 0:
            return task_list
        else:
            return []
