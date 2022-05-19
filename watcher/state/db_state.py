import json
from datetime import datetime
from datetime import timezone
from time import time
from typing import List

import sqlalchemy.exc
from sqlalchemy import create_engine
from sqlalchemy import insert
from sqlalchemy import select
from sqlalchemy import update
from sqlalchemy.orm import Session

from watcher.models import Task
from watcher.state.base import State
from watcher.state.base import Tasks


class DBState(State):
    def __init__(self, db_host, db_name, db_user, db_password, db_port):
        self.db = create_engine(
            f"postgresql://{db_user}:{db_password}@{db_host}:{db_port}/{db_name}",
            pool_pre_ping=True,
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

    def get_state(
        self, time_range_from: float, time_range_to: float, app_name: str
    ) -> List[Task]:
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

    def get_app_list(self) -> set:
        return {app[0] for app in self.session.query(Tasks.app).distinct()}
