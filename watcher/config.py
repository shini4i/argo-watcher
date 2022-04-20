import logging
from os import environ
from os import getenv


class Config:
    class Argo:
        url = environ['ARGO_URL']
        user = environ['ARGO_USER']
        password = environ['ARGO_PASSWORD']
        timeout = int(getenv('ARGO_TIMEOUT', 300))

    class Watcher:
        state_type = getenv("STATE_TYPE", "in-memory")
        ssl_verify = getenv("SSL_VERIFY", "True")
        if ssl_verify.upper() == "FALSE":
            ssl_verify = False
        else:
            ssl_verify = True

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
