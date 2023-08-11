FROM alpine:3.18

COPY argo-watcher /argo-watcher
COPY web/build /static

RUN addgroup -S argo-watcher && adduser -S argo-watcher -G argo-watcher
RUN apk add --no-cache ca-certificates

USER argo-watcher

CMD ["/argo-watcher", "-server"]
