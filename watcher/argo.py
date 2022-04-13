import json
import logging
from time import time
from typing import Optional

import requests
from prometheus_client import Gauge
from requests.exceptions import RequestException
from requests.packages.urllib3.exceptions import InsecureRequestWarning
from tenacity import RetryError
from tenacity import retry
from tenacity import retry_if_exception_type
from tenacity import stop_after_delay
from tenacity import wait_fixed

from watcher.models import Task
from watcher.settings import Settings
from watcher.state import DBState
from watcher.state import InMemoryState

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
        self.session.verify = Settings.Watcher.ssl_verify
        if not self.session.verify:
            # Reducing noise in logs as we assume that the user knows
            # the risk of disabling SSL verification
            requests.packages.urllib3.disable_warnings(InsecureRequestWarning)

        # Exposes per-app Prometheus metric in the following way
        # failed_deployment{app_name="test"} 0.0
        self.failed_deployment_gauge = Gauge(
            "failed_deployment", "Failed deployment", ["app_name"]
        )

        self.argo_url = Settings.Argo.url
        self.argo_user = Settings.Argo.user
        self.argo_password = Settings.Argo.password
        self.authorized = self.auth()

    def auth(self) -> bool:
        url = f"{self.argo_url}/api/v1/session"
        payload = {"username": self.argo_user, "password": self.argo_password}

        try:
            response = self.session.post(url=url, json=payload)
        except RequestException as exception:
            logging.error(exception)
            return False

        match response.status_code:
            case 401:
                logging.critical("Unauthorized, please check ArgoCD credentials!")
                exit(1)
            case 403:
                logging.critical("Forbidden, please check the firewall!")
                exit(1)

    def check_argo(self):
        try:
            response = self.session.get(url=f"{self.argo_url}/api/v1/session/userinfo")
            if response.json()["loggedIn"]:
                return "up"
        except KeyError:
            return "down"

    def start_task(self, task: Task):
        logging.info(
            f"New task with id {task.id} was triggered. "
            f"Expecting tag {task.images[0].tag} in {task.app} app."
        )
        try:
            state.set_current_task(task=task, status="in progress")
            self.wait_for_rollout(task=task)
            self.failed_deployment_gauge.labels(task.app).set(0)
            state.update_task(task_id=task.id, status="deployed")
            logging.info(
                f"Task {task.id} has succeeded. "
                f"App {task.app} is running on the expected version."
            )
        except RetryError:
            logging.warning(
                f"Task {task.id} has failed. "
                f"App {task.app} did not become healthy within reasonable timeframe."
            )
            state.update_task(task_id=task.id, status="failed")
            self.failed_deployment_gauge.labels(task.app).inc()
        except AppDoesNotExistException:
            logging.warning(
                f"Task {task.id} has failed. " f"App {task.app} does not exists."
            )
            state.update_task(task_id=task.id, status="app not found")

    @staticmethod
    def get_task_status(task_id: str):
        return state.get_task_status(task_id=task_id)

    @staticmethod
    def return_state(from_timestamp: float, to_timestamp: float, app_name: str):
        # Set "to_timestamp" to the current timestamp
        # to return all tasks starting from "from_timestamp"
        if to_timestamp is None:
            to_timestamp = time()

        return state.get_state(
            time_range_from=(time() - from_timestamp) / 60,
            time_range_to=to_timestamp / 60,
            app_name=app_name,
        )

    @staticmethod
    def return_app_list():
        return state.get_app_list()

    def refresh_app(self, app: str) -> int:
        # Trigger app refresh to increase new version detection speed
        url = f"{self.argo_url}/api/v1/applications/{app}?refresh=normal"
        return self.session.get(url=url).status_code

    def get_app_status(self, app: str) -> Optional[dict]:
        r = self.session.get(url=f"{self.argo_url}/api/v1/applications/{app}")
        if r.status_code != 200:
            return None
        else:
            app = json.loads(r.content.decode("utf-8"))

        status = {
            "images": app["status"]["summary"]["images"],
            "synced": app["status"]["sync"]["status"],
            "healthy": app["status"]["health"]["status"],
        }

        return status

    @retry(
        stop=stop_after_delay(Settings.Argo.timeout),
        retry=retry_if_exception_type((AppNotReadyException, InvalidImageException)),
        wait=wait_fixed(5),
    )
    def wait_for_rollout(self, task: Task):

        if self.refresh_app(app=task.app) == 404:
            raise AppDoesNotExistException

        app_status = self.get_app_status(task.app)

        for target in task.images:
            if f"{target.image}:{target.tag}" not in app_status["images"]:
                logging.debug(f"{target.image}:{target.tag} is not available yet...")
                raise InvalidImageException

        if app_status["synced"] == "Synced" and app_status["healthy"] == "Healthy":
            return
        else:
            raise AppNotReadyException
