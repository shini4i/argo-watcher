FROM python:3.10.2-alpine3.15

WORKDIR /src

COPY cli.py .
COPY requirements.txt .

RUN pip install -r requirements.txt \
 && rm -f requirements.txt
