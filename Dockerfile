FROM alpine:3.5

RUN apk add --no-cache ca-certificates

ADD operator/server/fabric-operator-alpine /usr/local/bin

CMD ["fabric-operator-alpine"]