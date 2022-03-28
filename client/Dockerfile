FROM python:3.10.3-slim-buster

WORKDIR /app

RUN adduser --uid 1000 --home /app --disabled-password --gecos "" app

COPY --chown=app:app . .

RUN pip install -r requirements.txt

CMD ['/app/client.py']
