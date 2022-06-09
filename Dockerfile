FROM alpine:3.16 as ca-certs

RUN apk add --no-cache ca-certificates

FROM node:17.7-alpine3.15 as builder-frontend

WORKDIR /app

COPY web/package.json .
COPY web/package-lock.json .

RUN npm ci --silent
RUN npm install react-scripts --silent

COPY web/ .

RUN npm run build

FROM scratch

COPY --from=ca-certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder-frontend /app/build /static

COPY bin/argo-watcher /argo-watcher
COPY db /db

CMD ["/argo-watcher"]
