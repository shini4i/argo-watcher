from os import getenv


class Settings:
    class Argo:
        url = getenv("ARGO_URL")
        user = getenv("ARGO_USER")
        password = getenv("ARGO_PASSWORD")
