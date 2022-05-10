import json
from abc import ABC
from abc import abstractmethod
from datetime import datetime
from datetime import timezone
from time import time

import sqlalchemy.exc
from expiringdict import ExpiringDict
from sqlalchemy import create_engine
from sqlalchemy import insert
from sqlalchemy import select
from sqlalchemy import update
from sqlalchemy.orm import Session

from watcher.config import config
from watcher.models import Task
from watcher.models import Tasks


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
    def __init__(self, history_ttl=config.get_watcher_history_ttl()):
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
            task for task in self.tasks.values() if time_range_from <= task.created
        ]

        if time_range_to is not None:
            result = [task for task in result if task.created <= time_range_to * 60]

        if app_name is not None:
            result = [task for task in result if task.app == app_name]

        return result

    def get_app_list(self) -> set:
        return set([task.app for task in self.tasks.values()])


class DBState(State):
    def __init__(self, db_host, db_name, db_user, db_password):
        self.db = create_engine(
            f"postgresql://{db_user}:{db_password}@{db_host}:5432/{db_name}"
        )
        self.session = Session(self.db)

    def set_current_task(self, task: Task, status: str):
        self.session.execute(
            insert(Tasks).values(
                id=task.id,
                created=datetime.fromtimestamp(time(), tz=timezone.utc).strftime(
                    "%Y-%m-%d %H:%M:%S"
                ),
                images=json.loads(task.json())["images"],
                status=status,
                app=task.app,
                author=task.author,
                project=task.project,
            )
        )
        self.session.commit()

    def get_task_status(self, task_id: str) -> str:
        try:
            status = self.session.execute(
                select(Tasks.status).where(Tasks.id == task_id)
            ).scalar_one()
        except sqlalchemy.exc.NoResultFound:
            return "task not found"
        return status

    def update_task(self, task_id: str, status: str):
        updated = datetime.now(tz=timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
        self.session.execute(
            update(Tasks)
            .where(Tasks.id == task_id)
            .values(status=status, updated=updated)
        )
        self.session.commit()

    def get_state(self, time_range_from: float, time_range_to: float, app_name: str):
        all_filters = [
            Tasks.created >= datetime.fromtimestamp(time_range_from, tz=timezone.utc)
        ]

        if time_range_to is not None:
            all_filters.append(
                Tasks.created <= datetime.fromtimestamp(time_range_to, tz=timezone.utc)
            )

        if app_name is not None:
            all_filters.append(Tasks.app == app_name)

        results = self.session.query(Tasks).filter(*all_filters).all()

        return [Task(**task.__dict__) for task in results]

    def get_app_list(self) -> list:
        return [app[0] for app in self.session.query(Tasks.app).distinct()]
