FROM alpine:3.5

RUN apk add --no-cache ca-certificates

ADD cmd/operator/fabric-operator-alpine /usr/local/bin

CMD ["fabric-operator-alpine"]