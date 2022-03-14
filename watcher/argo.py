import requests
import logging
import json

from watcher.settings import Settings


class Argo:
    def __init__(self):
        self.session = requests.session()
        self.authorized = self.auth()

    def auth(self) -> bool:
        status_code = self.session.post(url=f"{Settings.Argo.url}/api/v1/session",
                                        json={
                                            "username": Settings.Argo.user,
                                            "password": Settings.Argo.password
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

    def get_app_status(self, app: str):
        r = self.session.get(url=f"{Settings.Argo.url}/api/v1/applications/{app}")
        if r.status_code != 200:
            return None
        else:
            app = json.loads(r.content.decode('utf-8'))
        # logging.info(app['status']['summary'])
        return {'synced': app['status']['sync']['status'], 'healthy': app['status']['health']['status']}
