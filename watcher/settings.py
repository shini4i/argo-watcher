import logging

from os import getenv


class Settings:
    class Argo:
        url = getenv("ARGO_URL")
        user = getenv("ARGO_USER")
        password = getenv("ARGO_PASSWORD")
        timeout = 300

    class Logs:
        log_level = logging.getLevelName(getenv("LOG_LEVEL", "DEBUG"))
        json_logs = True if getenv("JSON_LOGS", "0") == "1" else False
