import logging

from environs import Env

env = Env()


class Config:
    class Argo:
        url = env.str("ARGO_URL")
        user = env.str("ARGO_USER")
        password = env.str("ARGO_PASSWORD")
        timeout = env.int("ARGO_TIMEOUT", 300)

    class Watcher:
        state_type = env.str("STATE_TYPE", "in-memory")
        ssl_verify = env.bool("SSL_VERIFY", True)
        history_ttl = env.int("HISTORY_TTL", 3600)

    class DB:
        host = env.str("DB_HOST")
        db_name = env.str("DB_NAME")
        db_user = env.str("DB_USER")
        db_password = env.str("DB_PASSWORD")

    class Logs:
        log_level = env.log_level("LOG_LEVEL", logging.INFO)
