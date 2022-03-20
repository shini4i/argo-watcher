import logging

from os import getenv


class Settings:
    class Argo:
        url = getenv("ARGO_URL")
        user = getenv("ARGO_USER")
        password = getenv("ARGO_PASSWORD")
        timeout = getenv("ARGO_TIMEOUT", 300)
        if not isinstance(timeout, int):
            timeout = int(timeout)

    class Watcher:
        task_ttl = getenv("TASK_TTL", 60)
        if not isinstance(task_ttl, int):
            timeout = int(task_ttl)

    class Logs:
        log_level = logging.getLevelName(getenv("LOG_LEVEL", "DEBUG"))
        json_logs = True if getenv("JSON_LOGS", "0") == "1" else False
