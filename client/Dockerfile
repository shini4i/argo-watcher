FROM python:3.10.3-slim-buster

WORKDIR /app

RUN adduser --uid 1000 --home /app --disabled-password --gecos "" app \
 && pip install -r requirements.txt

COPY --chown=app:app . .

CMD ['/app/client.py']
