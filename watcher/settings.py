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
        history_ttl = getenv("HISTORY_TTL", 3600)
        if not isinstance(history_ttl, int):
            history_ttl = int(history_ttl)

    class Logs:
        log_level = logging.getLevelName(getenv("LOG_LEVEL", "DEBUG"))
        json_logs = True if getenv("JSON_LOGS", "0") == "1" else False
