import json
from abc import ABC
from abc import abstractmethod
from datetime import datetime
from datetime import timezone
from time import time

import psycopg2
import psycopg2.extras
from expiringdict import ExpiringDict

from watcher.models import Task
from watcher.settings import Settings


class State(ABC):
    @abstractmethod
    def set_current_task(self, task: Task, status: str):
        ...

    @abstractmethod
    def get_task_status(self, task_id: str) -> str:
        ...

    @abstractmethod
    def update_task(self, task_id: str, status: str):
        ...

    @abstractmethod
    def get_state(self, time_range_from: float, time_range_to: float, app_name: str):
        ...

    @abstractmethod
    def get_app_list(self) -> set:
        ...


class InMemoryState(State):
    def __init__(self, history_ttl=Settings.Watcher.history_ttl):
        self.tasks = ExpiringDict(max_len=100, max_age_seconds=history_ttl)

    def set_current_task(self, task: Task, status: str):
        task.status = status
        task.created = time()
        self.tasks[task.id] = task

    def get_task_status(self, task_id: str) -> str:
        try:
            return self.tasks.get(task_id).status
        except AttributeError:
            return "task not found"

    def update_task(self, task_id: str, status: str):
        self.tasks[task_id].status = status
        self.tasks[task_id].updated = time()

    def get_state(self, time_range_from: float, time_range_to: float, app_name: str):
        result = [
            task
            for task in self.tasks.values()
            if time() - time_range_from * 60 <= task.created <= time_range_to * 60
        ]

        if app_name is not None:
            result = [task for task in result if task.app == app_name]

        return result

    def get_app_list(self) -> set:
        return set([task.app for task in self.tasks.values()])


class DBState(State):
    def __init__(self):
        self.db = psycopg2.connect(
            host=Settings.DB.host,
            database=Settings.DB.db_name,
            user=Settings.DB.db_user,
            password=Settings.DB.db_password,
        )

    def set_current_task(self, task: Task, status: str):
        task = {
            "id": task.id,
            "created": datetime.fromtimestamp(time(), tz=timezone.utc).strftime(
                "%Y-%m-%d %H:%M:%S"
            ),
            "images": json.dumps(json.loads(task.json())["images"]),
            "status": status,
            "app": task.app,
            "author": task.author,
            "project": task.project,
        }

        cursor = self.db.cursor()
        cursor.execute(
            "INSERT INTO public.tasks(id, created, images, status, app, author, project) "
            "VALUES (%s, %s, %s, %s, %s, %s, %s);",
            (
                task["id"],
                task["created"],
                task["images"],
                task["status"],
                task["app"],
                task["author"],
                task["project"],
            ),
        )
        self.db.commit()

    def get_task_status(self, task_id: str) -> str:
        query = f"select status from public.tasks where id='{task_id}'"
        cursor = self.db.cursor(cursor_factory=psycopg2.extras.DictCursor)
        cursor.execute(query=query)
        task = cursor.fetchone()

        return task["status"]

    def update_task(self, task_id: str, status: str):
        updated = datetime.now(tz=timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
        cursor = self.db.cursor()
        query = (
            "UPDATE public.tasks "
            f"SET status='{status}', updated='{updated}' "
            f"where id='{task_id}'"
        )
        cursor.execute(query)
        self.db.commit()

    def get_state(self, time_range_from: float, time_range_to: float, app_name: str):
        query = (
            "select id, extract(epoch from created) AS created, "
            "extract(epoch from updated) AS updated, "
            "images, status, app, author, project from public.tasks "
            f"where created >= '{datetime.fromtimestamp(time_range_from)}'"
        )

        if time_range_to is not None:
            query = f"{query} AND created <= '{datetime.fromtimestamp(time_range_to)};"

        if app_name is not None:
            query = f"{query} and app = '{app_name}'"

        cursor = self.db.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cursor.execute(query=query)
        tasks = cursor.fetchall()

        return [Task(**task) for task in tasks]

    def get_app_list(self) -> set:
        query = "select distinct app from public.tasks"
        cursor = self.db.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        cursor.execute(query=query)
        apps = cursor.fetchall()

        return set([res["app"] for res in apps])
