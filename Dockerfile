FROM alpine:3.19

COPY argo-watcher /argo-watcher
COPY web/build /static
COPY db /db

RUN addgroup -S argo-watcher && adduser -S argo-watcher -G argo-watcher
RUN apk add --no-cache ca-certificates

USER argo-watcher

CMD ["/argo-watcher", "-server"]
