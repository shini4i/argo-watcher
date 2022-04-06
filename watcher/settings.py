import logging

from os import getenv, environ


class Settings:
    class Argo:
        url = environ['ARGO_URL']
        user = environ['ARGO_USER']
        password = environ['ARGO_PASSWORD']
        timeout = getenv('ARGO_TIMEOUT', 300)
        if not isinstance(timeout, int):
            timeout = int(timeout)

    class Watcher:
        state_type = getenv("STATE_TYPE", "in-memory")
        ssl_verify = getenv("SSL_VERIFY", True)
        if not isinstance(ssl_verify, bool):
            ssl_verify = bool(ssl_verify)
        history_ttl = getenv('HISTORY_TTL', 3600)
        if not isinstance(history_ttl, int):
            history_ttl = int(history_ttl)

    class DB:
        host = getenv("DB_HOST")
        db_name = getenv("DB_NAME")
        db_user = getenv("DB_USER")
        db_password = getenv("DB_PASSWORD")

    class Logs:
        log_level = logging.getLevelName(getenv('LOG_LEVEL', 'INFO'))
