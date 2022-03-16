import requests
import logging
import json

from tenacity import retry, stop_after_delay, retry_if_exception_type, wait_fixed
from typing import Optional

from watcher.settings import Settings
from watcher.models import Images


class InvalidImageException(Exception):
    pass


class AppNotReadyException(Exception):
    pass


class Argo:
    def __init__(self):
        self.session = requests.session()
        self.argo_url = Settings.Argo.url
        self.argo_user = Settings.Argo.user
        self.argo_password = Settings.Argo.password
        self.authorized = self.auth()

    def auth(self) -> bool:
        status_code = self.session.post(url=f"{self.argo_url}/api/v1/session",
                                        json={
                                            "username": self.argo_user,
                                            "password": self.argo_password
                                        }).status_code

        match status_code:
            case 200:
                return True
            case 401:
                logging.error("Unauthorized, please check ArgoCD credentials!")
                return False
            case 403:
                logging.error("Forbidden, please check the firewall!")
                return False

    def refresh_app(self, app: str) -> bool:
        if self.session.get(url=f"{self.argo_url}/api/v1/applications/{app}?refresh=normal").status_code == 200:
            return True
        else:
            return False

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
    def wait_for_rollout(self, payload: Images):

        self.refresh_app(app=payload.app)

        app_status = self.get_app_status(payload.app)
        for target in payload.images:
            if f"{target.image}:{target.tag}" not in app_status['images']:
                logging.debug(f"{target.image}:{target.tag} is not available yet...")
                raise InvalidImageException

        if app_status['synced'] == 'Synced' and app_status['healthy'] == "Healthy":
            return True
        else:
            raise AppNotReadyException
