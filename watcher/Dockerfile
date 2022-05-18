# poetry 1.1.3, python 3.10
FROM mwalbeck/python-poetry:1.1.13-3.10

# install dependencies for psycopg2
RUN apt update && apt install libpq-dev gcc -y && apt clean
