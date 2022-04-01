import requests
import logging
import json

from time import time
from tenacity import retry, stop_after_delay, retry_if_exception_type, wait_fixed, RetryError
from typing import Optional
from requests.exceptions import RequestException

from prometheus_client import Gauge

from watcher.settings import Settings
from watcher.models import Task
from watcher.state import InMemoryState, DBState

match Settings.Watcher.state_type:
    case "in-memory":
        state = InMemoryState()
    case "postgres":
        state = DBState()
    case _:
        logging.error("Invalid STATE_TYPE was provided")
        exit(1)


class InvalidImageException(Exception):
    pass


class AppNotReadyException(Exception):
    pass


class AppDoesNotExistException(Exception):
    pass


class Argo:
    def __init__(self):
        self.session = requests.Session()
        self.gauge = Gauge('failed_deployment',
                           'Failed deployment', ['app_name'])
        self.argo_url = Settings.Argo.url
        self.argo_user = Settings.Argo.user
        self.argo_password = Settings.Argo.password
        self.authorized = self.auth()

    def auth(self) -> bool:
        try:
            response = self.session.post(url=f"{self.argo_url}/api/v1/session",
                                         json={
                                             "username": self.argo_user,
                                             "password": self.argo_password
                                         })
        except RequestException as exception:
            logging.error(exception)
            return False

        match response.status_code:
            case 200:
                return True
            case 401:
                logging.error("Unauthorized, please check ArgoCD credentials!")
                return False
            case 403:
                logging.error("Forbidden, please check the firewall!")
                return False

    def check_argo(self):
        try:
            response = self.session.get(url=f"{self.argo_url}/api/v1/session/userinfo")
            if response.json()['loggedIn']:
                return "up"
        except KeyError:
            return "down"

    def start_task(self, task: Task):
        try:
            state.set_current_task(task=task, status="in progress")
            self.wait_for_rollout(task=task)
            self.gauge.labels(task.app).set(0)
            state.update_task(task_id=task.id, status="deployed")
        except RetryError:
            state.update_task(task_id=task.id, status="failed")
            self.gauge.labels(task.app).inc()
        except AppDoesNotExistException:
            state.update_task(task_id=task.id, status="app not found")

    @staticmethod
    def get_task_status(task_id: str):
        return state.get_task_status(task_id=task_id)

    @staticmethod
    def return_state(from_timestamp: float, app_name: str):
        return state.get_state(time_range=int((time() - from_timestamp) / 60), app_name=app_name)

    @staticmethod
    def return_app_list():
        return state.get_app_list()

    def refresh_app(self, app: str) -> int:
        return self.session.get(url=f"{self.argo_url}/api/v1/applications/{app}?refresh=normal").status_code

    def get_app_status(self, app: str) -> Optional[dict]:
        r = self.session.get(url=f"{self.argo_url}/api/v1/applications/{app}")
        if r.status_code != 200:
            return None
        else:
            app = json.loads(r.content.decode('utf-8'))

        status = {
            "images": app['status']['summary']['images'],
            'synced': app['status']['sync']['status'],
            'healthy': app['status']['health']['status']
        }

        return status

    @retry(stop=stop_after_delay(Settings.Argo.timeout),
           retry=retry_if_exception_type((AppNotReadyException, InvalidImageException)),
           wait=wait_fixed(5))
    def wait_for_rollout(self, task: Task):

        if self.refresh_app(app=task.app) == 404:
            raise AppDoesNotExistException

        app_status = self.get_app_status(task.app)

        for target in task.images:
            if f"{target.image}:{target.tag}" not in app_status['images']:
                logging.debug(f"{target.image}:{target.tag} is not available yet...")
                raise InvalidImageException

        if app_status['synced'] == 'Synced' and app_status['healthy'] == "Healthy":
            return
        else:
            raise AppNotReadyException
