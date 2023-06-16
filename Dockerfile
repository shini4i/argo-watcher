#######################
# Frontend build
#######################
FROM node:17.7-alpine3.15 as builder-frontend

WORKDIR /app

COPY web/package.json .
COPY web/package-lock.json .

RUN npm ci --silent
RUN npm install react-scripts --silent

COPY web/ .

RUN npm run build

#######################
# Final image
#######################
FROM alpine:3.18

COPY ./bin/argo-watcher /argo-watcher
COPY --from=builder-frontend /app/build /static

RUN addgroup -S argo-watcher && adduser -S argo-watcher -G argo-watcher
RUN apk add --no-cache ca-certificates

COPY db /db

USER argo-watcher

CMD ["/argo-watcher", "-server"]
